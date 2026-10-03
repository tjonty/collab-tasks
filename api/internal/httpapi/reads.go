package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/domain"
	"github.com/tjonty/collab-tasks/api/internal/realtime"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// Page sizes.
const (
	defaultTaskPage    = 500
	maxTaskPage        = 500
	defaultCommentPage = 50
	maxCommentPage     = 100
	maxEventPage       = 1000
)

func (s *server) ListUsers(ctx context.Context, _ apigen.ListUsersRequestObject) (apigen.ListUsersResponseObject, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make(apigen.ListUsers200JSONResponse, len(rows))
	for i, u := range rows {
		users[i] = apigen.User{Id: u.ID, DisplayName: u.DisplayName}
	}
	return users, nil
}

func (s *server) ListProjects(ctx context.Context, _ apigen.ListProjectsRequestObject) (apigen.ListProjectsResponseObject, error) {
	rows, err := s.q.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	projects := make(apigen.ListProjects200JSONResponse, len(rows))
	for i, p := range rows {
		projects[i] = apigen.ProjectSummary{Id: p.ID, Name: p.Name, Description: p.Description, Seq: p.Seq}
	}
	return projects, nil
}

func (s *server) GetProject(ctx context.Context, req apigen.GetProjectRequestObject) (apigen.GetProjectResponseObject, error) {
	row, err := s.q.GetProject(ctx, req.ProjectId)
	if errors.Is(err, pgx.ErrNoRows) {
		return apigen.GetProject404JSONResponse{NotFoundJSONResponse: projectNotFound(req.ProjectId)}, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := projectView(row)
	if err != nil {
		return nil, err
	}
	return apigen.GetProject200JSONResponse(p), nil
}

func (s *server) ListTasks(ctx context.Context, req apigen.ListTasksRequestObject) (apigen.ListTasksResponseObject, error) {
	limit, err := pageSize("limit", req.Params.Limit, defaultTaskPage, maxTaskPage)
	if err != nil {
		return nil, err
	}
	after := uuid.Nil
	if req.Params.After != nil {
		after = *req.Params.After
	}

	// The seq and the page come from one snapshot, so the page shows the
	// project exactly as of that seq.
	var seq int64
	var rows []store.ListTaskSummariesRow
	err = s.snapshot(ctx, func(q *store.Queries) error {
		var err error
		if seq, err = q.ProjectSeq(ctx, req.ProjectId); err != nil {
			return err
		}
		// One row past the page tells whether another page follows.
		rows, err = q.ListTaskSummaries(ctx, store.ListTaskSummariesParams{
			ProjectID: req.ProjectId, After: after, PageSize: limit + 1,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return apigen.ListTasks404JSONResponse{NotFoundJSONResponse: projectNotFound(req.ProjectId)}, nil
	}
	if err != nil {
		return nil, err
	}

	rows, more := trimPage(rows, limit)
	page := apigen.TaskPage{Tasks: make([]apigen.TaskSummary, 0, len(rows)), Seq: seq}
	if more {
		page.NextCursor = &rows[len(rows)-1].ID
	}
	for _, r := range rows {
		page.Tasks = append(page.Tasks, apigen.TaskSummary{
			Id:           r.ID,
			Title:        r.Title,
			Status:       apigen.Status(r.Status),
			Priority:     apigen.Priority(r.Priority),
			AssignedTo:   r.Assignees,
			Tags:         r.Tags,
			Dependencies: r.Dependencies,
			UpdatedSeq:   r.UpdatedSeq,
		})
	}
	return apigen.ListTasks200JSONResponse(page), nil
}

func (s *server) GetTask(ctx context.Context, req apigen.GetTaskRequestObject) (apigen.GetTaskResponseObject, error) {
	// One statement, so the dependencies match the row's updatedSeq.
	t, err := s.q.GetTaskDetail(ctx, req.TaskId)
	if errors.Is(err, pgx.ErrNoRows) {
		return apigen.GetTask404JSONResponse{NotFoundJSONResponse: taskNotFound(req.TaskId)}, nil
	}
	if err != nil {
		return nil, err
	}
	var customFields apigen.CustomFields
	if err := json.Unmarshal(t.CustomFields, &customFields); err != nil {
		return nil, err
	}
	return apigen.GetTask200JSONResponse{
		Id:         t.ID,
		ProjectId:  t.ProjectID,
		Title:      t.Title,
		Status:     apigen.Status(t.Status),
		AssignedTo: t.Assignees,
		Configuration: apigen.TaskConfiguration{
			Priority:     apigen.Priority(t.Priority),
			Description:  t.Description,
			Tags:         t.Tags,
			CustomFields: customFields,
		},
		Dependencies: t.Dependencies,
		UpdatedSeq:   t.UpdatedSeq,
	}, nil
}

func (s *server) ListComments(ctx context.Context, req apigen.ListCommentsRequestObject) (apigen.ListCommentsResponseObject, error) {
	limit, err := pageSize("limit", req.Params.Limit, defaultCommentPage, maxCommentPage)
	if err != nil {
		return nil, err
	}
	afterSeq, err := seqCursor("afterSeq", req.Params.AfterSeq)
	if err != nil {
		return nil, err
	}
	exists, err := s.q.TaskIDExists(ctx, req.TaskId)
	if err != nil {
		return nil, err
	}
	if !exists {
		return apigen.ListComments404JSONResponse{NotFoundJSONResponse: taskNotFound(req.TaskId)}, nil
	}
	rows, err := s.q.ListComments(ctx, store.ListCommentsParams{TaskID: req.TaskId, AfterSeq: afterSeq, PageSize: limit + 1})
	if err != nil {
		return nil, err
	}

	rows, more := trimPage(rows, limit)
	page := apigen.CommentPage{Comments: make([]apigen.Comment, 0, len(rows))}
	if more {
		page.NextCursor = &rows[len(rows)-1].Seq
	}
	for _, c := range rows {
		page.Comments = append(page.Comments, apigen.Comment{
			Id: c.ID, TaskId: c.TaskID, Content: c.Content, Author: c.AuthorID, Timestamp: c.CreatedAt, Seq: c.Seq,
		})
	}
	return apigen.ListComments200JSONResponse(page), nil
}

func (s *server) ListEvents(ctx context.Context, req apigen.ListEventsRequestObject) (apigen.ListEventsResponseObject, error) {
	limit, err := pageSize("limit", req.Params.Limit, maxEventPage, maxEventPage)
	if err != nil {
		return nil, err
	}
	after, err := seqCursor("after", req.Params.After)
	if err != nil {
		return nil, err
	}
	if _, err := s.q.ProjectSeq(ctx, req.ProjectId); errors.Is(err, pgx.ErrNoRows) {
		return apigen.ListEvents404JSONResponse{NotFoundJSONResponse: projectNotFound(req.ProjectId)}, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := s.q.ListEvents(ctx, store.ListEventsParams{ProjectID: req.ProjectId, AfterSeq: after, PageSize: limit + 1})
	if err != nil {
		return nil, err
	}

	rows, more := trimPage(rows, limit)
	page := apigen.EventPage{Events: make([]apigen.Event, 0, len(rows))}
	if more {
		page.NextCursor = &rows[len(rows)-1].Seq
	}
	for _, r := range rows {
		e, err := realtime.EventView(req.ProjectId, r)
		if err != nil {
			return nil, err
		}
		page.Events = append(page.Events, e)
	}
	return apigen.ListEvents200JSONResponse(page), nil
}

// snapshot runs fn in a read-only REPEATABLE READ transaction, so every
// query in fn sees the database as of the same moment.
func (s *server) snapshot(ctx context.Context, fn func(q *store.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// pageSize reads an optional page-size parameter: def when it's absent,
// otherwise it must be between 1 and most.
func pageSize(field string, limit *int32, def, most int32) (int32, error) {
	if limit == nil {
		return def, nil
	}
	if *limit < 1 || *limit > most {
		return 0, &domain.ValidationError{Field: field, Message: fmt.Sprintf("must be between 1 and %d", most)}
	}
	return *limit, nil
}

// trimPage cuts rows, fetched with a limit of limit+1, back to limit and
// reports whether the extra row was there: that is, whether another page
// follows.
func trimPage[T any](rows []T, limit int32) ([]T, bool) {
	if len(rows) > int(limit) {
		return rows[:limit], true
	}
	return rows, false
}

// seqCursor reads an optional seq cursor: 0 (the start) when it's absent.
func seqCursor(field string, after *int64) (int64, error) {
	if after == nil {
		return 0, nil
	}
	if *after < 0 {
		return 0, &domain.ValidationError{Field: field, Message: "must be 0 or more"}
	}
	return *after, nil
}

func taskNotFound(id uuid.UUID) apigen.NotFoundJSONResponse {
	return apigen.NotFoundJSONResponse{
		Code:    apigen.ErrorCodeTaskNotFound,
		Message: fmt.Sprintf("task %s does not exist", id),
	}
}

func projectNotFound(id uuid.UUID) apigen.NotFoundJSONResponse {
	return apigen.NotFoundJSONResponse{
		Code:    apigen.ErrorCodeProjectNotFound,
		Message: fmt.Sprintf("project %s does not exist", id),
	}
}

// projectView converts a stored project to the contract's shape.
func projectView(p store.GetProjectRow) (apigen.Project, error) {
	var metadata apigen.Metadata
	if err := json.Unmarshal(p.Metadata, &metadata); err != nil {
		return apigen.Project{}, err
	}
	return apigen.Project{Id: p.ID, Name: p.Name, Description: p.Description, Metadata: metadata, Seq: p.Seq}, nil
}
