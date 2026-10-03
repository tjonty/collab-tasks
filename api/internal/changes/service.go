// Package changes runs the write pipeline: every change to a project is
// validated, checked against the current state, applied and logged as one
// event, all in one Postgres transaction.
package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// Doorbell is told after each accepted change commits, so every API
// instance can pull the new event. It must not block, and its failures are
// its own to log: a missed ring only delays delivery until the next poll.
type Doorbell interface {
	// Ring announces that the project's log has reached seq.
	Ring(projectID uuid.UUID, seq int64)
	// ProjectChanged announces that the project's name or description,
	// as shown in the project list, changed.
	ProjectChanged(projectID uuid.UUID)
}

// Error is an expected failure with a code from the contract, such as
// project_not_found or dependency_cycle. Field-level problems are
// *domain.ValidationError instead.
type Error struct {
	Code    apigen.ErrorCode
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func reject(code apigen.ErrorCode, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Service applies changes.
type Service struct {
	pool     *pgxpool.Pool
	doorbell Doorbell
}

// NewService returns a Service that writes to pool and rings doorbell after
// each commit. doorbell may be nil.
func NewService(pool *pgxpool.Pool, doorbell Doorbell) *Service {
	return &Service{pool: pool, doorbell: doorbell}
}

// Submit applies one change to a project on behalf of actorID and returns
// the seq of the event it produced. Submitting a changeId that was already
// applied returns the original seq and changes nothing.
//
// A rejected change rolls back its seq increment, so a project's seqs have
// no gaps.
func (s *Service) Submit(ctx context.Context, projectID, actorID, changeID uuid.UUID, wire apigen.Change) (int64, error) {
	change, err := decode(wire)
	if err != nil {
		return 0, err
	}
	if err := validate(change); err != nil {
		return 0, err
	}
	changeType, err := wire.Discriminator()
	if err != nil {
		return 0, err
	}
	// The concrete type only knows the contract's fields, so anything else
	// the client sent is dropped here and never reaches the log.
	payload, err := json.Marshal(change)
	if err != nil {
		return 0, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	seq, err := q.NextProjectSeq(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, reject(apigen.ErrorCodeProjectNotFound, "project %s does not exist", projectID)
	}
	if err != nil {
		return 0, err
	}

	// Checked under the project lock, so a retry racing its original waits
	// for it and then finds its event.
	prior, err := q.EventByChangeID(ctx, changeID)
	switch {
	case err == nil:
		if prior.ProjectID != projectID {
			return 0, invalid("changeId", "was already used for another project")
		}
		return prior.Seq, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, err
	}

	a := applier{q: q, projectID: projectID, actorID: actorID, seq: seq}
	entityID, touches, err := a.apply(ctx, change)
	if err != nil {
		return 0, err
	}
	if len(touches) > 0 {
		if err := q.TouchTasks(ctx, store.TouchTasksParams{Seq: seq, ProjectID: projectID, Ids: touches}); err != nil {
			return 0, err
		}
	}
	if touches == nil {
		touches = []uuid.UUID{}
	}
	err = q.InsertEvent(ctx, store.InsertEventParams{
		ProjectID: projectID,
		Seq:       seq,
		ChangeID:  changeID,
		Type:      changeType,
		EntityID:  entityID,
		Payload:   payload,
		ActorID:   actorID,
		Touches:   touches,
	})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	if s.doorbell != nil {
		s.doorbell.Ring(projectID, seq)
		if p, ok := change.(apigen.ProjectUpdate); ok && (p.Name != nil || p.Description != nil) {
			s.doorbell.ProjectChanged(projectID)
		}
	}
	return seq, nil
}
