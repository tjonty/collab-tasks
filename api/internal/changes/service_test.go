package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/store"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { testdb.Main(m, &pool, store.Migrate) }

type ring struct {
	project uuid.UUID
	seq     int64
}

// bell records every ring, standing in for the Redis doorbell.
type bell struct {
	mu    sync.Mutex
	rings []ring
}

func (b *bell) Ring(projectID uuid.UUID, seq int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rings = append(b.rings, ring{projectID, seq})
}

func (b *bell) got() []ring {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ring(nil), b.rings...)
}

// wrap puts a concrete change into the wire union, setting its type.
func wrap(t *testing.T, v any) apigen.Change {
	t.Helper()
	var c apigen.Change
	var err error
	switch v := v.(type) {
	case apigen.ProjectUpdate:
		err = c.FromProjectUpdate(v)
	case apigen.TaskCreate:
		err = c.FromTaskCreate(v)
	case apigen.TaskUpdate:
		err = c.FromTaskUpdate(v)
	case apigen.TaskSetStatus:
		err = c.FromTaskSetStatus(v)
	case apigen.TaskDelete:
		err = c.FromTaskDelete(v)
	case apigen.TaskAddAssignee:
		err = c.FromTaskAddAssignee(v)
	case apigen.TaskRemoveAssignee:
		err = c.FromTaskRemoveAssignee(v)
	case apigen.TaskAddTag:
		err = c.FromTaskAddTag(v)
	case apigen.TaskRemoveTag:
		err = c.FromTaskRemoveTag(v)
	case apigen.TaskAddDependency:
		err = c.FromTaskAddDependency(v)
	case apigen.TaskRemoveDependency:
		err = c.FromTaskRemoveDependency(v)
	case apigen.CommentCreate:
		err = c.FromCommentCreate(v)
	default:
		t.Fatalf("wrap: unknown change %T", v)
	}
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// fixture is one project with one user acting on it.
type fixture struct {
	t       *testing.T
	svc     *Service
	bell    *bell
	project uuid.UUID
	actor   uuid.UUID
}

func setup(t *testing.T) *fixture {
	t.Helper()
	b := &bell{}
	return &fixture{
		t:       t,
		svc:     NewService(pool, b),
		bell:    b,
		project: testdb.Project(t, pool),
		actor:   testdb.User(t, pool),
	}
}

// submit sends v as a new change (a fresh changeId).
func (f *fixture) submit(v any) (int64, error) {
	f.t.Helper()
	return f.svc.Submit(context.Background(), f.project, f.actor, testdb.NewID(), wrap(f.t, v))
}

func (f *fixture) mustSubmit(v any) int64 {
	f.t.Helper()
	seq, err := f.submit(v)
	if err != nil {
		f.t.Fatalf("Submit(%T) = %v", v, err)
	}
	return seq
}

func (f *fixture) projectSeq() int64 {
	f.t.Helper()
	return int64(testdb.Count(f.t, pool, `SELECT seq FROM projects WHERE id = $1`, f.project))
}

// eventSeqs lists the project's logged seqs in order.
func (f *fixture) eventSeqs() []int64 {
	f.t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT seq FROM events WHERE project_id = $1 ORDER BY seq`, f.project)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			f.t.Fatal(err)
		}
		seqs = append(seqs, s)
	}
	return seqs
}

type loggedEvent struct {
	Type     string
	EntityID uuid.UUID
	ActorID  uuid.UUID
	Touches  []uuid.UUID
	Payload  map[string]any
}

func (f *fixture) event(seq int64) loggedEvent {
	f.t.Helper()
	var e loggedEvent
	var payload []byte
	err := pool.QueryRow(context.Background(),
		`SELECT type, entity_id, actor_id, touches, payload FROM events WHERE project_id = $1 AND seq = $2`,
		f.project, seq).Scan(&e.Type, &e.EntityID, &e.ActorID, &e.Touches, &payload)
	if err != nil {
		f.t.Fatalf("event %d: %v", seq, err)
	}
	if err := json.Unmarshal(payload, &e.Payload); err != nil {
		f.t.Fatal(err)
	}
	return e
}

func wantCode(t *testing.T, err error, code apigen.ErrorCode) {
	t.Helper()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *Error with code %s", err, code)
	}
	if ce.Code != code {
		t.Fatalf("code = %s (%s), want %s", ce.Code, ce.Message, code)
	}
}

func jsonValue(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSubmit_ProjectUpdate(t *testing.T) {
	f := setup(t)

	seq := f.mustSubmit(apigen.ProjectUpdate{Name: ptr("Launch v2"), Metadata: &apigen.MetadataPatch{"color": "red", "size": "S"}})
	if seq != 1 {
		t.Errorf("seq = %d, want 1", seq)
	}
	seq = f.mustSubmit(apigen.ProjectUpdate{Metadata: &apigen.MetadataPatch{"color": nil, "size": "L"}})
	if seq != 2 {
		t.Errorf("seq = %d, want 2", seq)
	}

	var name, description string
	var metadata map[string]any
	if err := pool.QueryRow(context.Background(), `SELECT name, description, metadata FROM projects WHERE id = $1`, f.project).
		Scan(&name, &description, &metadata); err != nil {
		t.Fatal(err)
	}
	// The second change didn't send a name, so the rename stands.
	if name != "Launch v2" || description != "" || !reflect.DeepEqual(metadata, map[string]any{"size": "L"}) {
		t.Errorf("project = %q %q %v, want \"Launch v2\" \"\" map[size:L]", name, description, metadata)
	}
	if got := f.projectSeq(); got != 2 {
		t.Errorf("projects.seq = %d, want 2", got)
	}

	e := f.event(1)
	if e.Type != "project.update" || e.EntityID != f.project || e.ActorID != f.actor || len(e.Touches) != 0 {
		t.Errorf("event 1 = %+v, want project.update on the project by the actor, touching no task", e)
	}
	want := jsonValue(t, `{"type":"project.update","name":"Launch v2","metadata":{"color":"red","size":"S"}}`)
	if !reflect.DeepEqual(e.Payload, want) {
		t.Errorf("payload = %v, want %v", e.Payload, want)
	}
}

func TestSubmit_UnknownProject(t *testing.T) {
	f := setup(t)
	f.project = testdb.NewID()
	_, err := f.submit(apigen.ProjectUpdate{Name: ptr("x")})
	wantCode(t, err, apigen.ErrorCodeProjectNotFound)
}

func TestSubmit_InvalidChangeUsesNoSeq(t *testing.T) {
	f := setup(t)
	_, err := f.submit(apigen.ProjectUpdate{Name: ptr("")})
	wantInvalid(t, err, "name")
	if got := f.projectSeq(); got != 0 {
		t.Errorf("projects.seq = %d, want 0", got)
	}
	if got := f.eventSeqs(); len(got) != 0 {
		t.Errorf("events = %v, want none", got)
	}
}

func TestSubmit_RetryReturnsOriginalSeq(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	changeID := testdb.NewID()
	rename := wrap(t, apigen.ProjectUpdate{Name: ptr("Once")})

	first, err := f.svc.Submit(ctx, f.project, f.actor, changeID, rename)
	if err != nil {
		t.Fatal(err)
	}
	f.mustSubmit(apigen.ProjectUpdate{Name: ptr("Later")})
	again, err := f.svc.Submit(ctx, f.project, f.actor, changeID, rename)
	if err != nil {
		t.Fatal(err)
	}

	if first != 1 || again != 1 {
		t.Errorf("seqs = %d then %d, want 1 both times", first, again)
	}
	if got := f.eventSeqs(); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Errorf("events = %v, want [1 2]", got)
	}
	// The retry must not apply the rename a second time over "Later".
	if n := testdb.Count(t, pool, `SELECT count(*) FROM projects WHERE id = $1 AND name = 'Later'`, f.project); n != 1 {
		t.Error("retry re-applied the change")
	}
	if got := f.projectSeq(); got != 2 {
		t.Errorf("projects.seq = %d, want 2", got)
	}
}

func TestSubmit_ChangeIDFromAnotherProject(t *testing.T) {
	a, b := setup(t), setup(t)
	changeID := testdb.NewID()
	rename := wrap(t, apigen.ProjectUpdate{Name: ptr("x")})
	if _, err := a.svc.Submit(context.Background(), a.project, a.actor, changeID, rename); err != nil {
		t.Fatal(err)
	}
	_, err := b.svc.Submit(context.Background(), b.project, b.actor, changeID, rename)
	wantInvalid(t, err, "changeId")
	if got := b.projectSeq(); got != 0 {
		t.Errorf("projects.seq = %d, want 0", got)
	}
}

func TestSubmit_PayloadKeepsOnlyContractFields(t *testing.T) {
	f := setup(t)
	var c apigen.Change
	if err := json.Unmarshal([]byte(`{"type":"project.update","name":"x","injected":"<script>"}`), &c); err != nil {
		t.Fatal(err)
	}
	seq, err := f.svc.Submit(context.Background(), f.project, f.actor, testdb.NewID(), c)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.event(seq).Payload, jsonValue(t, `{"type":"project.update","name":"x"}`); !reflect.DeepEqual(got, want) {
		t.Errorf("payload = %v, want %v", got, want)
	}
}

func TestSubmit_RingsDoorbellOncePerAcceptedChange(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	changeID := testdb.NewID()
	rename := wrap(t, apigen.ProjectUpdate{Name: ptr("x")})

	if _, err := f.svc.Submit(ctx, f.project, f.actor, changeID, rename); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Submit(ctx, f.project, f.actor, changeID, rename); err != nil { // retry
		t.Fatal(err)
	}
	if _, err := f.submit(apigen.ProjectUpdate{Name: ptr("")}); err == nil { // invalid
		t.Fatal("invalid change accepted")
	}

	if got, want := f.bell.got(), []ring{{f.project, 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("rings = %v, want %v", got, want)
	}
}

type taskRow struct {
	Title        string
	Status       string
	Priority     string
	Description  string
	Tags         []string
	CustomFields map[string]any
	Assignees    []uuid.UUID
	UpdatedSeq   int64
}

func (f *fixture) task(id uuid.UUID) taskRow {
	f.t.Helper()
	var r taskRow
	err := pool.QueryRow(context.Background(),
		`SELECT title, status, priority, description, tags, custom_fields, assignees, updated_seq
		 FROM tasks WHERE project_id = $1 AND id = $2`, f.project, id).
		Scan(&r.Title, &r.Status, &r.Priority, &r.Description, &r.Tags, &r.CustomFields, &r.Assignees, &r.UpdatedSeq)
	if err != nil {
		f.t.Fatalf("task %s: %v", id, err)
	}
	return r
}

// createTask creates a todo task through the pipeline and returns its id.
func (f *fixture) createTask(title string) uuid.UUID {
	f.t.Helper()
	id := testdb.NewID()
	f.mustSubmit(apigen.TaskCreate{Id: id, Title: title})
	return id
}

func (f *fixture) setStatus(id uuid.UUID, to ...apigen.Status) {
	f.t.Helper()
	for _, s := range to {
		f.mustSubmit(apigen.TaskSetStatus{TaskId: id, To: s})
	}
}

func (f *fixture) dependencies(id uuid.UUID) []uuid.UUID {
	f.t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT depends_on_id FROM task_dependencies WHERE task_id = $1 ORDER BY depends_on_id`, id)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var d uuid.UUID
		if err := rows.Scan(&d); err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, d)
	}
	return ids
}

func sorted(ids ...uuid.UUID) []uuid.UUID {
	out := append([]uuid.UUID(nil), ids...)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func TestSubmit_TaskCreate_Defaults(t *testing.T) {
	f := setup(t)
	id := testdb.NewID()
	seq := f.mustSubmit(apigen.TaskCreate{Id: id, Title: "Write tests"})

	got := f.task(id)
	want := taskRow{Title: "Write tests", Status: "todo", Priority: "medium", Description: "",
		Tags: []string{}, CustomFields: map[string]any{}, Assignees: []uuid.UUID{}, UpdatedSeq: seq}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("task = %+v, want %+v", got, want)
	}
	e := f.event(seq)
	if e.Type != "task.create" || e.EntityID != id || !reflect.DeepEqual(e.Touches, []uuid.UUID{id}) {
		t.Errorf("event = %s on %s touching %v, want task.create on %s touching [%s]", e.Type, e.EntityID, e.Touches, id, id)
	}
}

func TestSubmit_TaskCreate_Full(t *testing.T) {
	f := setup(t)
	dep := f.createTask("Design")
	id := testdb.NewID()
	seq := f.mustSubmit(apigen.TaskCreate{
		Id: id, Title: "Build",
		Configuration: &apigen.TaskCreateConfiguration{
			Priority: ptr(apigen.High), Description: ptr("Ship it"), Tags: &[]string{"api", "web"},
			CustomFields: &apigen.CustomFields{"points": 3.0, "billable": true},
		},
		AssignedTo:   &[]uuid.UUID{f.actor},
		Dependencies: &[]uuid.UUID{dep},
	})

	got := f.task(id)
	want := taskRow{Title: "Build", Status: "todo", Priority: "high", Description: "Ship it",
		Tags: []string{"api", "web"}, CustomFields: map[string]any{"points": 3.0, "billable": true},
		Assignees: []uuid.UUID{f.actor}, UpdatedSeq: seq}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("task = %+v, want %+v", got, want)
	}
	if deps := f.dependencies(id); !reflect.DeepEqual(deps, []uuid.UUID{dep}) {
		t.Errorf("dependencies = %v, want [%s]", deps, dep)
	}
}

func TestSubmit_TaskCreate_Rejections(t *testing.T) {
	f := setup(t)
	elsewhere := testdb.Task(t, pool, testdb.Project(t, pool))
	existing := testdb.Task(t, pool, f.project)

	_, err := f.submit(apigen.TaskCreate{Id: testdb.NewID(), Title: "T", Dependencies: &[]uuid.UUID{testdb.NewID()}})
	wantCode(t, err, apigen.ErrorCodeTaskNotFound)

	_, err = f.submit(apigen.TaskCreate{Id: testdb.NewID(), Title: "T", Dependencies: &[]uuid.UUID{existing, elsewhere}})
	wantCode(t, err, apigen.ErrorCodeTaskNotFound)

	_, err = f.submit(apigen.TaskCreate{Id: testdb.NewID(), Title: "T", AssignedTo: &[]uuid.UUID{f.actor, testdb.NewID()}})
	wantInvalid(t, err, "assignedTo")

	_, err = f.submit(apigen.TaskCreate{Id: elsewhere, Title: "T"})
	wantInvalid(t, err, "id")

	if got := f.projectSeq(); got != 0 {
		t.Errorf("projects.seq = %d after only rejected changes, want 0", got)
	}
}

func TestSubmit_TaskUpdate(t *testing.T) {
	f := setup(t)
	id := testdb.NewID()
	f.mustSubmit(apigen.TaskCreate{Id: id, Title: "Old", Configuration: &apigen.TaskCreateConfiguration{
		Description: ptr("Keep me"), CustomFields: &apigen.CustomFields{"a": 1.0, "b": 2.0},
	}})

	seq := f.mustSubmit(apigen.TaskUpdate{TaskId: id, Title: ptr("New"),
		CustomFields: &apigen.CustomFieldsPatch{"a": ptr[any](5.0), "b": nil, "c": ptr[any]("x")}})

	got := f.task(id)
	if got.Title != "New" || got.Description != "Keep me" || got.Priority != "medium" || got.UpdatedSeq != seq {
		t.Errorf("task = %+v, want title New, description kept, priority kept, updatedSeq %d", got, seq)
	}
	if want := map[string]any{"a": 5.0, "c": "x"}; !reflect.DeepEqual(got.CustomFields, want) {
		t.Errorf("customFields = %v, want %v", got.CustomFields, want)
	}
	if e := f.event(seq); !reflect.DeepEqual(e.Touches, []uuid.UUID{id}) {
		t.Errorf("touches = %v, want [%s]", e.Touches, id)
	}
}

func TestSubmit_TaskUpdate_CustomFieldLimit(t *testing.T) {
	f := setup(t)
	full := apigen.CustomFields{}
	for i := range 20 {
		full[fmt.Sprintf("k%d", i)] = 1.0
	}
	id := testdb.NewID()
	f.mustSubmit(apigen.TaskCreate{Id: id, Title: "T", Configuration: &apigen.TaskCreateConfiguration{CustomFields: &full}})

	_, err := f.submit(apigen.TaskUpdate{TaskId: id, CustomFields: &apigen.CustomFieldsPatch{"k20": ptr[any](1.0)}})
	wantCode(t, err, apigen.ErrorCodeLimitExceeded)

	// Swapping one key for another stays at 20.
	f.mustSubmit(apigen.TaskUpdate{TaskId: id, CustomFields: &apigen.CustomFieldsPatch{"k0": nil, "k20": ptr[any](1.0)}})
	if n := len(f.task(id).CustomFields); n != 20 {
		t.Errorf("custom fields = %d, want 20", n)
	}
}

func TestSubmit_TaskUpdate_MissingTask(t *testing.T) {
	f := setup(t)
	// A task in another project is as good as missing.
	for _, id := range []uuid.UUID{testdb.NewID(), testdb.Task(t, pool, testdb.Project(t, pool))} {
		_, err := f.submit(apigen.TaskUpdate{TaskId: id, Title: ptr("x")})
		wantCode(t, err, apigen.ErrorCodeTaskNotFound)
	}
}

func TestSubmit_SetStatus(t *testing.T) {
	f := setup(t)
	id := f.createTask("T")

	_, err := f.submit(apigen.TaskSetStatus{TaskId: id, To: apigen.Done})
	wantCode(t, err, apigen.ErrorCodeInvalidTransition)

	seq := f.mustSubmit(apigen.TaskSetStatus{TaskId: id, To: apigen.InProgress})
	if got := f.task(id); got.Status != "in_progress" || got.UpdatedSeq != seq {
		t.Errorf("task = %s at %d, want in_progress at %d", got.Status, got.UpdatedSeq, seq)
	}
	f.setStatus(id, apigen.InReview, apigen.Done, apigen.InProgress)
	if got := f.task(id).Status; got != "in_progress" {
		t.Errorf("status = %s, want in_progress after reopening", got)
	}
}

func TestSubmit_SetStatus_DoneWaitsForDependencies(t *testing.T) {
	f := setup(t)
	dep := f.createTask("Design")
	id := testdb.NewID()
	f.mustSubmit(apigen.TaskCreate{Id: id, Title: "Build", Dependencies: &[]uuid.UUID{dep}})

	// Starting work is never blocked.
	f.setStatus(id, apigen.InProgress)
	_, err := f.submit(apigen.TaskSetStatus{TaskId: id, To: apigen.Done})
	wantCode(t, err, apigen.ErrorCodeDependenciesNotDone)

	f.setStatus(dep, apigen.InProgress, apigen.Done)
	f.setStatus(id, apigen.Done)

	// Reopening the dependency leaves the dependent Done.
	f.setStatus(dep, apigen.InProgress)
	if got := f.task(id).Status; got != "done" {
		t.Errorf("status = %s, want done", got)
	}
}

func TestSubmit_TaskDelete(t *testing.T) {
	f := setup(t)
	upstream := f.createTask("Upstream")
	gone := testdb.NewID()
	f.mustSubmit(apigen.TaskCreate{Id: gone, Title: "Gone", Dependencies: &[]uuid.UUID{upstream}})
	waiterA, waiterB, unrelated := testdb.NewID(), testdb.NewID(), f.createTask("Unrelated")
	f.mustSubmit(apigen.TaskCreate{Id: waiterA, Title: "A", Dependencies: &[]uuid.UUID{gone}})
	f.mustSubmit(apigen.TaskCreate{Id: waiterB, Title: "B", Dependencies: &[]uuid.UUID{gone, upstream}})
	testdb.MustExec(t, pool, `INSERT INTO comments (id, task_id, project_id, author_id, content, seq) VALUES ($1, $2, $3, $4, 'bye', 0)`,
		testdb.NewID(), gone, f.project, f.actor)
	before := f.task(upstream).UpdatedSeq

	seq := f.mustSubmit(apigen.TaskDelete{TaskId: gone})

	if n := testdb.Count(t, pool, `SELECT count(*) FROM tasks WHERE id = $1`, gone); n != 0 {
		t.Error("task still exists")
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM comments WHERE task_id = $1`, gone); n != 0 {
		t.Error("comments still exist")
	}
	if deps := f.dependencies(waiterB); !reflect.DeepEqual(deps, []uuid.UUID{upstream}) {
		t.Errorf("B depends on %v, want only upstream", deps)
	}
	e := f.event(seq)
	if e.Type != "task.delete" || e.EntityID != gone {
		t.Errorf("event = %s on %s, want task.delete on %s", e.Type, e.EntityID, gone)
	}
	// The deleted task and every task that lost a dependency on it.
	if got, want := sorted(e.Touches...), sorted(gone, waiterA, waiterB); !reflect.DeepEqual(got, want) {
		t.Errorf("touches = %v, want %v", got, want)
	}
	for _, id := range []uuid.UUID{waiterA, waiterB} {
		if got := f.task(id).UpdatedSeq; got != seq {
			t.Errorf("dependent %s updatedSeq = %d, want %d", id, got, seq)
		}
	}
	// Losing a dependent changes nothing about upstream's own row.
	if got := f.task(upstream).UpdatedSeq; got != before {
		t.Errorf("upstream updatedSeq = %d, want %d", got, before)
	}
	if got := f.task(unrelated).UpdatedSeq; got == seq {
		t.Error("unrelated task was touched")
	}

	_, err := f.submit(apigen.TaskUpdate{TaskId: gone, Title: ptr("Back?")})
	wantCode(t, err, apigen.ErrorCodeTaskNotFound)
}

func TestSubmit_RejectedChangeLeavesNoGap(t *testing.T) {
	f := setup(t)
	id := f.createTask("T")
	if _, err := f.submit(apigen.TaskSetStatus{TaskId: id, To: apigen.Done}); err == nil {
		t.Fatal("todo → done accepted")
	}
	if _, err := f.submit(apigen.TaskDelete{TaskId: testdb.NewID()}); err == nil {
		t.Fatal("deleting a missing task accepted")
	}
	f.setStatus(id, apigen.InProgress)

	if got := f.eventSeqs(); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Errorf("events = %v, want [1 2]", got)
	}
	if got, want := f.bell.got(), []ring{{f.project, 1}, {f.project, 2}}; !reflect.DeepEqual(got, want) {
		t.Errorf("rings = %v, want %v", got, want)
	}
}

func TestSubmit_Assignees(t *testing.T) {
	f := setup(t)
	id := f.createTask("T")
	other := testdb.User(t, pool)

	seq := f.mustSubmit(apigen.TaskAddAssignee{TaskId: id, UserId: f.actor})
	if e := f.event(seq); e.Type != "task.addAssignee" || !reflect.DeepEqual(e.Touches, []uuid.UUID{id}) {
		t.Errorf("event = %s touching %v, want task.addAssignee touching [%s]", e.Type, e.Touches, id)
	}
	f.mustSubmit(apigen.TaskAddAssignee{TaskId: id, UserId: f.actor}) // already there: no-op
	f.mustSubmit(apigen.TaskAddAssignee{TaskId: id, UserId: other})
	if got, want := f.task(id).Assignees, []uuid.UUID{f.actor, other}; !reflect.DeepEqual(got, want) {
		t.Errorf("assignees = %v, want %v", got, want)
	}

	_, err := f.submit(apigen.TaskAddAssignee{TaskId: id, UserId: testdb.NewID()})
	wantInvalid(t, err, "userId")

	f.mustSubmit(apigen.TaskRemoveAssignee{TaskId: id, UserId: f.actor})
	f.mustSubmit(apigen.TaskRemoveAssignee{TaskId: id, UserId: f.actor}) // already gone: no-op
	if got, want := f.task(id).Assignees, []uuid.UUID{other}; !reflect.DeepEqual(got, want) {
		t.Errorf("assignees = %v, want %v", got, want)
	}

	_, err = f.submit(apigen.TaskRemoveAssignee{TaskId: testdb.NewID(), UserId: other})
	wantCode(t, err, apigen.ErrorCodeTaskNotFound)
}

func TestSubmit_Tags(t *testing.T) {
	f := setup(t)
	id := f.createTask("T")

	f.mustSubmit(apigen.TaskAddTag{TaskId: id, Tag: "api"})
	f.mustSubmit(apigen.TaskAddTag{TaskId: id, Tag: "web"})
	f.mustSubmit(apigen.TaskAddTag{TaskId: id, Tag: "api"}) // already there: no-op
	if got, want := f.task(id).Tags, []string{"api", "web"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}

	seq := f.mustSubmit(apigen.TaskRemoveTag{TaskId: id, Tag: "api"})
	f.mustSubmit(apigen.TaskRemoveTag{TaskId: id, Tag: "nope"}) // absent: no-op
	if got := f.task(id); !reflect.DeepEqual(got.Tags, []string{"web"}) || got.UpdatedSeq != seq+1 {
		t.Errorf("task = %v at %d, want [web] at %d", got.Tags, got.UpdatedSeq, seq+1)
	}

	_, err := f.submit(apigen.TaskAddTag{TaskId: testdb.NewID(), Tag: "api"})
	wantCode(t, err, apigen.ErrorCodeTaskNotFound)
}

func TestSubmit_Tags_Limit(t *testing.T) {
	f := setup(t)
	tags := make([]string, 20)
	for i := range tags {
		tags[i] = fmt.Sprintf("t%d", i)
	}
	id := testdb.NewID()
	f.mustSubmit(apigen.TaskCreate{Id: id, Title: "T", Configuration: &apigen.TaskCreateConfiguration{Tags: &tags}})

	_, err := f.submit(apigen.TaskAddTag{TaskId: id, Tag: "one-more"})
	wantCode(t, err, apigen.ErrorCodeLimitExceeded)
	// Re-adding a tag it already has is still fine at the limit.
	f.mustSubmit(apigen.TaskAddTag{TaskId: id, Tag: "t0"})
	if n := len(f.task(id).Tags); n != 20 {
		t.Errorf("tags = %d, want 20", n)
	}
}

func TestSubmit_Dependencies(t *testing.T) {
	f := setup(t)
	a, b, c := f.createTask("A"), f.createTask("B"), f.createTask("C")
	bBefore := f.task(b).UpdatedSeq

	seq := f.mustSubmit(apigen.TaskAddDependency{TaskId: a, DependsOnId: b})
	f.mustSubmit(apigen.TaskAddDependency{TaskId: a, DependsOnId: b}) // already there: no-op
	if deps := f.dependencies(a); !reflect.DeepEqual(deps, []uuid.UUID{b}) {
		t.Errorf("A depends on %v, want [B]", deps)
	}
	if e := f.event(seq); !reflect.DeepEqual(e.Touches, []uuid.UUID{a}) {
		t.Errorf("touches = %v, want only A", e.Touches)
	}
	// Gaining a dependent doesn't change B's own row.
	if got := f.task(b).UpdatedSeq; got != bBefore {
		t.Errorf("B updatedSeq = %d, want %d", got, bBefore)
	}
	f.mustSubmit(apigen.TaskAddDependency{TaskId: b, DependsOnId: c})

	for _, link := range [][2]uuid.UUID{{b, a}, {c, a}} {
		_, err := f.submit(apigen.TaskAddDependency{TaskId: link[0], DependsOnId: link[1]})
		wantCode(t, err, apigen.ErrorCodeDependencyCycle)
	}

	elsewhere := testdb.Task(t, pool, testdb.Project(t, pool))
	for _, link := range [][2]uuid.UUID{{a, elsewhere}, {a, testdb.NewID()}, {testdb.NewID(), a}} {
		_, err := f.submit(apigen.TaskAddDependency{TaskId: link[0], DependsOnId: link[1]})
		wantCode(t, err, apigen.ErrorCodeTaskNotFound)
	}

	seq = f.mustSubmit(apigen.TaskRemoveDependency{TaskId: a, DependsOnId: b})
	f.mustSubmit(apigen.TaskRemoveDependency{TaskId: a, DependsOnId: b}) // already gone: no-op
	if deps := f.dependencies(a); len(deps) != 0 {
		t.Errorf("A depends on %v, want nothing", deps)
	}
	if e := f.event(seq); e.Type != "task.removeDependency" || !reflect.DeepEqual(e.Touches, []uuid.UUID{a}) {
		t.Errorf("event = %s touching %v, want task.removeDependency touching only A", e.Type, e.Touches)
	}
	// With A→B gone, C→A no longer closes a loop.
	f.mustSubmit(apigen.TaskAddDependency{TaskId: c, DependsOnId: a})
}

func TestSubmit_AddingOpenDependencyKeepsDone(t *testing.T) {
	f := setup(t)
	done, open := f.createTask("Done"), f.createTask("Open")
	f.setStatus(done, apigen.InProgress, apigen.Done)

	f.mustSubmit(apigen.TaskAddDependency{TaskId: done, DependsOnId: open})
	if got := f.task(done).Status; got != "done" {
		t.Errorf("status = %s, want done", got)
	}
}

func TestSubmit_CommentCreate(t *testing.T) {
	f := setup(t)
	task := f.createTask("T")
	before := f.task(task).UpdatedSeq
	id := testdb.NewID()

	seq := f.mustSubmit(apigen.CommentCreate{Id: id, TaskId: task, Content: "Looks good"})

	var author uuid.UUID
	var content string
	var commentSeq int64
	err := pool.QueryRow(context.Background(),
		`SELECT author_id, content, seq FROM comments WHERE id = $1 AND task_id = $2 AND project_id = $3`, id, task, f.project).
		Scan(&author, &content, &commentSeq)
	if err != nil {
		t.Fatal(err)
	}
	if author != f.actor || content != "Looks good" || commentSeq != seq {
		t.Errorf("comment = %s %q at %d, want the actor's \"Looks good\" at %d", author, content, commentSeq, seq)
	}
	if e := f.event(seq); e.Type != "comment.create" || e.EntityID != id || len(e.Touches) != 0 {
		t.Errorf("event = %s on %s touching %v, want comment.create on the comment touching nothing", e.Type, e.EntityID, e.Touches)
	}
	// Comments live outside the task row, so the task isn't touched.
	if got := f.task(task).UpdatedSeq; got != before {
		t.Errorf("task updatedSeq = %d, want %d", got, before)
	}

	_, err = f.submit(apigen.CommentCreate{Id: id, TaskId: task, Content: "again"})
	wantInvalid(t, err, "id")
	_, err = f.submit(apigen.CommentCreate{Id: testdb.NewID(), TaskId: testdb.NewID(), Content: "hi"})
	wantCode(t, err, apigen.ErrorCodeTaskNotFound)
}

func TestSubmit_RemoveFromEmptySets(t *testing.T) {
	f := setup(t)
	id := f.createTask("T")
	f.mustSubmit(apigen.TaskRemoveTag{TaskId: id, Tag: "none"})
	f.mustSubmit(apigen.TaskRemoveAssignee{TaskId: id, UserId: f.actor})
	f.mustSubmit(apigen.TaskRemoveDependency{TaskId: id, DependsOnId: testdb.NewID()})
	if got := f.task(id); len(got.Tags) != 0 || len(got.Assignees) != 0 {
		t.Errorf("task = %+v, want empty sets", got)
	}
}
