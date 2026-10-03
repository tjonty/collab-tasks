package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

// get sends a GET as user and, on 200, decodes the body into out.
func get(t *testing.T, h http.Handler, path string, user uuid.UUID, out any) *httptest.ResponseRecorder {
	t.Helper()
	rec := call(h, http.MethodGet, path, user, "")
	if rec.Code == http.StatusOK && out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
	}
	return rec
}

func wantOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// submit applies one change through the API and returns its seq.
func submit(t *testing.T, h http.Handler, project, user uuid.UUID, change map[string]any) int64 {
	t.Helper()
	body, err := json.Marshal(map[string]any{"changeId": testdb.NewID(), "change": change})
	if err != nil {
		t.Fatal(err)
	}
	rec := call(h, http.MethodPost, "/projects/"+project.String()+"/changes", user, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("submit %v: status %d (body %s)", change["type"], rec.Code, rec.Body.String())
	}
	var res apigen.SubmitChangeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.Seq
}

// createProject creates a project through the API and returns its id.
func createProject(t *testing.T, h http.Handler, user uuid.UUID, body map[string]any) uuid.UUID {
	t.Helper()
	id := testdb.NewID()
	body["id"] = id
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if rec := call(h, http.MethodPost, "/projects", user, string(raw)); rec.Code != http.StatusCreated {
		t.Fatalf("create project: status %d (body %s)", rec.Code, rec.Body.String())
	}
	return id
}

// bulkTasks inserts n tasks with random ids straight into the database, for
// tests that only need many rows.
func bulkTasks(t *testing.T, project uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	testdb.MustExec(t, pool, `INSERT INTO tasks (id, project_id, title)
		SELECT gen_random_uuid(), $1, 'Task ' || g FROM generate_series(1, $2) g`, project, n)
	return taskIDs(t, project)
}

// taskIDs lists a project's task ids in id order, as Postgres sorts uuids.
func taskIDs(t *testing.T, project uuid.UUID) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	rows, err := pool.Query(t.Context(), `SELECT id FROM tasks WHERE project_id = $1 ORDER BY id`, project)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestListUsers(t *testing.T) {
	h := newAPI(t)
	viewer := testdb.User(t, pool)
	suffix := testdb.NewID().String()[:8]
	var created []apigen.User
	for _, name := range []string{"Zed " + suffix, "Amy " + suffix} {
		rec := call(h, http.MethodPost, "/users", uuid.Nil, `{"displayName":"`+name+`"}`)
		var u apigen.User
		if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
			t.Fatal(err)
		}
		created = append(created, u)
	}

	var users []apigen.User
	wantOK(t, get(t, h, "/users", viewer, &users))

	for _, want := range created {
		if !slices.Contains(users, want) {
			t.Errorf("users missing %+v", want)
		}
	}
	sorted := slices.IsSortedFunc(users, func(a, b apigen.User) int {
		if c := strings.Compare(a.DisplayName, b.DisplayName); c != 0 {
			return c
		}
		return strings.Compare(a.Id.String(), b.Id.String())
	})
	if !sorted {
		t.Error("users are not ordered by display name, then id")
	}
}

func TestListProjects(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	first := createProject(t, h, user, map[string]any{"name": "First"})
	second := createProject(t, h, user, map[string]any{"name": "Second", "description": "d", "metadata": map[string]any{"k": "v"}})
	submit(t, h, second, user, map[string]any{"type": "project.update", "name": "Second v2"})

	var raw []map[string]any
	wantOK(t, get(t, h, "/projects", user, &raw))

	var got []map[string]any
	for _, p := range raw {
		if p["id"] == first.String() || p["id"] == second.String() {
			got = append(got, p)
		}
	}
	want := []map[string]any{
		{"id": first.String(), "name": "First", "description": "", "seq": 0.0},
		{"id": second.String(), "name": "Second v2", "description": "d", "seq": 1.0},
	}
	// Exactly the summary fields, oldest project first; no metadata.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projects = %v, want %v", got, want)
	}
}

func TestGetProject(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	id := createProject(t, h, user, map[string]any{"name": "Launch", "metadata": map[string]any{"color": "red"}})
	submit(t, h, id, user, map[string]any{"type": "project.update", "description": "Ship it"})

	var p apigen.Project
	wantOK(t, get(t, h, "/projects/"+id.String(), user, &p))
	want := apigen.Project{Id: id, Name: "Launch", Description: "Ship it", Metadata: apigen.Metadata{"color": "red"}, Seq: 1}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("project = %+v, want %+v", p, want)
	}

	wantError(t, get(t, h, "/projects/"+testdb.NewID().String(), user, nil), http.StatusNotFound, "project_not_found", "")
}

// page fetches one page of a project's task summaries.
func page(t *testing.T, h http.Handler, project, user uuid.UUID, query string) apigen.TaskPage {
	t.Helper()
	var p apigen.TaskPage
	wantOK(t, get(t, h, "/projects/"+project.String()+"/tasks"+query, user, &p))
	return p
}

func summaryIDs(p apigen.TaskPage) []uuid.UUID {
	ids := make([]uuid.UUID, len(p.Tasks))
	for i, s := range p.Tasks {
		ids[i] = s.Id
	}
	return ids
}

func TestListTasks_PagesByID(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	ids := bulkTasks(t, project, 6)

	first := page(t, h, project, user, "?limit=4")
	if got := summaryIDs(first); !reflect.DeepEqual(got, ids[:4]) {
		t.Errorf("page 1 = %v, want %v", got, ids[:4])
	}
	if first.NextCursor == nil || *first.NextCursor != ids[3] {
		t.Fatalf("page 1 nextCursor = %v, want %s", first.NextCursor, ids[3])
	}

	second := page(t, h, project, user, "?limit=2&after="+first.NextCursor.String())
	if got := summaryIDs(second); !reflect.DeepEqual(got, ids[4:]) {
		t.Errorf("page 2 = %v, want %v", got, ids[4:])
	}
	// The page ends exactly at the last task: there is no next page.
	if second.NextCursor != nil {
		t.Errorf("page 2 nextCursor = %s, want null", second.NextCursor)
	}
}

func TestListTasks_EmptyProject(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	rec := get(t, h, "/projects/"+project.String()+"/tasks", user, nil)
	wantOK(t, rec)
	// An empty page is an empty list, not null.
	if body := strings.TrimSpace(rec.Body.String()); body != `{"nextCursor":null,"seq":0,"tasks":[]}` {
		t.Errorf("body = %s", body)
	}
}

func TestListTasks_EmptySetsAreLists(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	bulkTasks(t, project, 1)
	rec := get(t, h, "/projects/"+project.String()+"/tasks", user, nil)
	wantOK(t, rec)
	for _, field := range []string{`"assignedTo":[]`, `"tags":[]`, `"dependencies":[]`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("body %s lacks %s", rec.Body.String(), field)
		}
	}
}

func TestListTasks_SummaryLeavesOutHeavyFields(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	project := createProject(t, h, user, map[string]any{"name": "P"})
	dep, id := testdb.NewID(), testdb.NewID()
	submit(t, h, project, user, map[string]any{"type": "task.create", "id": dep, "title": "Design"})
	seq := submit(t, h, project, user, map[string]any{
		"type": "task.create", "id": id, "title": "Build",
		"configuration": map[string]any{
			"priority": "high", "description": strings.Repeat("x", 10_000),
			"tags": []string{"api"}, "customFields": map[string]any{"points": 3},
		},
		"assignedTo": []uuid.UUID{user}, "dependencies": []uuid.UUID{dep},
	})

	rec := get(t, h, "/projects/"+project.String()+"/tasks", user, nil)
	wantOK(t, rec)
	var p struct{ Tasks []map[string]any }
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	for _, s := range p.Tasks {
		if s["id"] == id.String() {
			summary = s
		}
	}
	want := map[string]any{
		"id": id.String(), "title": "Build", "status": "todo", "priority": "high",
		"assignedTo": []any{user.String()}, "tags": []any{"api"},
		"dependencies": []any{dep.String()}, "updatedSeq": float64(seq),
	}
	if !reflect.DeepEqual(summary, want) {
		t.Errorf("summary = %v, want %v", summary, want)
	}
	if n := rec.Body.Len(); n > 1_000 {
		t.Errorf("page is %d bytes; the 10,000-character description leaked into it", n)
	}
}

func TestListTasks_DefaultAndMaxPageSize(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	ids := bulkTasks(t, project, 501)

	first := page(t, h, project, user, "")
	if len(first.Tasks) != 500 || first.NextCursor == nil || *first.NextCursor != ids[499] {
		t.Fatalf("default page = %d tasks, nextCursor %v; want 500 and %s", len(first.Tasks), first.NextCursor, ids[499])
	}
	last := page(t, h, project, user, "?limit=500&after="+first.NextCursor.String())
	if got := summaryIDs(last); !reflect.DeepEqual(got, ids[500:]) || last.NextCursor != nil {
		t.Errorf("last page = %v (next %v), want %v and null", got, last.NextCursor, ids[500:])
	}
}

func TestListTasks_BadParams(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	tasks := "/projects/" + project.String() + "/tasks"

	for _, limit := range []string{"0", "501", "-1"} {
		wantError(t, get(t, h, tasks+"?limit="+limit, user, nil), http.StatusUnprocessableEntity, "validation_failed", "limit")
	}
	wantError(t, get(t, h, tasks+"?after=not-a-uuid", user, nil), http.StatusUnprocessableEntity, "validation_failed", "after")
	wantError(t, get(t, h, "/projects/"+testdb.NewID().String()+"/tasks", user, nil), http.StatusNotFound, "project_not_found", "")
}

func TestGetTask(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	project := createProject(t, h, user, map[string]any{"name": "P"})
	depA, depB, id := testdb.NewID(), testdb.NewID(), testdb.NewID()
	submit(t, h, project, user, map[string]any{"type": "task.create", "id": depA, "title": "A"})
	submit(t, h, project, user, map[string]any{"type": "task.create", "id": depB, "title": "B"})
	seq := submit(t, h, project, user, map[string]any{
		"type": "task.create", "id": id, "title": "Build",
		"configuration": map[string]any{
			"priority": "high", "description": "Ship it", "tags": []string{"web", "api"},
			"customFields": map[string]any{"points": 3, "billable": true},
		},
		"assignedTo": []uuid.UUID{user}, "dependencies": []uuid.UUID{depB, depA},
	})

	var task apigen.Task
	wantOK(t, get(t, h, "/tasks/"+id.String(), user, &task))

	deps := []uuid.UUID{depA, depB}
	slices.SortFunc(deps, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	want := apigen.Task{
		Id: id, ProjectId: project, Title: "Build", Status: apigen.Todo, AssignedTo: []uuid.UUID{user},
		Configuration: apigen.TaskConfiguration{
			Priority: apigen.High, Description: "Ship it", Tags: []string{"web", "api"},
			CustomFields: apigen.CustomFields{"points": 3.0, "billable": true},
		},
		Dependencies: deps, // ordered by id, not as sent
		UpdatedSeq:   seq,
	}
	if !reflect.DeepEqual(task, want) {
		t.Errorf("task = %+v, want %+v", task, want)
	}

	wantError(t, get(t, h, "/tasks/"+testdb.NewID().String(), user, nil), http.StatusNotFound, "task_not_found", "")
}

func TestGetTask_EmptyFieldsAreEmptyValues(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	id := bulkTasks(t, project, 1)[0]
	rec := get(t, h, "/tasks/"+id.String(), user, nil)
	wantOK(t, rec)
	for _, field := range []string{`"assignedTo":[]`, `"tags":[]`, `"dependencies":[]`, `"customFields":{}`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("body %s lacks %s", rec.Body.String(), field)
		}
	}
}

func comments(t *testing.T, h http.Handler, task, user uuid.UUID, query string) apigen.CommentPage {
	t.Helper()
	var p apigen.CommentPage
	wantOK(t, get(t, h, "/tasks/"+task.String()+"/comments"+query, user, &p))
	return p
}

func TestListComments(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	project := createProject(t, h, user, map[string]any{"name": "P"})
	a, b := testdb.NewID(), testdb.NewID()
	submit(t, h, project, user, map[string]any{"type": "task.create", "id": a, "title": "A"})
	submit(t, h, project, user, map[string]any{"type": "task.create", "id": b, "title": "B"})

	type posted struct {
		id      uuid.UUID
		content string
		seq     int64
	}
	post := func(task uuid.UUID, content string) posted {
		id := testdb.NewID()
		seq := submit(t, h, project, user, map[string]any{"type": "comment.create", "id": id, "taskId": task, "content": content})
		return posted{id, content, seq}
	}
	first := post(a, "first")
	post(b, "on another task")
	second, third := post(a, "second"), post(a, "third")

	all := comments(t, h, a, user, "")
	if all.NextCursor != nil {
		t.Errorf("nextCursor = %d, want null", *all.NextCursor)
	}
	if len(all.Comments) != 3 {
		t.Fatalf("comments = %d, want 3", len(all.Comments))
	}
	for i, want := range []posted{first, second, third} {
		c := all.Comments[i]
		if c.Id != want.id || c.TaskId != a || c.Content != want.content || c.Author != user || c.Seq != want.seq {
			t.Errorf("comment %d = %+v, want %q by the user at seq %d", i, c, want.content, want.seq)
		}
		if c.Timestamp.IsZero() {
			t.Errorf("comment %d has no timestamp", i)
		}
	}

	p1 := comments(t, h, a, user, "?limit=2")
	if len(p1.Comments) != 2 || p1.Comments[1].Id != second.id || p1.NextCursor == nil || *p1.NextCursor != second.seq {
		t.Fatalf("page 1 = %d comments, next %v; want first two, next %d", len(p1.Comments), p1.NextCursor, second.seq)
	}
	p2 := comments(t, h, a, user, fmt.Sprintf("?limit=2&afterSeq=%d", *p1.NextCursor))
	if len(p2.Comments) != 1 || p2.Comments[0].Id != third.id || p2.NextCursor != nil {
		t.Errorf("page 2 = %+v, want only the third comment and no next page", p2)
	}
}

func TestListComments_EmptyAndErrors(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	task := bulkTasks(t, project, 1)[0]
	path := "/tasks/" + task.String() + "/comments"

	rec := get(t, h, path, user, nil)
	wantOK(t, rec)
	if body := strings.TrimSpace(rec.Body.String()); body != `{"comments":[],"nextCursor":null}` {
		t.Errorf("body = %s", body)
	}
	wantError(t, get(t, h, "/tasks/"+testdb.NewID().String()+"/comments", user, nil), http.StatusNotFound, "task_not_found", "")
	for _, limit := range []string{"0", "101"} {
		wantError(t, get(t, h, path+"?limit="+limit, user, nil), http.StatusUnprocessableEntity, "validation_failed", "limit")
	}
	wantError(t, get(t, h, path+"?afterSeq=-1", user, nil), http.StatusUnprocessableEntity, "validation_failed", "afterSeq")
}

func events(t *testing.T, h http.Handler, project, user uuid.UUID, query string) apigen.EventPage {
	t.Helper()
	var p apigen.EventPage
	wantOK(t, get(t, h, "/projects/"+project.String()+"/events"+query, user, &p))
	return p
}

func TestListEvents(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	project := createProject(t, h, user, map[string]any{"name": "P"})
	task, comment := testdb.NewID(), testdb.NewID()
	changes := []map[string]any{
		{"type": "task.create", "id": task, "title": "T"},
		{"type": "task.update", "taskId": task, "title": "Renamed"},
		{"type": "comment.create", "id": comment, "taskId": task, "content": "hi"},
	}
	var changeIDs []uuid.UUID
	for _, c := range changes {
		changeID := testdb.NewID()
		body, _ := json.Marshal(map[string]any{"changeId": changeID, "change": c})
		if rec := call(h, http.MethodPost, "/projects/"+project.String()+"/changes", user, string(body)); rec.Code != http.StatusOK {
			t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
		}
		changeIDs = append(changeIDs, changeID)
	}

	all := events(t, h, project, user, "")
	if len(all.Events) != 3 || all.NextCursor != nil {
		t.Fatalf("events = %d (next %v), want 3 and null", len(all.Events), all.NextCursor)
	}
	wantTouches := [][]uuid.UUID{{task}, {task}, {}}
	for i, e := range all.Events {
		if e.ProjectId != project || e.Seq != int64(i+1) || e.ChangeId != changeIDs[i] || e.ActorId != user ||
			e.CreatedAt.IsZero() || !reflect.DeepEqual(e.Touches, wantTouches[i]) {
			t.Errorf("event %d = %+v", i, e)
		}
		if typ, _ := e.Change.Discriminator(); typ != changes[i]["type"] {
			t.Errorf("event %d type = %s, want %s", i, typ, changes[i]["type"])
		}
	}
	if u, err := all.Events[1].Change.AsTaskUpdate(); err != nil || u.TaskId != task || u.Title == nil || *u.Title != "Renamed" {
		t.Errorf("event 2 change = %+v (%v), want the rename", u, err)
	}

	p := events(t, h, project, user, "?after=1&limit=1")
	if len(p.Events) != 1 || p.Events[0].Seq != 2 || p.NextCursor == nil || *p.NextCursor != 2 {
		t.Errorf("after=1 limit=1: %d events, next %v; want seq 2 and next 2", len(p.Events), p.NextCursor)
	}
	rec := get(t, h, "/projects/"+project.String()+"/events?after=3", user, nil)
	wantOK(t, rec)
	if body := strings.TrimSpace(rec.Body.String()); body != `{"events":[],"nextCursor":null}` {
		t.Errorf("caught-up body = %s", body)
	}
}

func TestListEvents_Errors(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	path := "/projects/" + project.String() + "/events"
	wantError(t, get(t, h, "/projects/"+testdb.NewID().String()+"/events", user, nil), http.StatusNotFound, "project_not_found", "")
	for _, limit := range []string{"0", "1001"} {
		wantError(t, get(t, h, path+"?limit="+limit, user, nil), http.StatusUnprocessableEntity, "validation_failed", "limit")
	}
	wantError(t, get(t, h, path+"?after=-1", user, nil), http.StatusUnprocessableEntity, "validation_failed", "after")
}

// Projects grow past 2 MB, so the log must carry what changed, never a copy
// of the task or project it changed.
func TestEvents_EditsAreSmallDeltas(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	project := createProject(t, h, user, map[string]any{"name": "P"})
	big := testdb.NewID()
	fields := map[string]any{}
	for i := range 20 {
		fields[fmt.Sprintf("field%d", i)] = strings.Repeat("v", 500)
	}
	created := submit(t, h, project, user, map[string]any{"type": "task.create", "id": big, "title": "Big",
		"configuration": map[string]any{"description": strings.Repeat("d", 10_000), "customFields": fields}})

	edits := []map[string]any{
		{"type": "task.update", "taskId": big, "title": "Big, renamed"},
		{"type": "task.setStatus", "taskId": big, "to": "in_progress"},
		{"type": "task.addTag", "taskId": big, "tag": "api"},
		{"type": "task.addAssignee", "taskId": big, "userId": user},
		{"type": "task.update", "taskId": big, "customFields": map[string]any{"field0": "short"}},
		{"type": "comment.create", "id": testdb.NewID(), "taskId": big, "content": "On it"},
		{"type": "project.update", "name": "P, renamed"},
	}
	for _, e := range edits {
		submit(t, h, project, user, e)
	}

	sizes := func(query string) []int {
		rec := get(t, h, "/projects/"+project.String()+"/events"+query, user, nil)
		wantOK(t, rec)
		var p struct{ Events []json.RawMessage }
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		n := make([]int, len(p.Events))
		for i, e := range p.Events {
			n[i] = len(e)
		}
		return n
	}
	// The create carries the whole task, so the log really holds a big one.
	if n := sizes(fmt.Sprintf("?after=%d&limit=1", created-1)); n[0] < 10_000 {
		t.Fatalf("create event is %d bytes, want the full 10 KB+ task", n[0])
	}
	got := sizes(fmt.Sprintf("?after=%d", created))
	if len(got) != len(edits) {
		t.Fatalf("events after the create = %d, want %d", len(got), len(edits))
	}
	for i, n := range got {
		if n >= 1024 {
			t.Errorf("%s event is %d bytes, want under 1 KB", edits[i]["type"], n)
		}
	}
}
