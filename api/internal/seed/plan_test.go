package seed

import (
	"reflect"
	"testing"

	"github.com/tjonty/collab-tasks/api/internal/domain"
)

func TestNewPlan_IsDeterministic(t *testing.T) {
	if !reflect.DeepEqual(NewPlan(7, 500, 100), NewPlan(7, 500, 100)) {
		t.Error("the same random seed gave two different plans")
	}
	if reflect.DeepEqual(NewPlan(7, 500, 100), NewPlan(8, 500, 100)) {
		t.Error("different random seeds gave the same plan")
	}
}

func TestNewPlan_HasTheRequestedSize(t *testing.T) {
	p := NewPlan(1, 321, 45)
	if len(p.Tasks) != 321 || len(p.Comments) != 45 || len(p.Users) < 2 {
		t.Errorf("plan has %d tasks, %d comments, %d users; want 321, 45 and at least 2", len(p.Tasks), len(p.Comments), len(p.Users))
	}
	if err := domain.ValidateProjectName(p.Project.Name); err != nil {
		t.Error(err)
	}
}

// Everything the server will check, so the seed never has a change rejected.
func TestNewPlan_FollowsTheRules(t *testing.T) {
	p := NewPlan(3, 2_000, 400)
	for _, name := range p.Users {
		if err := domain.ValidateDisplayName(name); err != nil {
			t.Fatalf("user %q: %v", name, err)
		}
	}
	for i, task := range p.Tasks {
		for _, err := range []error{
			domain.ValidateTaskTitle(task.Title),
			domain.ValidateTaskDescription(task.Description),
			domain.ValidateTags(task.Tags),
			domain.ValidateCustomFields(task.CustomFields),
		} {
			if err != nil {
				t.Fatalf("task %d: %v", i, err)
			}
		}
		if !domain.Priority(task.Priority).Valid() || !task.Status.Valid() {
			t.Fatalf("task %d: priority %q, status %q", i, task.Priority, task.Status)
		}
		seen := map[int]bool{}
		for _, u := range task.Assignees {
			if u < 0 || u >= len(p.Users) || seen[u] {
				t.Fatalf("task %d: assignee %d is out of range or repeated", i, u)
			}
			seen[u] = true
		}
		seen = map[int]bool{}
		for _, d := range task.Dependencies {
			// Only earlier tasks: the dependency graph can't have a loop.
			if d < 0 || d >= i || seen[d] {
				t.Fatalf("task %d: dependency %d isn't a distinct earlier task", i, d)
			}
			seen[d] = true
			if task.Status == domain.StatusDone && p.Tasks[d].Status != domain.StatusDone {
				t.Fatalf("task %d is done but its dependency %d is %s", i, d, p.Tasks[d].Status)
			}
		}
	}
	for i, c := range p.Comments {
		if c.Task < 0 || c.Task >= len(p.Tasks) || c.Author < 0 || c.Author >= len(p.Users) {
			t.Fatalf("comment %d points nowhere: %+v", i, c)
		}
		if err := domain.ValidateCommentContent(c.Content); err != nil {
			t.Fatalf("comment %d: %v", i, err)
		}
	}
}

func TestNewPlan_LooksLikeARealProject(t *testing.T) {
	p := NewPlan(5, 2_000, 400)
	statuses := map[domain.Status]int{}
	withDeps, doneWithDeps, blocked, described := 0, 0, 0, 0
	for _, task := range p.Tasks {
		statuses[task.Status]++
		if len(task.Dependencies) > 0 {
			withDeps++
			if task.Status == domain.StatusDone {
				doneWithDeps++
			}
		}
		for _, d := range task.Dependencies {
			if task.Status != domain.StatusDone && p.Tasks[d].Status != domain.StatusDone {
				blocked++
				break
			}
		}
		described += len(task.Description)
	}
	for _, s := range []domain.Status{domain.StatusTodo, domain.StatusInProgress, domain.StatusInReview, domain.StatusDone} {
		if statuses[s] < 100 {
			t.Errorf("only %d of 2,000 tasks are %s", statuses[s], s)
		}
	}
	if withDeps < 200 || blocked < 50 {
		t.Errorf("%d tasks have dependencies and %d are blocked; want a visible share of both", withDeps, blocked)
	}
	// Finished work that waited on finished work: the seed has to move those
	// in dependency order, or the server rejects them.
	if doneWithDeps < 50 {
		t.Errorf("only %d done tasks have dependencies; want at least 50", doneWithDeps)
	}
	// At 10,000 tasks the project should pass 2 MB, as the brief requires.
	if avg := described / len(p.Tasks); avg < 210 {
		t.Errorf("descriptions average %d bytes; want at least 210 so 10k tasks pass 2 MB", avg)
	}
}
