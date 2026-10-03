// Package realtime delivers each project's events to WebSocket clients.
//
// Doorbell + pull: after a commit the writing instance rings a tiny Redis
// doorbell ({projectId, seq}); every instance then reads the new events from
// Postgres, in seq order, and fans them out to its own clients. Postgres is
// the only source of order, so a lost, duplicated or late doorbell can't
// break anything: the next doorbell, or the poll every few seconds, catches
// up.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
)

// Store is what the hub reads. NewPGStore reads Postgres.
type Store interface {
	// ProjectSeq returns the project's latest seq, or ErrProjectNotFound.
	ProjectSeq(ctx context.Context, projectID uuid.UUID) (int64, error)
	// ProjectSeqs returns the latest seq of each project that exists.
	ProjectSeqs(ctx context.Context, projectIDs []uuid.UUID) (map[uuid.UUID]int64, error)
	// EventsAfter returns up to limit events with seq > after, in seq order.
	EventsAfter(ctx context.Context, projectID uuid.UUID, after int64, limit int32) ([]apigen.Event, error)
	// ProjectSummary returns the project as the project list shows it.
	ProjectSummary(ctx context.Context, projectID uuid.UUID) (apigen.ProjectSummary, error)
	// UserExists reports whether userID names a user.
	UserExists(ctx context.Context, userID uuid.UUID) (bool, error)
}

// ErrProjectNotFound is returned by a Store for a project that doesn't exist.
var ErrProjectNotFound = errors.New("project not found")

// StatusSlowConsumer closes a connection whose send queue overflowed. The
// client reconnects and replays from the last seq it applied.
const StatusSlowConsumer websocket.StatusCode = 4008

// pullBatch is how many events a feed or a replay reads at a time.
const pullBatch = 500

// Options tunes the hub. Zero values take the defaults.
type Options struct {
	PollInterval   time.Duration // how often to check followed projects without a doorbell (5s)
	PingInterval   time.Duration // how often to ping each connection (20s)
	PongTimeout    time.Duration // how long to wait for the pong (10s)
	WriteTimeout   time.Duration // how long one write may take (5s)
	QueueSize      int           // messages queued per connection before it's dropped (256)
	MaxReplay      int64         // events a subscribe may replay before it's told to resync (5,000)
	OriginPatterns []string      // origins allowed to open a WebSocket, besides the API's own
}

func (o Options) withDefaults() Options {
	if o.PollInterval == 0 {
		o.PollInterval = 5 * time.Second
	}
	if o.PingInterval == 0 {
		o.PingInterval = 20 * time.Second
	}
	if o.PongTimeout == 0 {
		o.PongTimeout = 10 * time.Second
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 5 * time.Second
	}
	if o.QueueSize == 0 {
		o.QueueSize = 256
	}
	if o.MaxReplay == 0 {
		o.MaxReplay = 5000
	}
	return o
}

// Hub tracks this instance's connections and, for each project they follow,
// one feed that pulls the project's new events and fans them out.
type Hub struct {
	store Store
	opts  Options

	mu       sync.Mutex
	feeds    map[uuid.UUID]*feed
	clients  map[*client]struct{}
	watchers map[*client]struct{} // following the project list
}

// NewHub returns a hub reading from store. Call Run to start its poll.
func NewHub(store Store, opts Options) *Hub {
	return &Hub{
		store:    store,
		opts:     opts.withDefaults(),
		feeds:    map[uuid.UUID]*feed{},
		clients:  map[*client]struct{}{},
		watchers: map[*client]struct{}{},
	}
}

// client is one connection as the hub sees it: a bounded queue of messages
// for its writer, and the project it follows.
type client struct {
	send chan outbound
	drop func(code websocket.StatusCode, reason string)
	once sync.Once

	// gen numbers the client's subscriptions. Queued messages carry the gen
	// they were queued for, and the writer skips those of a subscription the
	// client has since left.
	gen atomic.Uint64

	mu      sync.Mutex
	project uuid.UUID // followed project; uuid.Nil for none
}

// outbound is one queued item: an encoded message, or a replay to stream.
type outbound struct {
	gen    uint64 // the subscription it belongs to; 0 if none
	msg    []byte
	replay *replay
}

// replay asks the writer to stream the events (from, to] from the log, then
// caughtUp(to).
type replay struct {
	projectID uuid.UUID
	from, to  int64
}

// feed follows one project for this instance's subscribers.
type feed struct {
	projectID uuid.UUID
	wake      chan struct{}
	stop      context.CancelFunc

	mu            sync.Mutex
	lastDelivered int64
	subs          map[*client]subscription
}

type subscription struct {
	gen   uint64
	after int64 // deliver only events with a greater seq
}

// connect registers a connection. drop is how the hub closes it (a full
// queue, or shutdown); it may block, and is called at most once.
func (h *Hub) connect(drop func(code websocket.StatusCode, reason string)) *client {
	c := &client{send: make(chan outbound, h.opts.QueueSize), drop: drop}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// disconnect forgets a connection. Calling it again is a no-op.
func (h *Hub) disconnect(c *client) {
	h.leave(c)
	h.mu.Lock()
	delete(h.clients, c)
	delete(h.watchers, c)
	h.mu.Unlock()
}

// dropClient closes c without blocking the caller.
func (h *Hub) dropClient(c *client, code websocket.StatusCode, reason string) {
	c.once.Do(func() { go c.drop(code, reason) })
}

// enqueue queues o for c without blocking. A full queue means the client
// can't keep up: it is dropped with StatusSlowConsumer, and enqueue reports
// false so the caller can forget it.
func (h *Hub) enqueue(c *client, o outbound) bool {
	select {
	case c.send <- o:
		return true
	default:
		h.dropClient(c, StatusSlowConsumer, "send queue full")
		return false
	}
}

// receive handles one message from the client. A message the hub can't act
// on gets an error reply; the connection stays open.
func (h *Hub) receive(ctx context.Context, c *client, data []byte) {
	var m apigen.WsClientMessage
	if err := json.Unmarshal(data, &m); err != nil {
		h.sendError(c, apigen.ErrorCodeValidationFailed, "message is not JSON")
		return
	}
	v, err := m.ValueByDiscriminator()
	if err != nil {
		h.sendError(c, apigen.ErrorCodeValidationFailed, "message is not a valid subscribe, unsubscribe or subscribeProjects")
		return
	}
	switch v := v.(type) {
	case apigen.WsSubscribe:
		switch {
		case v.ProjectId == uuid.Nil:
			h.sendError(c, apigen.ErrorCodeValidationFailed, "projectId is required")
		case v.FromSeq < 0:
			h.sendError(c, apigen.ErrorCodeValidationFailed, "fromSeq must be 0 or more")
		default:
			h.subscribe(ctx, c, v.ProjectId, v.FromSeq)
		}
	case apigen.WsUnsubscribe:
		c.mu.Lock()
		following := c.project == v.ProjectId
		c.mu.Unlock()
		if following {
			h.leave(c)
		}
	case apigen.WsSubscribeProjects:
		h.mu.Lock()
		h.watchers[c] = struct{}{}
		h.mu.Unlock()
	}
}

// subscribe makes c follow a project from fromSeq: the writer replays the
// events up to the feed's position, then sends caughtUp, and the feed
// delivers everything after. Both are decided under the feed's lock, which
// delivery also holds, so every event lands in exactly one of the two.
func (h *Hub) subscribe(ctx context.Context, c *client, projectID uuid.UUID, fromSeq int64) {
	gen := h.leave(c) // a connection follows one project at a time

	head, err := h.store.ProjectSeq(ctx, projectID)
	if errors.Is(err, ErrProjectNotFound) {
		h.sendError(c, apigen.ErrorCodeProjectNotFound, "project "+projectID.String()+" does not exist")
		return
	}
	if err != nil {
		slog.Warn("subscribe: read project seq", "project", projectID, "err", err)
		h.sendError(c, apigen.ErrorCodeUnavailable, "the database is unreachable; subscribe again")
		return
	}
	switch {
	case fromSeq > head:
		h.resync(c, gen, projectID, apigen.UnknownSeq)
		return
	case head-fromSeq > h.opts.MaxReplay:
		h.resync(c, gen, projectID, apigen.TooFarBehind)
		return
	}

	h.mu.Lock()
	f, existed := h.feeds[projectID]
	if !existed {
		f = h.startFeed(projectID, head)
	}
	f.mu.Lock()
	h.mu.Unlock()
	// Replay up to wherever the feed is. If the client is ahead of a lagging
	// feed, replay nothing and skip live events it already has.
	to := max(f.lastDelivered, fromSeq)
	queued := h.enqueue(c, outbound{gen: gen, replay: &replay{projectID: projectID, from: fromSeq, to: to}})
	if queued {
		f.subs[c] = subscription{gen: gen, after: to}
		c.mu.Lock()
		c.project = projectID
		c.mu.Unlock()
	}
	lagging := f.lastDelivered < head
	f.mu.Unlock()

	// A new feed may have missed doorbells rung before it existed.
	if !existed || lagging {
		f.poke()
	}
}

// leave stops c following its project, stopping the project's feed if c
// was its last follower, and returns c's new subscription gen.
func (h *Hub) leave(c *client) uint64 {
	c.mu.Lock()
	project := c.project
	c.project = uuid.Nil
	gen := c.gen.Add(1)
	c.mu.Unlock()
	if project == uuid.Nil {
		return gen
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.feeds[project]
	if f == nil {
		return gen
	}
	f.mu.Lock()
	delete(f.subs, c)
	empty := len(f.subs) == 0
	f.mu.Unlock()
	if empty {
		delete(h.feeds, project)
		f.stop()
	}
	return gen
}

func (h *Hub) resync(c *client, gen uint64, projectID uuid.UUID, reason apigen.WsResyncReason) {
	h.enqueue(c, outbound{gen: gen, msg: encode(apigen.WsResync{Type: "resync", ProjectId: projectID, Reason: reason})})
}

func (h *Hub) sendError(c *client, code apigen.ErrorCode, message string) {
	h.enqueue(c, outbound{msg: encode(apigen.WsError{Type: "error", Code: code, Message: message})})
}

// startFeed starts following a project from seq head. The caller holds h.mu.
func (h *Hub) startFeed(projectID uuid.UUID, head int64) *feed {
	ctx, stop := context.WithCancel(context.Background())
	f := &feed{
		projectID:     projectID,
		wake:          make(chan struct{}, 1),
		stop:          stop,
		lastDelivered: head,
		subs:          map[*client]subscription{},
	}
	h.feeds[projectID] = f
	go h.runFeed(ctx, f)
	return f
}

// poke wakes the feed to pull. Pokes that arrive while it's already awake
// fold into one.
func (f *feed) poke() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (h *Hub) runFeed(ctx context.Context, f *feed) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.wake:
			h.pull(ctx, f)
		}
	}
}

// pull reads every committed event after the feed's position and fans it
// out. Only the feed's goroutine calls it, so only it moves lastDelivered.
func (h *Hub) pull(ctx context.Context, f *feed) {
	for {
		f.mu.Lock()
		after := f.lastDelivered
		f.mu.Unlock()
		events, err := h.store.EventsAfter(ctx, f.projectID, after, pullBatch)
		if err != nil {
			if ctx.Err() == nil {
				// The next doorbell or poll tries again.
				slog.Warn("feed: read events", "project", f.projectID, "err", err)
			}
			return
		}
		if len(events) == 0 {
			return
		}
		h.deliver(f, events)
		if len(events) < pullBatch {
			return
		}
	}
}

// deliver queues events for every subscriber of the feed, in seq order.
func (h *Hub) deliver(f *feed, events []apigen.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range events {
		b := encode(apigen.WsEvent{Type: "event", ProjectId: f.projectID, Event: e})
		for c, sub := range f.subs {
			if e.Seq <= sub.after {
				continue
			}
			if !h.enqueue(c, outbound{gen: sub.gen, msg: b}) {
				delete(f.subs, c)
			}
		}
		f.lastDelivered = e.Seq
	}
}

// Ring tells the hub a project's log has reached seq. A doorbell for a
// project nobody here follows, or one the feed is already past, does
// nothing.
func (h *Hub) Ring(projectID uuid.UUID, seq int64) {
	h.mu.Lock()
	f := h.feeds[projectID]
	h.mu.Unlock()
	if f == nil {
		return
	}
	f.mu.Lock()
	stale := seq <= f.lastDelivered
	f.mu.Unlock()
	if !stale {
		f.poke()
	}
}

// ProjectChanged sends the project's new summary to every client following
// the project list.
func (h *Hub) ProjectChanged(projectID uuid.UUID) {
	h.mu.Lock()
	watchers := make([]*client, 0, len(h.watchers))
	for c := range h.watchers {
		watchers = append(watchers, c)
	}
	h.mu.Unlock()
	if len(watchers) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := h.store.ProjectSummary(ctx, projectID)
	if err != nil {
		slog.Warn("project list: read project", "project", projectID, "err", err)
		return
	}
	b := encode(apigen.WsProjectsChanged{Type: "projectsChanged", Project: p})
	for _, c := range watchers {
		h.enqueue(c, outbound{msg: b})
	}
}

// Run polls the followed projects until ctx is done, so their events arrive
// even when doorbells don't (Redis down, or a crash between commit and
// ring).
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(h.opts.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.poll(ctx)
		}
	}
}

func (h *Hub) poll(ctx context.Context) {
	h.mu.Lock()
	ids := make([]uuid.UUID, 0, len(h.feeds))
	for id := range h.feeds {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	seqs, err := h.store.ProjectSeqs(ctx, ids)
	if err != nil {
		slog.Warn("poll: read project seqs", "err", err)
		return
	}
	for id, seq := range seqs {
		h.Ring(id, seq)
	}
}

// CloseAll closes every connection with 1001 Going Away, for shutdown.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		h.dropClient(c, websocket.StatusGoingAway, "server shutting down")
	}
}

// writer is where a connection's messages go: the WebSocket, or a test.
type writer interface {
	Write(ctx context.Context, msg []byte) error
}

// errStalled means a write didn't finish within the write timeout.
var errStalled = errors.New("write stalled")

// pump writes c's queued messages to w until ctx is done or a write fails.
func (h *Hub) pump(ctx context.Context, c *client, w writer) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case o := <-c.send:
			if o.gen != 0 && o.gen != c.gen.Load() {
				continue // queued for a subscription the client has left
			}
			var err error
			if o.replay != nil {
				err = h.stream(ctx, c, o.gen, *o.replay, w)
			} else {
				err = h.write(ctx, w, o.msg)
			}
			if err != nil {
				return err
			}
		}
	}
}

// stream writes the events (from, to] from the log, then caughtUp(to).
func (h *Hub) stream(ctx context.Context, c *client, gen uint64, r replay, w writer) error {
	after := r.from
	for after < r.to {
		if c.gen.Load() != gen {
			return nil // the client moved on mid-replay
		}
		events, err := h.store.EventsAfter(ctx, r.projectID, after, int32(min(pullBatch, r.to-after)))
		if err != nil {
			return err
		}
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			if err := h.write(ctx, w, encode(apigen.WsEvent{Type: "event", ProjectId: r.projectID, Event: e})); err != nil {
				return err
			}
			after = e.Seq
		}
	}
	return h.write(ctx, w, encode(apigen.WsCaughtUp{Type: "caughtUp", ProjectId: r.projectID, Seq: r.to}))
}

func (h *Hub) write(ctx context.Context, w writer, msg []byte) error {
	if msg == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, h.opts.WriteTimeout)
	defer cancel()
	err := w.Write(ctx, msg)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errStalled
	}
	return err
}

// encode marshals one of the contract's message types. Only a corrupt
// event payload could make it fail; that message is logged and skipped (as
// nil) rather than taking the server down.
func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("realtime: encode message", "err", err)
		return nil
	}
	return b
}
