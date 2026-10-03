package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/realtime"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

// stack is one API instance: the router, its realtime hub and poll, and
// the Redis doorbell publisher and listener, served over real HTTP.
type stack struct {
	h   http.Handler
	url string
	hub *realtime.Hub
}

func newStack(t *testing.T, redisURL string, opts realtime.Options) *stack {
	t.Helper()
	rdb := redisClient(t, redisURL)
	ctx, cancel := context.WithCancel(context.Background())
	opts.OriginPatterns = realtime.OriginPatterns(origin)
	hub := realtime.NewHub(realtime.NewPGStore(pool), opts)
	pub := realtime.NewPublisher(rdb)
	go hub.Run(ctx)
	go pub.Run(ctx)
	go realtime.Listen(ctx, rdb, hub)
	h := NewRouter(Config{CORSOrigin: origin, Pool: pool, Doorbell: pub, Realtime: hub})
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		hub.CloseAll()
		srv.Close()
		cancel()
	})
	return &stack{h: h, url: srv.URL, hub: hub}
}

func redisClient(t *testing.T, url string) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opts.ContextTimeoutEnabled = true
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// downRedis is the URL of a Redis that isn't running.
func downRedis(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "redis://" + addr
}

// waitForListeners waits until n instances are subscribed to the doorbell
// channels, so a ring can't be published before anyone listens. Each
// listener subscribes to "projects" after its project:* pattern, on the
// same connection, so counting "projects" subscribers is enough. (PUBSUB
// NUMPAT counts unique patterns, not subscribers.)
func waitForListeners(t *testing.T, redisURL string, n int) {
	t.Helper()
	rdb := redisClient(t, redisURL)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		subs, _ := rdb.PubSubNumSub(context.Background(), "projects").Result()
		if subs["projects"] >= int64(n) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("fewer than %d instances listening on Redis", n)
}

func (s *stack) wsURL(user uuid.UUID) string {
	return "ws" + strings.TrimPrefix(s.url, "http") + "/ws?userId=" + user.String()
}

// liveClient is a WebSocket client of the API, as the browser would be.
type liveClient struct {
	t *testing.T
	c *websocket.Conn
}

func (s *stack) connect(t *testing.T, user uuid.UUID) *liveClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, s.wsURL(user), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return &liveClient{t: t, c: c}
}

func (lc *liveClient) send(v any) {
	lc.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		lc.t.Fatal(err)
	}
	if err := lc.c.Write(context.Background(), websocket.MessageText, b); err != nil {
		lc.t.Fatal(err)
	}
}

func (lc *liveClient) subscribe(project uuid.UUID, fromSeq int64) {
	lc.send(map[string]any{"type": "subscribe", "projectId": project, "fromSeq": fromSeq})
}

type liveMsg struct {
	Type    string
	Seq     int64
	Event   apigen.Event
	Code    string
	Project apigen.ProjectSummary
	raw     []byte
}

func (lc *liveClient) next(within time.Duration) liveMsg {
	lc.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	_, b, err := lc.c.Read(ctx)
	if err != nil {
		lc.t.Fatalf("read: %v", err)
	}
	var m liveMsg
	if err := json.Unmarshal(b, &m); err != nil {
		lc.t.Fatalf("decode %s: %v", b, err)
	}
	m.raw = b
	return m
}

func (lc *liveClient) events(from, to int64) {
	lc.t.Helper()
	for seq := from; seq <= to; seq++ {
		if m := lc.next(2 * time.Second); m.Type != "event" || m.Event.Seq != seq {
			lc.t.Fatalf("got %s seq %d, want event %d", m.Type, m.Event.Seq, seq)
		}
	}
}

func (lc *liveClient) caughtUp(seq int64) {
	lc.t.Helper()
	if m := lc.next(2 * time.Second); m.Type != "caughtUp" || m.Seq != seq {
		lc.t.Fatalf("got %s %d, want caughtUp %d", m.Type, m.Seq, seq)
	}
}

func (lc *liveClient) quiet() {
	lc.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, b, err := lc.c.Read(ctx); err == nil {
		lc.t.Fatalf("unexpected message %s", b)
	}
}

// closedWith reads until the connection closes and returns the close code.
func (lc *liveClient) closedWith(within time.Duration) websocket.StatusCode {
	lc.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		if _, _, err := lc.c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func TestLive_ChangeReachesAnotherInstanceWithinASecond(t *testing.T) {
	redisURL := testdb.Redis(t)
	// No poll: only the doorbell can deliver.
	a := newStack(t, redisURL, realtime.Options{PollInterval: time.Hour})
	b := newStack(t, redisURL, realtime.Options{PollInterval: time.Hour})
	waitForListeners(t, redisURL, 2)
	ana, raj := testdb.User(t, pool), testdb.User(t, pool)
	project := createProject(t, a.h, ana, map[string]any{"name": "Live"})

	watcher := b.connect(t, raj)
	watcher.subscribe(project, 0)
	watcher.caughtUp(0)

	start := time.Now()
	seq := submit(t, a.h, project, ana, map[string]any{"type": "task.create", "id": testdb.NewID(), "title": "Hello"})
	m := watcher.next(time.Second)
	if m.Type != "event" || m.Event.Seq != seq {
		t.Fatalf("got %s seq %d, want event %d", m.Type, m.Event.Seq, seq)
	}
	t.Logf("delivered across instances in %v", time.Since(start))

	// The pushed event is the same object the log endpoint serves.
	var page struct{ Events []json.RawMessage }
	wantOK(t, get(t, a.h, "/projects/"+project.String()+"/events", ana, &page))
	var pushed struct{ Event json.RawMessage }
	if err := json.Unmarshal(m.raw, &pushed); err != nil {
		t.Fatal(err)
	}
	if string(pushed.Event) != string(page.Events[0]) {
		t.Errorf("pushed %s\nlogged %s", pushed.Event, page.Events[0])
	}
}

func TestLive_ReconnectReplaysExactlyTheMissedEvents(t *testing.T) {
	redisURL := testdb.Redis(t)
	s := newStack(t, redisURL, realtime.Options{PollInterval: time.Hour})
	waitForListeners(t, redisURL, 1)
	user := testdb.User(t, pool)
	project := createProject(t, s.h, user, map[string]any{"name": "Replay"})
	edit := func(i int) {
		submit(t, s.h, project, user, map[string]any{"type": "task.create", "id": testdb.NewID(), "title": "T"})
	}

	first := s.connect(t, user)
	first.subscribe(project, 0)
	first.caughtUp(0)
	edit(1)
	edit(2)
	first.events(1, 2)
	first.c.Close(websocket.StatusNormalClosure, "offline")

	edit(3)
	edit(4)
	edit(5)

	again := s.connect(t, user)
	again.subscribe(project, 2)
	again.events(3, 5)
	again.caughtUp(5)
	edit(6)
	again.events(6, 6)
	again.quiet()
}

func TestLive_RedisDownFallsBackToThePoll(t *testing.T) {
	s := newStack(t, downRedis(t), realtime.Options{PollInterval: 500 * time.Millisecond})
	user := testdb.User(t, pool)
	project := createProject(t, s.h, user, map[string]any{"name": "No Redis"})
	c := s.connect(t, user)
	c.subscribe(project, 0)
	c.caughtUp(0)

	start := time.Now()
	seq := submit(t, s.h, project, user, map[string]any{"type": "task.create", "id": testdb.NewID(), "title": "T"})
	if d := time.Since(start); d > time.Second {
		t.Errorf("the write took %v with Redis down", d)
	}
	if m := c.next(3 * time.Second); m.Type != "event" || m.Event.Seq != seq {
		t.Fatalf("got %s seq %d, want event %d", m.Type, m.Event.Seq, seq)
	}
}

func TestLive_ChecksIdentityAndOrigin(t *testing.T) {
	s := newStack(t, downRedis(t), realtime.Options{})
	user := testdb.User(t, pool)
	dial := func(url string, header http.Header) (*websocket.Conn, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			if resp == nil {
				t.Fatalf("dial %s: %v", url, err)
			}
			return nil, resp.StatusCode
		}
		t.Cleanup(func() { c.CloseNow() })
		return c, http.StatusSwitchingProtocols
	}
	allowed := http.Header{"Origin": {origin}}

	if _, code := dial(s.wsURL(testdb.NewID()), allowed); code != http.StatusUnauthorized {
		t.Errorf("unknown user: status %d, want 401", code)
	}
	if _, code := dial(strings.TrimSuffix(s.wsURL(uuid.Nil), uuid.Nil.String()), allowed); code != http.StatusUnauthorized {
		t.Errorf("no user: status %d, want 401", code)
	}
	if _, code := dial(s.wsURL(user), http.Header{"Origin": {"http://evil.example"}}); code != http.StatusForbidden {
		t.Errorf("foreign origin: status %d, want 403", code)
	}
	if _, code := dial(s.wsURL(user), allowed); code != http.StatusSwitchingProtocols {
		t.Errorf("the web app's origin: status %d, want 101", code)
	}
}

func TestLive_BadMessagesGetAnErrorAndTheConnectionStaysOpen(t *testing.T) {
	s := newStack(t, downRedis(t), realtime.Options{})
	user := testdb.User(t, pool)
	project := createProject(t, s.h, user, map[string]any{"name": "P"})
	c := s.connect(t, user)

	if err := c.c.Write(context.Background(), websocket.MessageText, []byte("nope")); err != nil {
		t.Fatal(err)
	}
	if m := c.next(time.Second); m.Type != "error" || m.Code != "validation_failed" {
		t.Errorf("text garbage: got %s %s, want error validation_failed", m.Type, m.Code)
	}
	if err := c.c.Write(context.Background(), websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if m := c.next(time.Second); m.Type != "error" || m.Code != "validation_failed" {
		t.Errorf("binary message: got %s %s, want error validation_failed", m.Type, m.Code)
	}
	c.subscribe(project, 0)
	c.caughtUp(0)
}

func TestLive_ProjectList(t *testing.T) {
	redisURL := testdb.Redis(t)
	s := newStack(t, redisURL, realtime.Options{PollInterval: time.Hour})
	waitForListeners(t, redisURL, 1)
	user := testdb.User(t, pool)
	other := createProject(t, s.h, user, map[string]any{"name": "Other"})
	c := s.connect(t, user)
	c.send(map[string]any{"type": "subscribeProjects"})
	// The server handles a connection's messages in order, so once this
	// subscribe is answered, the one above has landed too.
	c.subscribe(other, 0)
	for m := c.next(2 * time.Second); m.Type != "caughtUp"; m = c.next(2 * time.Second) {
		if m.Type != "projectsChanged" { // "Other"'s own ring may still be in flight
			t.Fatalf("got %s while waiting for caughtUp", m.Type)
		}
	}

	project := createProject(t, s.h, user, map[string]any{"name": "Fresh"})
	if m := c.listUpdate(project); m.Project.Name != "Fresh" {
		t.Fatalf("after create: got %+v", m.Project)
	}
	submit(t, s.h, project, user, map[string]any{"type": "project.update", "name": "Renamed"})
	if m := c.listUpdate(project); m.Project.Name != "Renamed" {
		t.Fatalf("after rename: got %+v", m.Project)
	}
}

// listUpdate returns the next project-list update about project, skipping
// updates about other projects.
func (lc *liveClient) listUpdate(project uuid.UUID) liveMsg {
	lc.t.Helper()
	for {
		m := lc.next(time.Second)
		if m.Type != "projectsChanged" {
			lc.t.Fatalf("got %s, want projectsChanged", m.Type)
		}
		if m.Project.Id == project {
			return m
		}
	}
}

func TestLive_DropsAClientThatStopsAnsweringPings(t *testing.T) {
	s := newStack(t, downRedis(t), realtime.Options{PingInterval: 100 * time.Millisecond, PongTimeout: 100 * time.Millisecond})
	c := s.connect(t, testdb.User(t, pool))

	time.Sleep(time.Second) // not reading, so pings go unanswered
	if code := c.closedWith(3 * time.Second); code != websocket.StatusPolicyViolation {
		t.Errorf("closed with %d, want 1008", code)
	}
}

func TestLive_ShutdownSaysGoingAway(t *testing.T) {
	s := newStack(t, downRedis(t), realtime.Options{})
	c := s.connect(t, testdb.User(t, pool))
	s.hub.CloseAll()
	if code := c.closedWith(3 * time.Second); code != websocket.StatusGoingAway {
		t.Errorf("closed with %d, want 1001", code)
	}
}
