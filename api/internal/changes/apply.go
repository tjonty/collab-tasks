package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/domain"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// applier applies one validated change inside the pipeline's transaction,
// with the project row already locked.
type applier struct {
	q         *store.Queries
	projectID uuid.UUID
	actorID   uuid.UUID
	seq       int64
}

// apply checks the change against the current state and writes it. It
// returns the id the event is about and every task whose row it wrote.
func (a applier) apply(ctx context.Context, change any) (entityID uuid.UUID, touches []uuid.UUID, err error) {
	switch c := change.(type) {
	case apigen.ProjectUpdate:
		return a.projectID, nil, a.updateProject(ctx, c)
	case apigen.TaskCreate:
		return c.Id, []uuid.UUID{c.Id}, a.createTask(ctx, c)
	case apigen.TaskUpdate:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.updateTask(ctx, c)
	case apigen.TaskSetStatus:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.setStatus(ctx, c)
	case apigen.TaskDelete:
		touches, err := a.deleteTask(ctx, c)
		return c.TaskId, touches, err
	case apigen.TaskAddAssignee:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.addAssignee(ctx, c)
	case apigen.TaskRemoveAssignee:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.editTask(ctx, c.TaskId, func(t *store.GetTaskRow) error {
			t.Assignees = without(t.Assignees, c.UserId)
			return nil
		})
	case apigen.TaskAddTag:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.editTask(ctx, c.TaskId, func(t *store.GetTaskRow) error {
			if !slices.Contains(t.Tags, c.Tag) && len(t.Tags) >= domain.MaxTags {
				return reject(apigen.ErrorCodeLimitExceeded, "the task already has %d tags, max %d", len(t.Tags), domain.MaxTags)
			}
			t.Tags = with(t.Tags, c.Tag)
			return nil
		})
	case apigen.TaskRemoveTag:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.editTask(ctx, c.TaskId, func(t *store.GetTaskRow) error {
			t.Tags = without(t.Tags, c.Tag)
			return nil
		})
	case apigen.TaskAddDependency:
		return c.TaskId, []uuid.UUID{c.TaskId}, a.addDependency(ctx, c)
	case apigen.TaskRemoveDependency:
		if _, err := a.getTask(ctx, c.TaskId); err != nil {
			return uuid.Nil, nil, err
		}
		err := a.q.DeleteDependency(ctx, store.DeleteDependencyParams{TaskID: c.TaskId, DependsOnID: c.DependsOnId})
		return c.TaskId, []uuid.UUID{c.TaskId}, err
	case apigen.CommentCreate:
		// A comment lives outside the task row, so it touches no task.
		return c.Id, nil, a.createComment(ctx, c)
	}
	return uuid.Nil, nil, fmt.Errorf("apply: unhandled change %T", change)
}

func (a applier) updateProject(ctx context.Context, c apigen.ProjectUpdate) error {
	p, err := a.q.GetProject(ctx, a.projectID)
	if err != nil {
		return err
	}
	params := store.SaveProjectParams{ID: p.ID, Name: p.Name, Description: p.Description, Metadata: p.Metadata}
	if c.Name != nil {
		params.Name = *c.Name
	}
	if c.Description != nil {
		params.Description = *c.Description
	}
	if c.Metadata != nil {
		var current map[string]any
		if err := json.Unmarshal(p.Metadata, &current); err != nil {
			return err
		}
		if params.Metadata, err = json.Marshal(domain.MergePatch(current, *c.Metadata)); err != nil {
			return err
		}
	}
	return a.q.SaveProject(ctx, params)
}

func (a applier) createTask(ctx context.Context, c apigen.TaskCreate) error {
	taken, err := a.q.TaskIDExists(ctx, c.Id)
	if err != nil {
		return err
	}
	if taken {
		return invalid("id", "is already used by another task")
	}

	params := store.InsertTaskParams{
		ID:           c.Id,
		ProjectID:    a.projectID,
		Title:        c.Title,
		Priority:     store.TaskPriorityMedium,
		Tags:         []string{},
		CustomFields: []byte("{}"),
		Assignees:    []uuid.UUID{},
	}
	if conf := c.Configuration; conf != nil {
		if conf.Priority != nil {
			params.Priority = store.TaskPriority(*conf.Priority)
		}
		if conf.Description != nil {
			params.Description = *conf.Description
		}
		if conf.Tags != nil {
			params.Tags = *conf.Tags
		}
		if conf.CustomFields != nil {
			if params.CustomFields, err = json.Marshal(*conf.CustomFields); err != nil {
				return err
			}
		}
	}
	if c.AssignedTo != nil {
		if err := a.requireUsers(ctx, "assignedTo", *c.AssignedTo); err != nil {
			return err
		}
		params.Assignees = *c.AssignedTo
	}
	var deps []uuid.UUID
	if c.Dependencies != nil {
		deps = *c.Dependencies
		if err := a.requireTasks(ctx, deps); err != nil {
			return err
		}
	}

	if err := a.q.InsertTask(ctx, params); err != nil {
		return err
	}
	for _, dep := range deps {
		err := a.q.InsertDependency(ctx, store.InsertDependencyParams{ProjectID: a.projectID, TaskID: c.Id, DependsOnID: dep})
		if err != nil {
			return err
		}
	}
	return nil
}

func (a applier) updateTask(ctx context.Context, c apigen.TaskUpdate) error {
	t, err := a.getTask(ctx, c.TaskId)
	if err != nil {
		return err
	}
	if c.Title != nil {
		t.Title = *c.Title
	}
	if c.Priority != nil {
		t.Priority = store.TaskPriority(*c.Priority)
	}
	if c.Description != nil {
		t.Description = *c.Description
	}
	if c.CustomFields != nil {
		var current map[string]any
		if err := json.Unmarshal(t.CustomFields, &current); err != nil {
			return err
		}
		merged := domain.MergePatch(current, patchMap(*c.CustomFields))
		if len(merged) > domain.MaxCustomFields {
			return reject(apigen.ErrorCodeLimitExceeded,
				"the task would have %d custom fields, max %d", len(merged), domain.MaxCustomFields)
		}
		if t.CustomFields, err = json.Marshal(merged); err != nil {
			return err
		}
	}
	return a.saveTask(ctx, t)
}

func (a applier) addAssignee(ctx context.Context, c apigen.TaskAddAssignee) error {
	return a.editTask(ctx, c.TaskId, func(t *store.GetTaskRow) error {
		if err := a.requireUsers(ctx, "userId", []uuid.UUID{c.UserId}); err != nil {
			return err
		}
		t.Assignees = with(t.Assignees, c.UserId)
		return nil
	})
}

func (a applier) addDependency(ctx context.Context, c apigen.TaskAddDependency) error {
	if _, err := a.getTask(ctx, c.TaskId); err != nil {
		return err
	}
	if err := a.requireTasks(ctx, []uuid.UUID{c.DependsOnId}); err != nil {
		return err
	}
	// The project lock makes this check and the insert atomic: two requests
	// adding A→B and B→A can't both pass it.
	cycle, err := a.q.DependencyWouldCycle(ctx, store.DependencyWouldCycleParams{TaskID: c.TaskId, DependsOnID: c.DependsOnId})
	if err != nil {
		return err
	}
	if cycle {
		return reject(apigen.ErrorCodeDependencyCycle, "task %s already depends on task %s", c.DependsOnId, c.TaskId)
	}
	return a.q.InsertDependency(ctx, store.InsertDependencyParams{ProjectID: a.projectID, TaskID: c.TaskId, DependsOnID: c.DependsOnId})
}

func (a applier) createComment(ctx context.Context, c apigen.CommentCreate) error {
	if _, err := a.getTask(ctx, c.TaskId); err != nil {
		return err
	}
	taken, err := a.q.CommentIDExists(ctx, c.Id)
	if err != nil {
		return err
	}
	if taken {
		return invalid("id", "is already used by another comment")
	}
	return a.q.InsertComment(ctx, store.InsertCommentParams{
		ID:        c.Id,
		TaskID:    c.TaskId,
		ProjectID: a.projectID,
		AuthorID:  a.actorID,
		Content:   c.Content,
		Seq:       a.seq,
	})
}

func (a applier) setStatus(ctx context.Context, c apigen.TaskSetStatus) error {
	t, err := a.getTask(ctx, c.TaskId)
	if err != nil {
		return err
	}
	rows, err := a.q.DependencyStatuses(ctx, c.TaskId)
	if err != nil {
		return err
	}
	deps := make([]domain.Status, len(rows))
	open := 0
	for i, s := range rows {
		deps[i] = domain.Status(s)
		if deps[i] != domain.StatusDone {
			open++
		}
	}
	switch err := domain.CanMoveTo(domain.Status(t.Status), domain.Status(c.To), deps); {
	case errors.Is(err, domain.ErrInvalidTransition):
		return reject(apigen.ErrorCodeInvalidTransition, "a task can't move from %s to %s", t.Status, c.To)
	case errors.Is(err, domain.ErrDependenciesNotDone):
		return reject(apigen.ErrorCodeDependenciesNotDone, "%d of the task's dependencies aren't done", open)
	case err != nil:
		return err
	}
	t.Status = store.TaskStatus(c.To)
	return a.saveTask(ctx, t)
}

// deleteTask removes the task; the schema cascades its comments and links.
// It returns the task and every task that lost a dependency on it.
func (a applier) deleteTask(ctx context.Context, c apigen.TaskDelete) ([]uuid.UUID, error) {
	if _, err := a.getTask(ctx, c.TaskId); err != nil {
		return nil, err
	}
	dependents, err := a.q.DependentIDs(ctx, c.TaskId)
	if err != nil {
		return nil, err
	}
	if err := a.q.DeleteTask(ctx, store.DeleteTaskParams{ProjectID: a.projectID, ID: c.TaskId}); err != nil {
		return nil, err
	}
	return append([]uuid.UUID{c.TaskId}, dependents...), nil
}

// getTask loads a task of this project, or rejects with task_not_found.
func (a applier) getTask(ctx context.Context, id uuid.UUID) (store.GetTaskRow, error) {
	t, err := a.q.GetTask(ctx, store.GetTaskParams{ProjectID: a.projectID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return t, reject(apigen.ErrorCodeTaskNotFound, "task %s does not exist in this project", id)
	}
	return t, err
}

// editTask loads a task, lets edit change it, and saves it.
func (a applier) editTask(ctx context.Context, id uuid.UUID, edit func(*store.GetTaskRow) error) error {
	t, err := a.getTask(ctx, id)
	if err != nil {
		return err
	}
	if err := edit(&t); err != nil {
		return err
	}
	return a.saveTask(ctx, t)
}

func (a applier) saveTask(ctx context.Context, t store.GetTaskRow) error {
	return a.q.SaveTask(ctx, store.SaveTaskParams{
		ProjectID:    a.projectID,
		ID:           t.ID,
		Title:        t.Title,
		Status:       t.Status,
		Priority:     t.Priority,
		Description:  t.Description,
		Tags:         t.Tags,
		CustomFields: t.CustomFields,
		Assignees:    t.Assignees,
	})
}

// requireTasks rejects with task_not_found unless every id is a task in this
// project. ids must be distinct, which validate guarantees.
func (a applier) requireTasks(ctx context.Context, ids []uuid.UUID) error {
	n, err := a.q.CountProjectTasks(ctx, store.CountProjectTasksParams{ProjectID: a.projectID, Ids: ids})
	if err != nil {
		return err
	}
	if int(n) != len(ids) {
		return reject(apigen.ErrorCodeTaskNotFound, "%d of the listed tasks don't exist in this project", len(ids)-int(n))
	}
	return nil
}

// requireUsers fails validation unless every id is a user. Users are never
// deleted, so an unknown id is a client mistake, not a race.
func (a applier) requireUsers(ctx context.Context, field string, ids []uuid.UUID) error {
	n, err := a.q.CountUsers(ctx, ids)
	if err != nil {
		return err
	}
	if int(n) != len(ids) {
		return invalid(field, "names a user that doesn't exist")
	}
	return nil
}

// with returns items plus item, unless it's already there. Sets keep their
// insertion order, so every client shows them the same way.
func with[T comparable](items []T, item T) []T {
	if slices.Contains(items, item) {
		return items
	}
	return append(items, item)
}

// without returns items minus item.
func without[T comparable](items []T, item T) []T {
	return slices.DeleteFunc(slices.Clone(items), func(x T) bool { return x == item })
}
