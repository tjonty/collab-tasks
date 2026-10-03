package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

func TestListTasks_EachTaskExactlyOnceWhileWriting(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	ids := bulkTasks(t, project, 300)

	// A quarter of the tasks get deleted during the scan, spread across the
	// id range; the rest exist throughout and must each appear exactly once.
	var victims, stable []uuid.UUID
	for i, id := range ids {
		if i%4 == 1 {
			victims = append(victims, id)
		} else {
			stable = append(stable, id)
		}
	}
	less := func(a, b uuid.UUID) bool { return a.String() < b.String() }

	seen := map[uuid.UUID]int{}
	var order []uuid.UUID
	query := "?limit=40"
	for pages := 0; ; pages++ {
		if pages > 50 {
			t.Fatal("paging never ended")
		}
		p := page(t, h, project, user, query)
		for _, id := range summaryIDs(p) {
			seen[id]++
			order = append(order, id)
		}
		if p.NextCursor == nil {
			break
		}
		cursor := *p.NextCursor
		query = "?limit=40&after=" + cursor.String()

		// Between pages: delete tasks on both sides of the cursor, create
		// tasks whose random ids land anywhere, and retitle tasks.
		var behind, ahead []uuid.UUID
		for _, v := range victims {
			if less(cursor, v) {
				ahead = append(ahead, v)
			} else {
				behind = append(behind, v)
			}
		}
		for _, v := range append(behind[:min(4, len(behind))], ahead[:min(4, len(ahead))]...) {
			submit(t, h, project, user, map[string]any{"type": "task.delete", "taskId": v})
			victims = slices.DeleteFunc(victims, func(x uuid.UUID) bool { return x == v })
		}
		for range 4 {
			submit(t, h, project, user, map[string]any{"type": "task.create", "id": uuid.New(), "title": "New"})
		}
		for _, s := range []uuid.UUID{stable[pages%len(stable)], stable[(pages*37+100)%len(stable)]} {
			submit(t, h, project, user, map[string]any{"type": "task.update", "taskId": s, "title": "Retitled"})
		}
	}

	if !slices.IsSortedFunc(order, func(a, b uuid.UUID) int {
		if less(a, b) {
			return -1
		}
		return 1
	}) {
		t.Error("the scan did not return ids in strictly increasing order")
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("task %s appeared %d times", id, n)
		}
	}
	for _, id := range stable {
		if seen[id] != 1 {
			t.Errorf("task %s, which existed throughout, appeared %d times", id, seen[id])
		}
	}
}

// lastTouch is the seq of the last logged change to task at or before seq,
// or 0 when the log never touched it.
func lastTouch(t *testing.T, project, task uuid.UUID, seq int64) int64 {
	t.Helper()
	return int64(testdb.Count(t, pool, `SELECT coalesce(max(seq), 0) FROM events
		WHERE project_id = $1 AND $2 = ANY(touches) AND seq <= $3`, project, task, seq))
}

func TestListTasks_PageIsSnapshotOfSeq(t *testing.T) {
	h := newAPI(t)
	user, project := testdb.User(t, pool), testdb.Project(t, pool)
	ids := bulkTasks(t, project, 40)

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			body, _ := json.Marshal(map[string]any{"changeId": testdb.NewID(), "change": map[string]any{
				"type": "task.update", "taskId": ids[i%len(ids)], "title": "Edit"}})
			if rec := call(h, http.MethodPost, "/projects/"+project.String()+"/changes", user, string(body)); rec.Code != http.StatusOK {
				t.Errorf("writer: status %d (body %s)", rec.Code, rec.Body.String())
				return
			}
		}
	}()
	var pages []apigen.TaskPage
	for range 60 {
		pages = append(pages, page(t, h, project, user, ""))
	}
	close(stop)
	<-done

	// A page must show the project exactly as of its seq: each task as left
	// by the last change at or before that seq, and nothing later.
	for i, p := range pages {
		for _, s := range p.Tasks {
			if want := lastTouch(t, project, s.Id, p.Seq); s.UpdatedSeq != want {
				t.Fatalf("page %d (seq %d): task %s has updatedSeq %d, want %d", i, p.Seq, s.Id, s.UpdatedSeq, want)
			}
		}
	}
}
