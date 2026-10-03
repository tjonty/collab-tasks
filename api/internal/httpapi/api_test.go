package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/store"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { testdb.Main(m, &pool, store.Migrate) }

func newAPI(t *testing.T) http.Handler {
	t.Helper()
	return NewRouter(Config{CORSOrigin: origin, Pool: pool})
}

// call sends a JSON request, with X-User-Id when user isn't uuid.Nil.
func call(h http.Handler, method, path string, user uuid.UUID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != uuid.Nil {
		req.Header.Set("X-User-Id", user.String())
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return m
}

// wantError checks the status and the error body's code and, when field
// isn't "", details.field.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["code"] != code {
		t.Errorf("code = %v, want %s", body["code"], code)
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Error("message is empty")
	}
	if field != "" {
		details, _ := body["details"].(map[string]any)
		if details["field"] != field {
			t.Errorf("details.field = %v, want %s", details["field"], field)
		}
	}
}

func TestCreateUser(t *testing.T) {
	h := newAPI(t)
	rec := call(h, http.MethodPost, "/users", uuid.Nil, `{"displayName":"Ana"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	id, err := uuid.Parse(body["id"].(string))
	if err != nil || body["displayName"] != "Ana" {
		t.Fatalf("body = %v, want an id and displayName Ana", body)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM users WHERE id = $1 AND display_name = 'Ana'`, id); n != 1 {
		t.Error("user not stored")
	}
}

func TestCreateUser_Invalid(t *testing.T) {
	rec := call(newAPI(t), http.MethodPost, "/users", uuid.Nil, `{"displayName":"  "}`)
	wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed", "displayName")
}

func TestCreateProject(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	id := testdb.NewID()

	rec := call(h, http.MethodPost, "/projects", user, `{"id":"`+id.String()+`","name":"Launch","metadata":{"color":"red"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["id"] != id.String() || body["name"] != "Launch" || body["description"] != "" ||
		body["seq"] != 0.0 || body["metadata"].(map[string]any)["color"] != "red" {
		t.Errorf("body = %v", body)
	}

	// A retry returns the project as first created and makes no second one.
	rec = call(h, http.MethodPost, "/projects", user, `{"id":"`+id.String()+`","name":"Renamed by retry"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("retry status = %d, want 201", rec.Code)
	}
	if body := decodeBody(t, rec); body["name"] != "Launch" {
		t.Errorf("retry name = %v, want Launch", body["name"])
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM projects WHERE id = $1`, id); n != 1 {
		t.Errorf("projects = %d, want 1", n)
	}
}

func TestCreateProject_Invalid(t *testing.T) {
	h := newAPI(t)
	user := testdb.User(t, pool)
	wantError(t, call(h, http.MethodPost, "/projects", user, `{"id":"`+testdb.NewID().String()+`","name":""}`),
		http.StatusUnprocessableEntity, "validation_failed", "name")
	wantError(t, call(h, http.MethodPost, "/projects", user, `{"name":"No id"}`),
		http.StatusUnprocessableEntity, "validation_failed", "id")
}

func TestIdentityRequired(t *testing.T) {
	h := newAPI(t)
	project := testdb.Project(t, pool)
	paths := []struct{ path, body string }{
		{"/projects", `{"id":"` + testdb.NewID().String() + `","name":"x"}`},
		{"/projects/" + project.String() + "/changes", `{"changeId":"` + testdb.NewID().String() + `","change":{"type":"project.update","name":"x"}}`},
	}
	for _, p := range paths {
		for name, header := range map[string]string{"missing": "", "malformed": "not-a-uuid", "unknown": testdb.NewID().String()} {
			t.Run(p.path+"/"+name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, p.path, strings.NewReader(p.body))
				req.Header.Set("Content-Type", "application/json")
				if header != "" {
					req.Header.Set("X-User-Id", header)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				wantError(t, rec, http.StatusUnauthorized, "unknown_user", "")
			})
		}
	}
	if n := testdb.Count(t, pool, `SELECT seq FROM projects WHERE id = $1`, project); n != 0 {
		t.Errorf("project seq = %d, want 0: an unidentified change was applied", n)
	}
}

func TestSubmitChange(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	changeID := testdb.NewID()
	body := `{"changeId":"` + changeID.String() + `","change":{"type":"project.update","name":"Renamed"}}`

	for range 2 { // the second send is a retry
		rec := call(h, http.MethodPost, "/projects/"+project.String()+"/changes", user, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		if got := decodeBody(t, rec); got["seq"] != 1.0 {
			t.Errorf("body = %v, want seq 1", got)
		}
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM events WHERE project_id = $1 AND actor_id = $2`, project, user); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
}

func TestSubmitChange_Errors(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	task := testdb.Task(t, pool, project)
	changes := "/projects/" + project.String() + "/changes"
	submit := func(path, change string) *httptest.ResponseRecorder {
		return call(h, http.MethodPost, path, user, `{"changeId":"`+testdb.NewID().String()+`","change":`+change+`}`)
	}

	wantError(t, submit("/projects/"+testdb.NewID().String()+"/changes", `{"type":"project.update","name":"x"}`),
		http.StatusNotFound, "project_not_found", "")
	wantError(t, submit(changes, `{"type":"task.setStatus","taskId":"`+task.String()+`","to":"done"}`),
		http.StatusConflict, "invalid_transition", "")
	wantError(t, submit(changes, `{"type":"task.delete","taskId":"`+testdb.NewID().String()+`"}`),
		http.StatusConflict, "task_not_found", "")
	wantError(t, submit(changes, `{"type":"task.update","taskId":"`+task.String()+`","title":""}`),
		http.StatusUnprocessableEntity, "validation_failed", "title")
	wantError(t, submit(changes, `{"type":"task.archive","taskId":"`+task.String()+`"}`),
		http.StatusUnprocessableEntity, "validation_failed", "type")
	wantError(t, submit("/projects/not-a-uuid/changes", `{"type":"project.update","name":"x"}`),
		http.StatusUnprocessableEntity, "validation_failed", "projectId")
	wantError(t, call(h, http.MethodPost, changes, user, `{"change":{"type":"project.update","name":"x"}}`),
		http.StatusUnprocessableEntity, "validation_failed", "changeId")
	wantError(t, call(h, http.MethodPost, changes, user, `{"changeId":`),
		http.StatusUnprocessableEntity, "validation_failed", "")
}

func TestDatabaseDown(t *testing.T) {
	// A pool aimed at a port nothing listens on behaves like Postgres down.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	down, err := pgxpool.New(context.Background(), "postgres://app:app@"+addr+"/tasks?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer down.Close()
	h := NewRouter(Config{CORSOrigin: origin, Pool: down})

	wantError(t, call(h, http.MethodPost, "/users", uuid.Nil, `{"displayName":"Ana"}`),
		http.StatusServiceUnavailable, "unavailable", "")
	wantError(t, call(h, http.MethodPost, "/projects/"+testdb.NewID().String()+"/changes", testdb.NewID(),
		`{"changeId":"`+testdb.NewID().String()+`","change":{"type":"project.update","name":"x"}}`),
		http.StatusServiceUnavailable, "unavailable", "")
}
