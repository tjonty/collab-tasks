package httpapi

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/changes"
	"github.com/tjonty/collab-tasks/api/internal/domain"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// server implements the contract's operations. Handlers return errors
// instead of error responses, and respondError turns them into
// {code, message}; the exception is a read's 404, which is a typed response.
type server struct {
	pool    *pgxpool.Pool
	q       *store.Queries
	changes *changes.Service
}

var _ apigen.StrictServerInterface = (*server)(nil)

func (s *server) CreateUser(ctx context.Context, req apigen.CreateUserRequestObject) (apigen.CreateUserResponseObject, error) {
	if err := domain.ValidateDisplayName(req.Body.DisplayName); err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	u, err := s.q.InsertUser(ctx, store.InsertUserParams{ID: id, DisplayName: req.Body.DisplayName})
	if err != nil {
		return nil, err
	}
	return apigen.CreateUser201JSONResponse{Id: u.ID, DisplayName: u.DisplayName}, nil
}

func (s *server) CreateProject(ctx context.Context, req apigen.CreateProjectRequestObject) (apigen.CreateProjectResponseObject, error) {
	b := req.Body
	if b.Id == uuid.Nil {
		return nil, &domain.ValidationError{Field: "id", Message: "is required"}
	}
	if err := domain.ValidateProjectName(b.Name); err != nil {
		return nil, err
	}
	var description string
	if b.Description != nil {
		description = *b.Description
	}
	if err := domain.ValidateProjectDescription(description); err != nil {
		return nil, err
	}
	metadata := apigen.Metadata{}
	if b.Metadata != nil {
		metadata = *b.Metadata
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	err = s.q.InsertProject(ctx, store.InsertProjectParams{ID: b.Id, Name: b.Name, Description: description, Metadata: rawMetadata})
	if err != nil {
		return nil, err
	}
	// Read back rather than echo the request: a retry gets the project as
	// it was first created.
	row, err := s.q.GetProject(ctx, b.Id)
	if err != nil {
		return nil, err
	}
	p, err := projectView(row)
	if err != nil {
		return nil, err
	}
	return apigen.CreateProject201JSONResponse(p), nil
}

func (s *server) SubmitChange(ctx context.Context, req apigen.SubmitChangeRequestObject) (apigen.SubmitChangeResponseObject, error) {
	if req.Body.ChangeId == uuid.Nil {
		return nil, &domain.ValidationError{Field: "changeId", Message: "is required"}
	}
	seq, err := s.changes.Submit(ctx, req.ProjectId, actor(ctx), req.Body.ChangeId, req.Body.Change)
	if err != nil {
		return nil, err
	}
	return apigen.SubmitChange200JSONResponse{Seq: seq}, nil
}
