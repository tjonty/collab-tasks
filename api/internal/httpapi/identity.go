package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/changes"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

type actorKey struct{}

var errUnknownUser = &changes.Error{
	Code:    apigen.ErrorCodeUnknownUser,
	Message: "X-User-Id must be the id of a user created with POST /users",
}

// identify resolves X-User-Id to an existing user for every operation except
// CreateUser, and rejects the request with unknown_user otherwise. There is
// no login: this identifies the caller, it doesn't authenticate them.
func identify(q *store.Queries) apigen.StrictMiddlewareFunc {
	return func(next apigen.StrictHandlerFunc, operationID string) apigen.StrictHandlerFunc {
		if operationID == "CreateUser" {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			id, err := uuid.Parse(r.Header.Get("X-User-Id"))
			if err != nil {
				return nil, errUnknownUser
			}
			exists, err := q.UserExists(ctx, id)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, errUnknownUser
			}
			return next(context.WithValue(ctx, actorKey{}, id), w, r, req)
		}
	}
}

// actor is the caller identify resolved.
func actor(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(actorKey{}).(uuid.UUID)
	return id
}
