// Package seed builds a realistic demo project and writes it through the
// same change pipeline the API uses, so its event log, seqs and rules are
// exactly what real use would produce.
package seed

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/domain"
)

// Plan is a demo project, decided up front: the same random seed always
// gives the same project.
type Plan struct {
	Users    []string
	Project  ProjectPlan
	Tasks    []TaskPlan
	Comments []CommentPlan
}

type ProjectPlan struct {
	Name        string
	Description string
}

type TaskPlan struct {
	Title        string
	Priority     apigen.Priority
	Description  string
	Tags         []string
	CustomFields map[string]any
	Assignees    []int // indexes into Plan.Users
	Dependencies []int // indexes of earlier tasks, so the graph has no loops
	Status       domain.Status
}

type CommentPlan struct {
	Task    int // index into Plan.Tasks
	Author  int // index into Plan.Users
	Content string
}

var (
	users  = []string{"Ana", "Raj", "Mei", "Tomás", "Priya", "Kwame"}
	verbs  = []string{"Design", "Build", "Fix", "Refactor", "Test", "Document", "Profile", "Ship", "Review", "Migrate"}
	things = []string{
		"the sign-up flow", "password reset", "the billing page", "invoice emails", "search ranking",
		"the activity feed", "push notifications", "the settings screen", "CSV export", "audit logging",
		"rate limiting", "the onboarding checklist", "team invitations", "dark mode", "the public API",
		"webhook retries", "the mobile nav", "image uploads", "SSO login", "usage analytics",
	}
	areas = []string{"web", "mobile", "api", "billing", "growth", "platform", "data", "security"}
	tags  = []string{"frontend", "backend", "infra", "design", "bug", "tech-debt", "security", "performance", "docs", "ux", "data", "mobile"}

	sentences = []string{
		"Customers on the %s team keep running into this, and support has flagged it three times this month.",
		"Start from the current %s implementation and keep the change small enough to review in one sitting.",
		"Acceptance: the flow works on the latest Chrome, Safari and Firefox, and the %s dashboards stay green.",
		"Write down any follow-up work as separate tasks rather than growing this one; %s owns the rollout.",
		"Measure before and after so the %s review can see the impact, and attach the numbers here.",
		"There is a rough prototype from last quarter; reuse what still holds up and delete the rest (%s).",
		"Coordinate with %s before merging, since the release train cuts on Thursday afternoon.",
		"Edge cases worth a test: empty states, slow networks, and accounts migrated from the old %s system.",
	}
	remarks = []string{
		"Looks good to me.", "I can pick this up tomorrow.", "Pushed a first draft, feedback welcome.",
		"This needs a design review before we start.", "Blocked until the API change lands.",
		"Moved the estimate up after looking at the code.", "Paired on this today; nearly there.",
		"Can we split this into two smaller tasks?", "Shipped behind a flag, watching the metrics.",
		"Found an edge case with migrated accounts, adding a test.",
	}
	customers = []string{"Acme", "Globex", "Initech", "Umbrella", "Hooli", "Stark"}
)

// NewPlan plans a project of the given size from a random seed.
func NewPlan(randomSeed uint64, tasks, comments int) Plan {
	r := rand.New(rand.NewPCG(randomSeed, 0x5eed))
	p := Plan{
		Users: users,
		Project: ProjectPlan{
			Name:        fmt.Sprintf("Demo: %s tasks", thousands(tasks)),
			Description: "Seeded demo data: a product team's backlog, with dependencies, comments and every status.",
		},
		Tasks:    make([]TaskPlan, tasks),
		Comments: make([]CommentPlan, comments),
	}
	for i := range p.Tasks {
		p.Tasks[i] = planTask(r, p.Tasks[:i], i)
	}
	for i := range p.Comments {
		p.Comments[i] = CommentPlan{
			Task:    r.IntN(tasks),
			Author:  r.IntN(len(users)),
			Content: pick(r, remarks),
		}
	}
	return p
}

func planTask(r *rand.Rand, earlier []TaskPlan, i int) TaskPlan {
	area := pick(r, areas)
	t := TaskPlan{
		Title:       fmt.Sprintf("%s %s (%s) #%d", pick(r, verbs), pick(r, things), area, i+1),
		Priority:    pick(r, []apigen.Priority{apigen.Low, apigen.Medium, apigen.Medium, apigen.High, apigen.Urgent}),
		Description: describe(r, area),
		Tags:        sample(r, tags, r.IntN(4)),
		Assignees:   indexes(r, len(users), r.IntN(3)),
	}
	if r.Float64() < 0.6 {
		t.CustomFields = map[string]any{
			"estimate":  float64(pick(r, []int{1, 2, 3, 5, 8, 13})),
			"component": area,
		}
		if r.Float64() < 0.3 {
			t.CustomFields["customer"] = pick(r, customers)
			t.CustomFields["billable"] = r.Float64() < 0.5
		}
	}
	// Where the task ends up, then what it waits on: finished work waits on
	// finished work; anything else may wait on anything.
	switch x := r.Float64(); {
	case x < 0.22:
		t.Status = domain.StatusDone
	case x < 0.55:
		t.Status = domain.StatusInProgress
	case x < 0.67:
		t.Status = domain.StatusInReview
	default:
		t.Status = domain.StatusTodo
	}
	// A quarter of the tasks wait on one or two of the 50 tasks before them.
	if i >= 5 && r.Float64() < 0.25 {
		var candidates []int
		for j := i - min(i, 50); j < i; j++ {
			if t.Status != domain.StatusDone || earlier[j].Status == domain.StatusDone {
				candidates = append(candidates, j)
			}
		}
		for _, k := range indexes(r, len(candidates), 1+r.IntN(2)) {
			t.Dependencies = append(t.Dependencies, candidates[k])
		}
	}
	return t
}

// describe writes a paragraph of 230–500 characters.
func describe(r *rand.Rand, area string) string {
	var b strings.Builder
	for b.Len() < 230 {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, pick(r, sentences), area)
	}
	return b.String()
}

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.IntN(len(xs))] }

// sample returns n distinct items of xs, in xs's order.
func sample[T any](r *rand.Rand, xs []T, n int) []T {
	var out []T
	for _, i := range indexes(r, len(xs), n) {
		out = append(out, xs[i])
	}
	return out
}

// indexes returns n distinct numbers below limit, in increasing order.
func indexes(r *rand.Rand, limit, n int) []int {
	n = min(n, limit)
	chosen := r.Perm(limit)[:n]
	out := make([]int, 0, n)
	for i := range limit {
		for _, c := range chosen {
			if c == i {
				out = append(out, i)
			}
		}
	}
	return out
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
