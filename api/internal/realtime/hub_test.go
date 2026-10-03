package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
)

// memStore is an in-memory Store: per-project logs with seqs 1..n.
type memStore struct {
	mu    sync.Mutex
	logs  map[uuid.UUID][]apigen.Event
	names map[uuid.UUID]string
	reads int
}

func newMemStore() *memStore {
	return &memStore{logs: map[uuid.UUID][]apigen.Event{}, names: map[uuid.UUID]string{}}
}

// project adds a project whose log already holds n events.
func (m *memStore) project(n int) uuid.UUID {
	id := uuid.New()
	m.mu.Lock()
	m.logs[id] = nil
	m.names[id] = "Project " + id.String()[:4]
	m.mu.Unlock()
	m.append(id, n)
	return id
}

// append commits n events to the project's log and returns its new seq.
func (m *memStore) append(projectID uuid.UUID, n int) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	log := m.logs[projectID]
	for range n {
		var change apigen.Change
		if err := change.FromTaskDelete(apigen.TaskDelete{TaskId: uuid.New()}); err != nil {
			panic(err)
		}
		log = append(log, apigen.Event{
			ProjectId: projectID, Seq: int64(len(log)) + 1, ChangeId: uuid.New(), ActorId: uuid.New(),
			CreatedAt: time.Now(), Touches: []uuid.UUID{}, Change: change,
		})
	}
	m.logs[projectID] = log
	return int64(len(log))
}

func (m *memStore) rename(projectID uuid.UUID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names[projectID] = name
}

func (m *memStore) readCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads
}

func (m *memStore) ProjectSeq(_ context.Context, projectID uuid.UUID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	log, ok := m.logs[projectID]
	if !ok {
		return 0, ErrProjectNotFound
	}
	return int64(len(log)), nil
}

func (m *memStore) ProjectSeqs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seqs := map[uuid.UUID]int64{}
	for _, id := range ids {
		if log, ok := m.logs[id]; ok {
			seqs[id] = int64(len(log))
		}
	}
	return seqs, nil
}

func (m *memStore) EventsAfter(_ context.Context, projectID uuid.UUID, after int64, limit int32) ([]apigen.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	log := m.logs[projectID]
	if after >= int64(len(log)) {
		return nil, nil
	}
	end := min(after+int64(limit), int64(len(log)))
	return append([]apigen.Event(nil), log[after:end]...), nil
}

func (m *memStore) ProjectSummary(_ context.Context, projectID uuid.UUID) (apigen.ProjectSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	log, ok := m.logs[projectID]
	if !ok {
		return apigen.ProjectSummary{}, ErrProjectNotFound
	}
	return apigen.ProjectSummary{Id: projectID, Name: m.names[projectID], Seq: int64(len(log))}, nil
}

func (m *memStore) UserExists(context.Context, uuid.UUID) (bool, error) { return true, nil }

// msg is any server message, decoded loosely for assertions.
type msg struct {
	Type      string
	ProjectId uuid.UUID
	Seq       int64
	Event     struct{ Seq int64 }
	Reason    string
	Code      string
	Project   struct {
		Id   uuid.UUID
		Name string
	}
}

// recorder is a writer that hands every message to the test.
type recorder chan []byte

func (r recorder) Write(_ context.Context, b []byte) error {
	r <- append([]byte(nil), b...)
	return nil
}

// conn is one client of the hub, as a test drives it.
type conn struct {
	t      *testing.T
	h      *Hub
	c      *client
	out    recorder
	drops  chan websocket.StatusCode
	cancel context.CancelFunc
}

// open connects a client without starting its writer.
func open(t *testing.T, h *Hub) *conn {
	t.Helper()
	tc := &conn{t: t, h: h, out: make(recorder, 100_000), drops: make(chan websocket.StatusCode, 4)}
	tc.c = h.connect(func(code websocket.StatusCode, _ string) { tc.drops <- code })
	t.Cleanup(func() {
		if tc.cancel != nil {
			tc.cancel()
		}
		h.disconnect(tc.c)
	})
	return tc
}

// start runs the client's writer, as the WebSocket would.
func (tc *conn) start() *conn {
	ctx, cancel := context.WithCancel(context.Background())
	tc.cancel = cancel
	go func() { _ = tc.h.pump(ctx, tc.c, tc.out) }()
	return tc
}

func dial(t *testing.T, h *Hub) *conn { t.Helper(); return open(t, h).start() }

func (tc *conn) send(format string, args ...any) {
	tc.h.receive(context.Background(), tc.c, []byte(fmt.Sprintf(format, args...)))
}

func (tc *conn) subscribe(project uuid.UUID, fromSeq int64) {
	tc.send(`{"type":"subscribe","projectId":"%s","fromSeq":%d}`, project, fromSeq)
}

func (tc *conn) next() msg {
	tc.t.Helper()
	select {
	case b := <-tc.out:
		var m msg
		if err := json.Unmarshal(b, &m); err != nil {
			tc.t.Fatalf("decode %s: %v", b, err)
		}
		return m
	case <-time.After(3 * time.Second):
		tc.t.Fatal("no message within 3s")
		return msg{}
	}
}

// events expects the events from..to, in order.
func (tc *conn) events(from, to int64) {
	tc.t.Helper()
	for seq := from; seq <= to; seq++ {
		if m := tc.next(); m.Type != "event" || m.Event.Seq != seq {
			tc.t.Fatalf("got %s seq %d, want event %d", m.Type, m.Event.Seq, seq)
		}
	}
}

func (tc *conn) caughtUp(seq int64) {
	tc.t.Helper()
	if m := tc.next(); m.Type != "caughtUp" || m.Seq != seq {
		tc.t.Fatalf("got %+v, want caughtUp %d", m, seq)
	}
}

// quiet expects no message for a moment.
func (tc *conn) quiet() {
	tc.t.Helper()
	select {
	case b := <-tc.out:
		tc.t.Fatalf("unexpected message %s", b)
	case <-time.After(150 * time.Millisecond):
	}
}

func newHub(store Store, opts Options) *Hub {
	if opts.PollInterval == 0 {
		opts.PollInterval = time.Hour // tests ring explicitly unless they test the poll
	}
	return NewHub(store, opts)
}

func TestSubscribe_ReplaysThenStreamsLive(t *testing.T) {
	store := newMemStore()
	p := store.project(5)
	h := newHub(store, Options{})
	c := dial(t, h)

	c.subscribe(p, 2)
	c.events(3, 5)
	c.caughtUp(5)

	h.Ring(p, store.append(p, 2))
	c.events(6, 7)
	c.quiet()
}

func TestSubscribe_AtTheHead(t *testing.T) {
	store := newMemStore()
	p := store.project(3)
	h := newHub(store, Options{})
	c := dial(t, h)

	c.subscribe(p, 3)
	c.caughtUp(3)
	c.quiet()
}

func TestRing_DuplicateAndStaleDoorbellsAreHarmless(t *testing.T) {
	store := newMemStore()
	p := store.project(5)
	h := newHub(store, Options{})
	c := dial(t, h)
	c.subscribe(p, 5)
	c.caughtUp(5)

	h.Ring(p, store.append(p, 1))
	c.events(6, 6)
	c.quiet()
	reads := store.readCount()

	for _, seq := range []int64{6, 3, 6, 1} {
		h.Ring(p, seq)
	}
	c.quiet()
	if got := store.readCount(); got != reads {
		t.Errorf("stale doorbells caused %d log reads, want 0", got-reads)
	}
}

func TestRing_LostDoorbellsAreCaughtUpByTheNext(t *testing.T) {
	store := newMemStore()
	p := store.project(0)
	h := newHub(store, Options{})
	c := dial(t, h)
	c.subscribe(p, 0)
	c.caughtUp(0)

	store.append(p, 3) // three commits whose doorbells were lost
	h.Ring(p, 2)       // and a late doorbell for an earlier one
	c.events(1, 3)
	c.quiet()
}

func TestPoll_DeliversWithoutDoorbells(t *testing.T) {
	store := newMemStore()
	p := store.project(1)
	h := NewHub(store, Options{PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	c := dial(t, h)
	c.subscribe(p, 1)
	c.caughtUp(1)

	store.append(p, 2) // Redis is down: nobody rings
	c.events(2, 3)
}

func TestSubscribe_ResyncWhenTooFarBehind(t *testing.T) {
	store := newMemStore()
	p := store.project(30)
	h := newHub(store, Options{MaxReplay: 10})

	far := dial(t, h)
	far.subscribe(p, 19) // 11 behind
	if m := far.next(); m.Type != "resync" || m.ProjectId != p || m.Reason != "too_far_behind" {
		t.Fatalf("got %+v, want resync too_far_behind", m)
	}
	far.quiet()

	near := dial(t, h)
	near.subscribe(p, 20) // exactly 10 behind: still replayed
	near.events(21, 30)
	near.caughtUp(30)
}

func TestSubscribe_ResyncWhenPastTheEndOfTheLog(t *testing.T) {
	store := newMemStore()
	p := store.project(5)
	h := newHub(store, Options{})
	c := dial(t, h)

	c.subscribe(p, 9) // e.g. the database was reset
	if m := c.next(); m.Type != "resync" || m.Reason != "unknown_seq" {
		t.Fatalf("got %+v, want resync unknown_seq", m)
	}
}

func TestSubscribe_UnknownProject(t *testing.T) {
	store := newMemStore()
	h := newHub(store, Options{})
	c := dial(t, h)

	c.subscribe(uuid.New(), 0)
	if m := c.next(); m.Type != "error" || m.Code != "project_not_found" {
		t.Fatalf("got %+v, want error project_not_found", m)
	}
	// The connection is still good.
	p := store.project(1)
	c.subscribe(p, 0)
	c.events(1, 1)
	c.caughtUp(1)
}

func TestReceive_BadMessages(t *testing.T) {
	store := newMemStore()
	p := store.project(1)
	h := newHub(store, Options{})
	c := dial(t, h)

	for _, bad := range []string{
		`not json`,
		`{"type":"dance"}`,
		`{"projectId":"` + p.String() + `"}`,
		`{"type":"subscribe","projectId":"not-a-uuid","fromSeq":0}`,
		`{"type":"subscribe","fromSeq":0}`,
		`{"type":"subscribe","projectId":"` + p.String() + `","fromSeq":-1}`,
	} {
		c.send("%s", bad)
		if m := c.next(); m.Type != "error" || m.Code != "validation_failed" {
			t.Errorf("%s: got %+v, want error validation_failed", bad, m)
		}
	}
	c.subscribe(p, 0)
	c.events(1, 1)
	c.caughtUp(1)
}

func TestUnsubscribe_StopsDelivery(t *testing.T) {
	store := newMemStore()
	p := store.project(1)
	h := newHub(store, Options{})
	c := dial(t, h)
	c.subscribe(p, 1)
	c.caughtUp(1)

	c.send(`{"type":"unsubscribe","projectId":"%s"}`, p)
	h.Ring(p, store.append(p, 1))
	c.quiet()
}

func TestSubscribe_SwitchingProjectsDropsTheOldStream(t *testing.T) {
	store := newMemStore()
	a, b := store.project(0), store.project(2)
	h := newHub(store, Options{})
	c := open(t, h) // writer not running yet, so messages queue up
	c.subscribe(a, 0)
	h.Ring(a, store.append(a, 3)) // queued for the old subscription

	c.subscribe(b, 0)
	c.start()
	// Nothing from a: neither its catch-up nor the events queued for it.
	c.events(1, 2)
	c.caughtUp(2)
	h.Ring(a, store.append(a, 1))
	c.quiet()
}

func TestProjectsChanged_ReachesProjectListFollowersOnly(t *testing.T) {
	store := newMemStore()
	p := store.project(4)
	h := newHub(store, Options{})
	follower, other := dial(t, h), dial(t, h)
	follower.send(`{"type":"subscribeProjects"}`)

	store.rename(p, "Renamed")
	h.ProjectChanged(p)
	m := follower.next()
	if m.Type != "projectsChanged" || m.Project.Id != p || m.Project.Name != "Renamed" {
		t.Fatalf("got %+v, want projectsChanged for the renamed project", m)
	}
	other.quiet()
}

func TestFeed_StopsReadingWhenTheLastSubscriberLeaves(t *testing.T) {
	store := newMemStore()
	p := store.project(1)
	h := newHub(store, Options{})
	c := dial(t, h)
	c.subscribe(p, 1)
	c.caughtUp(1)
	h.disconnect(c.c)
	reads := store.readCount()

	h.Ring(p, store.append(p, 1))
	time.Sleep(150 * time.Millisecond)
	if got := store.readCount(); got != reads {
		t.Errorf("a project nobody follows was read %d times", got-reads)
	}
}

func TestCloseAll_DropsEveryClientWithGoingAway(t *testing.T) {
	store := newMemStore()
	h := newHub(store, Options{})
	a, b := dial(t, h), dial(t, h)
	a.subscribe(store.project(0), 0)

	h.CloseAll()
	for _, c := range []*conn{a, b} {
		select {
		case code := <-c.drops:
			if code != websocket.StatusGoingAway {
				t.Errorf("closed with %d, want 1001", code)
			}
		case <-time.After(time.Second):
			t.Error("client not closed")
		}
	}
}

func TestSubscribe_RacingLiveWritesHasNoGapsOrDuplicates(t *testing.T) {
	store := newMemStore()
	p := store.project(0)
	// A big queue: this test is about ordering, not about slow clients.
	h := newHub(store, Options{QueueSize: 10_000})
	const total = 1500

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range total {
			seq := store.append(p, 1)
			switch i % 4 { // doorbells arrive on time, twice, never, or late
			case 0:
				h.Ring(p, seq)
			case 1:
				h.Ring(p, seq)
				h.Ring(p, seq)
			case 3:
				h.Ring(p, seq-1)
			}
			if i%50 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()

	type joined struct {
		c    *conn
		from int64
	}
	var subs []joined
	for range 20 {
		head, _ := store.ProjectSeq(context.Background(), p)
		from := rand.Int64N(head + 1) // whatever the client had applied
		c := dial(t, h)
		c.subscribe(p, from)
		subs = append(subs, joined{c, from})
		time.Sleep(2 * time.Millisecond)
	}
	<-done
	h.Ring(p, total)

	for i, s := range subs {
		next, caught := s.from+1, false
		for next <= total {
			switch m := s.c.next(); m.Type {
			case "event":
				if m.Event.Seq != next {
					t.Fatalf("subscriber %d (from %d): got event %d, want %d", i, s.from, m.Event.Seq, next)
				}
				next++
			case "caughtUp":
				if caught || m.Seq != next-1 {
					t.Fatalf("subscriber %d: caughtUp %d after event %d (seen before: %v)", i, m.Seq, next-1, caught)
				}
				caught = true
			default:
				t.Fatalf("subscriber %d: unexpected %+v", i, m)
			}
		}
		if !caught {
			s.c.caughtUp(total)
		}
		s.c.quiet()
	}
}

func TestFullQueue_DropsTheSlowClientWith4008(t *testing.T) {
	store := newMemStore()
	p := store.project(0)
	h := newHub(store, Options{QueueSize: 4})
	slow := open(t, h) // never writes, so its queue fills
	fast := dial(t, h)
	slow.subscribe(p, 0) // queues its replay: 1 of 4
	fast.subscribe(p, 0)
	fast.caughtUp(0)

	h.Ring(p, store.append(p, 4)) // the slow client's 5th message overflows
	select {
	case code := <-slow.drops:
		if code != StatusSlowConsumer {
			t.Errorf("closed with %d, want 4008", code)
		}
	case <-time.After(time.Second):
		t.Fatal("slow client not dropped")
	}
	fast.events(1, 4) // a slow client never holds anyone else back

	h.Ring(p, store.append(p, 2))
	fast.events(5, 6)
	select {
	case <-slow.drops:
		t.Error("slow client dropped twice")
	case <-time.After(100 * time.Millisecond):
	}
}

// stallWriter never finishes a write, like a client whose TCP window is
// full.
type stallWriter struct{}

func (stallWriter) Write(ctx context.Context, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestPump_GivesUpOnAStalledWrite(t *testing.T) {
	store := newMemStore()
	p := store.project(1)
	h := newHub(store, Options{WriteTimeout: 50 * time.Millisecond})
	c := open(t, h)
	c.subscribe(p, 0)

	errs := make(chan error, 1)
	go func() { errs <- h.pump(context.Background(), c.c, stallWriter{}) }()
	select {
	case err := <-errs:
		if !errors.Is(err, errStalled) {
			t.Errorf("pump = %v, want errStalled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pump never gave up on the stalled write")
	}
}
