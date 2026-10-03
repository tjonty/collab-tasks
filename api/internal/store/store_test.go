package store

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/tjonty/collab-tasks/api/db"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

// pool points at one migrated Postgres container shared by every test in the
// package. Tests use fresh random ids, so they don't see each other's rows.
var pool *pgxpool.Pool

func TestMain(m *testing.M) { testdb.Main(m, &pool, Migrate) }

// Postgres error codes the schema is expected to raise.
const (
	foreignKeyViolation = "23503"
	uniqueViolation     = "23505"
	checkViolation      = "23514"
	invalidEnumValue    = "22P02"
	raiseException      = "P0001"
)

func exec(t *testing.T, sql string, args ...any) error {
	t.Helper()
	return testdb.Exec(t, pool, sql, args...)
}

func mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	testdb.MustExec(t, pool, sql, args...)
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	return testdb.Count(t, pool, sql, args...)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	testdb.WantCode(t, err, code)
}

func newUser(t *testing.T) uuid.UUID    { t.Helper(); return testdb.User(t, pool) }
func newProject(t *testing.T) uuid.UUID { t.Helper(); return testdb.Project(t, pool) }

func newTask(t *testing.T, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	return testdb.Task(t, pool, projectID)
}

func addDep(t *testing.T, projectID, taskID, dependsOnID uuid.UUID) error {
	t.Helper()
	return testdb.Dependency(t, pool, projectID, taskID, dependsOnID)
}

func TestMigrate_IsIdempotent(t *testing.T) {
	// The API migrates on every start, so a second run must be a no-op.
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second Migrate = %v, want nil", err)
	}
}

// rollBackAll undoes every migration but leaves goose's version table, the
// state a database is in when a deploy brings a new pending migration.
func rollBackAll(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB := stdlib.OpenDBFromPool(p)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}

func TestMigrate_ConcurrentStartsAllSucceed(t *testing.T) {
	// Several API instances may start at once while a migration is pending.
	ctx := context.Background()
	name := "concurrent_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	mustExec(t, "CREATE DATABASE "+name)

	// Each instance is its own process with its own pool.
	const instances = 5
	pools := make([]*pgxpool.Pool, instances)
	for i := range pools {
		cfg := pool.Config().Copy()
		cfg.ConnConfig.Database = name
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		if err := p.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		pools[i] = p
	}
	if err := Migrate(ctx, pools[0]); err != nil {
		t.Fatal(err)
	}
	rollBackAll(t, pools[0])

	start := make(chan struct{})
	errs := make(chan error, instances)
	for _, p := range pools {
		go func() {
			<-start
			errs <- Migrate(ctx, p)
		}()
	}
	close(start)
	for range instances {
		if err := <-errs; err != nil {
			t.Errorf("Migrate = %v, want nil", err)
		}
	}
}

func TestTask_Defaults(t *testing.T) {
	task := newTask(t, newProject(t))

	var status, priority, description string
	var tags []string
	var customFields map[string]any
	var updatedSeq int64
	err := pool.QueryRow(context.Background(),
		`SELECT status, priority, description, tags, custom_fields, updated_seq FROM tasks WHERE id = $1`, task).
		Scan(&status, &priority, &description, &tags, &customFields, &updatedSeq)
	if err != nil {
		t.Fatal(err)
	}
	if status != "todo" || priority != "medium" || description != "" || len(tags) != 0 ||
		len(customFields) != 0 || updatedSeq != 0 {
		t.Errorf("defaults = %q %q %q %v %v %d, want todo medium \"\" [] {} 0",
			status, priority, description, tags, customFields, updatedSeq)
	}
}

func TestTask_RejectsUnknownStatusAndPriority(t *testing.T) {
	project := newProject(t)
	err := exec(t, `INSERT INTO tasks (id, project_id, title, status) VALUES ($1, $2, 'T', 'archived')`,
		uuid.Must(uuid.NewV7()), project)
	wantCode(t, err, invalidEnumValue)

	err = exec(t, `INSERT INTO tasks (id, project_id, title, priority) VALUES ($1, $2, 'T', 'critical')`,
		uuid.Must(uuid.NewV7()), project)
	wantCode(t, err, invalidEnumValue)
}

func TestLengthChecks(t *testing.T) {
	project := newProject(t)
	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{"empty display name", `INSERT INTO users (id, display_name) VALUES ($1, '')`, []any{uuid.Must(uuid.NewV7())}},
		{"empty project name", `INSERT INTO projects (id, name) VALUES ($1, '')`, []any{uuid.Must(uuid.NewV7())}},
		{"empty task title", `INSERT INTO tasks (id, project_id, title) VALUES ($1, $2, '')`, []any{uuid.Must(uuid.NewV7()), project}},
		{"title over 200", `INSERT INTO tasks (id, project_id, title) VALUES ($1, $2, repeat('a', 201))`, []any{uuid.Must(uuid.NewV7()), project}},
		{"21 tags", `INSERT INTO tasks (id, project_id, title, tags) VALUES ($1, $2, 'T', array_fill('x'::text, ARRAY[21]))`, []any{uuid.Must(uuid.NewV7()), project}},
		{"custom fields not an object", `INSERT INTO tasks (id, project_id, title, custom_fields) VALUES ($1, $2, 'T', '[]')`, []any{uuid.Must(uuid.NewV7()), project}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantCode(t, exec(t, tt.sql, tt.args...), checkViolation)
		})
	}
}

func TestDependency_SameProjectOnly(t *testing.T) {
	p1, p2 := newProject(t), newProject(t)
	a, b := newTask(t, p1), newTask(t, p2)

	// Whichever project the row claims, one of the two tasks isn't in it.
	wantCode(t, addDep(t, p1, a, b), foreignKeyViolation)
	wantCode(t, addDep(t, p2, a, b), foreignKeyViolation)
}

func TestDependency_NotOnItself(t *testing.T) {
	p := newProject(t)
	a := newTask(t, p)
	wantCode(t, addDep(t, p, a, a), checkViolation)
}

func TestDependency_NoDuplicates(t *testing.T) {
	p := newProject(t)
	a, b := newTask(t, p), newTask(t, p)
	if err := addDep(t, p, a, b); err != nil {
		t.Fatal(err)
	}
	wantCode(t, addDep(t, p, a, b), uniqueViolation)
}

func TestComment_MustBeInTasksProject(t *testing.T) {
	p1, p2 := newProject(t), newProject(t)
	task := newTask(t, p1)
	err := exec(t, `INSERT INTO comments (id, task_id, project_id, author_id, content, seq) VALUES ($1, $2, $3, $4, 'hi', 1)`,
		uuid.Must(uuid.NewV7()), task, p2, newUser(t))
	wantCode(t, err, foreignKeyViolation)
}

func TestTaskDelete_Cascades(t *testing.T) {
	p := newProject(t)
	user := newUser(t)
	deleted, upstream, downstream, other := newTask(t, p), newTask(t, p), newTask(t, p), newTask(t, p)

	// downstream depends on deleted, deleted depends on upstream, and an
	// unrelated link between the other tasks must survive.
	for _, dep := range [][2]uuid.UUID{{downstream, deleted}, {deleted, upstream}, {other, upstream}} {
		if err := addDep(t, p, dep[0], dep[1]); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, `INSERT INTO comments (id, task_id, project_id, author_id, content, seq) VALUES ($1, $2, $3, $4, 'on deleted', 1)`,
		uuid.Must(uuid.NewV7()), deleted, p, user)
	mustExec(t, `INSERT INTO comments (id, task_id, project_id, author_id, content, seq) VALUES ($1, $2, $3, $4, 'on other', 2)`,
		uuid.Must(uuid.NewV7()), other, p, user)

	mustExec(t, `DELETE FROM tasks WHERE id = $1`, deleted)

	if n := count(t, `SELECT count(*) FROM task_dependencies WHERE task_id = $1 OR depends_on_id = $1`, deleted); n != 0 {
		t.Errorf("links to deleted task = %d, want 0", n)
	}
	if n := count(t, `SELECT count(*) FROM comments WHERE task_id = $1`, deleted); n != 0 {
		t.Errorf("comments on deleted task = %d, want 0", n)
	}
	if n := count(t, `SELECT count(*) FROM task_dependencies WHERE task_id = $1`, other); n != 1 {
		t.Errorf("unrelated links = %d, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM comments WHERE task_id = $1`, other); n != 1 {
		t.Errorf("unrelated comments = %d, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM tasks WHERE id = ANY($1)`, []uuid.UUID{upstream, downstream, other}); n != 3 {
		t.Errorf("surviving tasks = %d, want 3", n)
	}
}

func logEvent(t *testing.T, projectID uuid.UUID, seq int64, changeID, actorID uuid.UUID) error {
	t.Helper()
	return exec(t, `INSERT INTO events (project_id, seq, change_id, type, entity_id, payload, actor_id)
		VALUES ($1, $2, $3, 'task.create', $4, '{}', $5)`,
		projectID, seq, changeID, uuid.Must(uuid.NewV7()), actorID)
}

func TestEvents_SeqAndChangeIDAreUnique(t *testing.T) {
	p, user := newProject(t), newUser(t)
	change := uuid.Must(uuid.NewV7())
	if err := logEvent(t, p, 1, change, user); err != nil {
		t.Fatal(err)
	}
	// A retried change can't be logged twice, even under another seq.
	wantCode(t, logEvent(t, p, 2, change, user), uniqueViolation)
	// Two events can't share a seq within a project.
	wantCode(t, logEvent(t, p, 1, uuid.Must(uuid.NewV7()), user), uniqueViolation)
	// The same seq in another project is fine.
	if err := logEvent(t, newProject(t), 1, uuid.Must(uuid.NewV7()), user); err != nil {
		t.Errorf("same seq in another project: %v", err)
	}
}

func TestEvents_AreAppendOnly(t *testing.T) {
	p, user := newProject(t), newUser(t)
	if err := logEvent(t, p, 1, uuid.Must(uuid.NewV7()), user); err != nil {
		t.Fatal(err)
	}
	wantCode(t, exec(t, `UPDATE events SET type = 'x' WHERE project_id = $1`, p), raiseException)
	wantCode(t, exec(t, `DELETE FROM events WHERE project_id = $1`, p), raiseException)
	if n := count(t, `SELECT count(*) FROM events WHERE project_id = $1 AND type = 'task.create'`, p); n != 1 {
		t.Errorf("events after update/delete = %d, want 1 unchanged", n)
	}
}

func TestEvents_RecordTouchedTasks(t *testing.T) {
	p, user := newProject(t), newUser(t)
	touched := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	mustExec(t, `INSERT INTO events (project_id, seq, change_id, type, entity_id, payload, actor_id, touches)
		VALUES ($1, 1, $2, 'task.delete', $3, '{}', $4, $5)`,
		p, uuid.Must(uuid.NewV7()), touched[0], user, touched)

	var got []uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT touches FROM events WHERE project_id = $1`, p).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != touched[0] || got[1] != touched[1] {
		t.Errorf("touches = %v, want %v", got, touched)
	}

	// An event that touches no task (a comment, a project rename) stores an
	// empty list, not NULL.
	if err := logEvent(t, p, 2, uuid.Must(uuid.NewV7()), user); err != nil {
		t.Fatal(err)
	}
	if n := count(t, `SELECT cardinality(touches) FROM events WHERE project_id = $1 AND seq = 2`, p); n != 0 {
		t.Errorf("default touches has %d items, want 0", n)
	}
}
