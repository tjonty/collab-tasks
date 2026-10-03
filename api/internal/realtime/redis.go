package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Redis channels. Doorbells for a project's log go on project:{id}; changes
// to the project list go on projects.
const (
	projectChannelPrefix = "project:"
	projectsChannel      = "projects"
)

// doorbell is what goes over Redis: which project, and how far its log has
// reached. Never the events themselves.
type doorbell struct {
	ProjectID uuid.UUID `json:"projectId"`
	Seq       int64     `json:"seq,omitempty"`
}

type ring struct {
	channel string
	payload []byte
}

// Publisher rings the Redis doorbells after commits. A write never waits on
// Redis: rings queue in memory and Run publishes them. When the queue is
// full (Redis hung or down) rings are dropped; the hubs' poll delivers those
// events a few seconds later instead.
type Publisher struct {
	rdb   *redis.Client
	rings chan ring
}

// NewPublisher returns a Publisher that publishes through rdb once Run is
// running.
func NewPublisher(rdb *redis.Client) *Publisher {
	return &Publisher{rdb: rdb, rings: make(chan ring, 1024)}
}

// Ring queues {projectId, seq} for channel project:{id}.
func (p *Publisher) Ring(projectID uuid.UUID, seq int64) {
	p.queue(projectChannelPrefix+projectID.String(), doorbell{ProjectID: projectID, Seq: seq})
}

// ProjectChanged queues {projectId} for channel projects.
func (p *Publisher) ProjectChanged(projectID uuid.UUID) {
	p.queue(projectsChannel, doorbell{ProjectID: projectID})
}

func (p *Publisher) queue(channel string, d doorbell) {
	payload, _ := json.Marshal(d)
	select {
	case p.rings <- ring{channel: channel, payload: payload}:
	default:
		slog.Warn("doorbell queue full; dropping a ring", "channel", channel)
	}
}

// Run publishes queued rings until ctx is done.
func (p *Publisher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-p.rings:
			pctx, cancel := context.WithTimeout(ctx, time.Second)
			err := p.rdb.Publish(pctx, r.channel, r.payload).Err()
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Warn("ring doorbell", "channel", r.channel, "err", err)
			}
		}
	}
}

// OriginPatterns turns the web app's origin (CORS_ORIGIN, e.g.
// http://localhost:3000) into the host pattern WebSocket upgrades are
// checked against. The API's own origin is always allowed.
func OriginPatterns(corsOrigin string) []string {
	u, err := url.Parse(corsOrigin)
	if err != nil || u.Host == "" {
		return nil
	}
	return []string{u.Host}
}

// Notifier is told about doorbells. The Hub is one.
type Notifier interface {
	Ring(projectID uuid.UUID, seq int64)
	ProjectChanged(projectID uuid.UUID)
}

// Listen relays every instance's doorbells from Redis to n until ctx is
// done. go-redis resubscribes by itself when Redis comes back; meanwhile
// the hub's poll keeps events flowing.
func Listen(ctx context.Context, rdb *redis.Client, n Notifier) {
	ps := rdb.PSubscribe(ctx, projectChannelPrefix+"*")
	defer ps.Close()
	if err := ps.Subscribe(ctx, projectsChannel); err != nil {
		slog.Warn("subscribe to project list doorbells; retrying in the background", "err", err)
	}
	msgs := ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-msgs:
			if !ok {
				return
			}
			var d doorbell
			if err := json.Unmarshal([]byte(m.Payload), &d); err != nil {
				slog.Warn("ignoring a malformed doorbell", "channel", m.Channel, "err", err)
				continue
			}
			if m.Channel == projectsChannel {
				n.ProjectChanged(d.ProjectID)
			} else {
				n.Ring(d.ProjectID, d.Seq)
			}
		}
	}
}
