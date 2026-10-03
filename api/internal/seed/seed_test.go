package seed

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/domain"
	"github.com/tjonty/collab-tasks/api/internal/store"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { testdb.Main(m, &pool, store.Migrate) }

func TestRun_WritesThePlanThroughThePipeline(t *testing.T) {
	ctx := context.Background()
	plan := NewPlan(11, 200, 40)
	var reported, total int
	res, err := Run(ctx, pool, plan, func(done, all int) { reported, total = done, all })
	if err != nil {
		t.Fatal(err)
	}

	moves := 0
	for _, task := range plan.Tasks {
		moves += len(movesTo(task.Status))
	}
	wantChanges := len(plan.Tasks) + moves + len(plan.Comments)
	if res.Changes != wantChanges || reported != wantChanges || total != wantChanges {
		t.Errorf("changes = %d (progress %d of %d), want %d", res.Changes, reported, total, wantChanges)
	}

	// The project, with one event per change, numbered 1..N without gaps.
	var name string
	var seq int64
	if err := pool.QueryRow(ctx, `SELECT name, seq FROM projects WHERE id = $1`, res.ProjectID).Scan(&name, &seq); err != nil {
		t.Fatal(err)
	}
	events := testdb.Count(t, pool, `SELECT count(*) FROM events WHERE project_id = $1`, res.ProjectID)
	maxSeq := testdb.Count(t, pool, `SELECT coalesce(max(seq), 0) FROM events WHERE project_id = $1`, res.ProjectID)
	if name != plan.Project.Name || seq != int64(wantChanges) || events != wantChanges || maxSeq != wantChanges {
		t.Errorf("project %q at seq %d with %d events (max seq %d); want %q, %d", name, seq, events, maxSeq, plan.Project.Name, wantChanges)
	}

	// Every task ended where the plan says.
	for i, want := range plan.Tasks {
		var title, status string
		var tags []string
		var assignees []uuid.UUID
		err := pool.QueryRow(ctx, `SELECT title, status, tags, assignees FROM tasks WHERE project_id = $1 AND id = $2`,
			res.ProjectID, res.Tasks[i]).Scan(&title, &status, &tags, &assignees)
		if err != nil {
			t.Fatalf("task %d: %v", i, err)
		}
		wantAssignees := []uuid.UUID{}
		for _, u := range want.Assignees {
			wantAssignees = append(wantAssignees, res.Users[u])
		}
		if title != want.Title || domain.Status(status) != want.Status ||
			!reflect.DeepEqual(tags, append([]string{}, want.Tags...)) || !reflect.DeepEqual(assignees, wantAssignees) {
			t.Fatalf("task %d = %q %s %v %v, want %q %s %v %v", i, title, status, tags, assignees, want.Title, want.Status, want.Tags, wantAssignees)
		}
		var deps []uuid.UUID
		rows, _ := pool.Query(ctx, `SELECT depends_on_id FROM task_dependencies WHERE task_id = $1`, res.Tasks[i])
		for rows.Next() {
			var d uuid.UUID
			_ = rows.Scan(&d)
			deps = append(deps, d)
		}
		rows.Close()
		var wantDeps []uuid.UUID
		for _, d := range want.Dependencies {
			wantDeps = append(wantDeps, res.Tasks[d])
		}
		slices.SortFunc(deps, func(a, b uuid.UUID) int { return compare(a, b) })
		slices.SortFunc(wantDeps, func(a, b uuid.UUID) int { return compare(a, b) })
		if !reflect.DeepEqual(deps, wantDeps) {
			t.Fatalf("task %d depends on %v, want %v", i, deps, wantDeps)
		}
	}

	// Each task's updated_seq is the last event that touched it.
	stale := testdb.Count(t, pool, `SELECT count(*) FROM tasks t WHERE t.project_id = $1 AND t.updated_seq <>
		(SELECT max(e.seq) FROM events e WHERE e.project_id = t.project_id AND t.id = ANY(e.touches))`, res.ProjectID)
	if stale != 0 {
		t.Errorf("%d tasks have an updated_seq that isn't their last event", stale)
	}

	// The comments, by their planned authors.
	for _, c := range plan.Comments {
		n := testdb.Count(t, pool, `SELECT count(*) FROM comments WHERE task_id = $1 AND author_id = $2 AND content = $3`,
			res.Tasks[c.Task], res.Users[c.Author], c.Content)
		if n == 0 {
			t.Fatalf("comment %+v is missing", c)
		}
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM comments WHERE project_id = $1`, res.ProjectID); n != len(plan.Comments) {
		t.Errorf("comments = %d, want %d", n, len(plan.Comments))
	}
}

func compare(a, b uuid.UUID) int {
	switch {
	case a.String() < b.String():
		return -1
	case a.String() > b.String():
		return 1
	}
	return 0
}
