package realtime

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// pgStore is the hub's Store on Postgres.
type pgStore struct {
	q *store.Queries
}

// NewPGStore returns a Store that reads Postgres.
func NewPGStore(pool *pgxpool.Pool) Store { return pgStore{q: store.New(pool)} }

func (s pgStore) ProjectSeq(ctx context.Context, projectID uuid.UUID) (int64, error) {
	seq, err := s.q.ProjectSeq(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrProjectNotFound
	}
	return seq, err
}

func (s pgStore) ProjectSeqs(ctx context.Context, projectIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := s.q.ProjectSeqs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	seqs := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		seqs[r.ID] = r.Seq
	}
	return seqs, nil
}

func (s pgStore) EventsAfter(ctx context.Context, projectID uuid.UUID, after int64, limit int32) ([]apigen.Event, error) {
	rows, err := s.q.ListEvents(ctx, store.ListEventsParams{ProjectID: projectID, AfterSeq: after, PageSize: limit})
	if err != nil {
		return nil, err
	}
	events := make([]apigen.Event, len(rows))
	for i, r := range rows {
		if events[i], err = EventView(projectID, r); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (s pgStore) ProjectSummary(ctx context.Context, projectID uuid.UUID) (apigen.ProjectSummary, error) {
	p, err := s.q.GetProject(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apigen.ProjectSummary{}, ErrProjectNotFound
	}
	if err != nil {
		return apigen.ProjectSummary{}, err
	}
	return apigen.ProjectSummary{Id: p.ID, Name: p.Name, Description: p.Description, Seq: p.Seq}, nil
}

func (s pgStore) UserExists(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.q.UserExists(ctx, userID)
}

// EventView is a logged event as the contract shows it. The WebSocket and
// GET /projects/{id}/events both use it, so they serve the same objects.
func EventView(projectID uuid.UUID, r store.ListEventsRow) (apigen.Event, error) {
	var change apigen.Change
	if err := change.UnmarshalJSON(r.Payload); err != nil {
		return apigen.Event{}, err
	}
	return apigen.Event{
		ProjectId: projectID,
		Seq:       r.Seq,
		ChangeId:  r.ChangeID,
		ActorId:   r.ActorID,
		CreatedAt: r.CreatedAt,
		Touches:   r.Touches,
		Change:    change,
	}, nil
}
