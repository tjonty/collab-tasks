package changes

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/testdb"
)

// race runs every fn at once and waits for them all.
func race(fns ...func()) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn()
		}()
	}
	close(start)
	wg.Wait()
}

func TestRace_OppositeLinksNeverFormCycle(t *testing.T) {
	f := setup(t)
	const pairs, perDirection = 10, 5

	type pair struct{ a, b uuid.UUID }
	ps := make([]pair, pairs)
	for i := range ps {
		ps[i] = pair{f.createTask("A"), f.createTask("B")}
	}

	var mu sync.Mutex
	var unexpected []error
	var fns []func()
	for _, p := range ps {
		for range perDirection {
			for _, link := range [][2]uuid.UUID{{p.a, p.b}, {p.b, p.a}} {
				change := wrap(t, apigen.TaskAddDependency{TaskId: link[0], DependsOnId: link[1]})
				fns = append(fns, func() {
					_, err := f.svc.Submit(context.Background(), f.project, f.actor, testdb.NewID(), change)
					if ce, ok := err.(*Error); err != nil && !(ok && ce.Code == apigen.ErrorCodeDependencyCycle) {
						mu.Lock()
						unexpected = append(unexpected, err)
						mu.Unlock()
					}
				})
			}
		}
	}
	race(fns...)

	for _, err := range unexpected {
		t.Errorf("Submit = %v, want nil or dependency_cycle", err)
	}
	for i, p := range ps {
		n := testdb.Count(t, pool, `SELECT count(*) FROM task_dependencies
			WHERE (task_id = $1 AND depends_on_id = $2) OR (task_id = $2 AND depends_on_id = $1)`, p.a, p.b)
		if n != 1 {
			t.Errorf("pair %d has %d links, want exactly 1 (one direction won)", i, n)
		}
	}
}

func TestRace_DoneNeverCommitsOverReopenedDependency(t *testing.T) {
	f := setup(t)
	const rounds = 20

	for round := range rounds {
		dep, task := f.createTask("Dep"), testdb.NewID()
		f.mustSubmit(apigen.TaskCreate{Id: task, Title: "Task", Dependencies: &[]uuid.UUID{dep}})
		f.setStatus(dep, apigen.InProgress, apigen.Done)
		f.setStatus(task, apigen.InProgress)

		var doneSeq, reopenSeq int64
		var doneErr, reopenErr error
		race(
			func() {
				doneSeq, doneErr = f.svc.Submit(context.Background(), f.project, f.actor, testdb.NewID(),
					wrap(t, apigen.TaskSetStatus{TaskId: task, To: apigen.Done}))
			},
			func() {
				reopenSeq, reopenErr = f.svc.Submit(context.Background(), f.project, f.actor, testdb.NewID(),
					wrap(t, apigen.TaskSetStatus{TaskId: dep, To: apigen.InProgress}))
			},
		)

		if reopenErr != nil {
			t.Fatalf("round %d: reopen = %v, want nil (reopening is always allowed)", round, reopenErr)
		}
		switch {
		case doneErr == nil && doneSeq > reopenSeq:
			t.Fatalf("round %d: task marked done at seq %d after its dependency reopened at %d", round, doneSeq, reopenSeq)
		case doneErr != nil:
			wantCode(t, doneErr, apigen.ErrorCodeDependenciesNotDone)
		}
	}
}
