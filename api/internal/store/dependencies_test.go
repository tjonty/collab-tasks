package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestDependencyWouldCycle(t *testing.T) {
	ctx := context.Background()
	q := New(pool)

	// Graph, read "x→y" as "x depends on y":
	//   a→b, b→c, a→d, d→c   (a diamond over c)
	//   e has no links
	p := newProject(t)
	a, b, c, d, e := newTask(t, p), newTask(t, p), newTask(t, p), newTask(t, p), newTask(t, p)
	for _, dep := range [][2]uuid.UUID{{a, b}, {b, c}, {a, d}, {d, c}} {
		if err := addDep(t, p, dep[0], dep[1]); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name              string
		task, dependsOnID uuid.UUID
		want              bool
	}{
		{"direct reverse b→a", b, a, true},
		{"indirect c→a", c, a, true},
		{"through either diamond side c→b", c, b, true},
		{"shortcut a→c", a, c, false},
		{"sibling b→d", b, d, false},
		{"new root e→a", e, a, false},
		{"new leaf c→e", c, e, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := q.DependencyWouldCycle(ctx, DependencyWouldCycleParams{
				TaskID:      tt.task,
				DependsOnID: tt.dependsOnID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("DependencyWouldCycle = %v, want %v", got, tt.want)
			}
		})
	}
}
