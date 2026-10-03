package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

func TestReads_GzipWhenAccepted(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	bulkTasks(t, project, 200)
	path := "/projects/" + project.String() + "/tasks"

	plain := call(h, http.MethodGet, path, user, "")
	wantOK(t, plain)
	if enc := plain.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q without Accept-Encoding, want none", enc)
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-User-Id", user.String())
	req.Header.Set("Accept-Encoding", "gzip")
	zipped := httptest.NewRecorder()
	h.ServeHTTP(zipped, req)
	wantOK(t, zipped)
	if enc := zipped.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unzipped, plain.Body.Bytes()) {
		t.Error("the gzipped body doesn't decompress to the plain body")
	}
	if zipped.Body.Len() >= plain.Body.Len()/2 {
		t.Errorf("gzipped %d bytes from %d; want at least 2x smaller", zipped.Body.Len(), plain.Body.Len())
	}
}

func TestIdentityRequired_Reads(t *testing.T) {
	h := newAPI(t)
	project := testdb.Project(t, pool)
	task := testdb.Task(t, pool, project)
	paths := []string{
		"/users", "/projects", "/projects/" + project.String(),
		"/projects/" + project.String() + "/tasks", "/projects/" + project.String() + "/events",
		"/tasks/" + task.String(), "/tasks/" + task.String() + "/comments",
	}
	for _, path := range paths {
		for _, user := range []uuid.UUID{uuid.Nil, testdb.NewID()} { // missing, unknown
			wantError(t, call(h, http.MethodGet, path, user, ""), http.StatusUnauthorized, "unknown_user", "")
		}
	}
}
