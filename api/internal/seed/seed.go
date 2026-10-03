package seed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/changes"
	"github.com/tjonty/collab-tasks/api/internal/domain"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// Result is what Run created.
type Result struct {
	ProjectID uuid.UUID
	Users     []uuid.UUID // by Plan.Users index
	Tasks     []uuid.UUID // by Plan.Tasks index
	Changes   int         // events written
}

// Run creates the plan's users and project, then applies every task, status
// move and comment as a change through changes.Service, as the API would:
// one transaction and one event each, every rule checked. progress, if not
// nil, hears about each change.
func Run(ctx context.Context, pool *pgxpool.Pool, plan Plan, progress func(done, total int)) (Result, error) {
	q := store.New(pool)
	res := Result{
		ProjectID: newID(),
		Users:     make([]uuid.UUID, len(plan.Users)),
		Tasks:     make([]uuid.UUID, len(plan.Tasks)),
	}
	for i, name := range plan.Users {
		res.Users[i] = newID()
		if _, err := q.InsertUser(ctx, store.InsertUserParams{ID: res.Users[i], DisplayName: name}); err != nil {
			return res, fmt.Errorf("create user %s: %w", name, err)
		}
	}
	metadata, _ := json.Marshal(map[string]any{"seeded": true})
	err := q.InsertProject(ctx, store.InsertProjectParams{
		ID: res.ProjectID, Name: plan.Project.Name, Description: plan.Project.Description, Metadata: metadata,
	})
	if err != nil {
		return res, fmt.Errorf("create project: %w", err)
	}
	for i := range res.Tasks {
		res.Tasks[i] = newID()
	}

	svc := changes.NewService(pool, nil)
	total := len(plan.Tasks) + len(plan.Comments)
	for _, t := range plan.Tasks {
		total += len(movesTo(t.Status))
	}
	submit := func(actor uuid.UUID, change apigen.Change) error {
		if _, err := svc.Submit(ctx, res.ProjectID, actor, newID(), change); err != nil {
			return err
		}
		res.Changes++
		if progress != nil {
			progress(res.Changes, total)
		}
		return nil
	}

	// Every task, created in todo with all its fields.
	for i, t := range plan.Tasks {
		var c apigen.Change
		_ = c.FromTaskCreate(apigen.TaskCreate{
			Id:    res.Tasks[i],
			Title: t.Title,
			Configuration: &apigen.TaskCreateConfiguration{
				Priority:     &t.Priority,
				Description:  &t.Description,
				Tags:         &t.Tags,
				CustomFields: (*apigen.CustomFields)(&t.CustomFields),
			},
			AssignedTo:   pickIDs(res.Users, t.Assignees),
			Dependencies: pickIDs(res.Tasks, t.Dependencies),
		})
		if err := submit(res.Users[i%len(res.Users)], c); err != nil {
			return res, fmt.Errorf("create task %d: %w", i, err)
		}
	}
	// Then the status moves, in task order, so a task's dependencies are done
	// before it moves to done.
	for i, t := range plan.Tasks {
		for _, to := range movesTo(t.Status) {
			var c apigen.Change
			_ = c.FromTaskSetStatus(apigen.TaskSetStatus{TaskId: res.Tasks[i], To: to})
			if err := submit(res.Users[i%len(res.Users)], c); err != nil {
				return res, fmt.Errorf("move task %d to %s: %w", i, to, err)
			}
		}
	}
	for i, cp := range plan.Comments {
		var c apigen.Change
		_ = c.FromCommentCreate(apigen.CommentCreate{Id: newID(), TaskId: res.Tasks[cp.Task], Content: cp.Content})
		if err := submit(res.Users[cp.Author], c); err != nil {
			return res, fmt.Errorf("comment %d: %w", i, err)
		}
	}
	return res, nil
}

// movesTo is the shortest walk through the workflow from todo to s.
func movesTo(s domain.Status) []apigen.Status {
	switch s {
	case domain.StatusInProgress:
		return []apigen.Status{apigen.InProgress}
	case domain.StatusInReview:
		return []apigen.Status{apigen.InProgress, apigen.InReview}
	case domain.StatusDone:
		return []apigen.Status{apigen.InProgress, apigen.Done}
	}
	return nil
}

func pickIDs(ids []uuid.UUID, indexes []int) *[]uuid.UUID {
	if len(indexes) == 0 {
		return nil
	}
	out := make([]uuid.UUID, len(indexes))
	for i, x := range indexes {
		out[i] = ids[x]
	}
	return &out
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
