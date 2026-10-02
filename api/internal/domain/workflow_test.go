package domain

import (
	"errors"
	"testing"
)

func TestCanMoveTo_Workflow(t *testing.T) {
	// Every from→to pair, with no dependencies, so only the workflow decides.
	tests := []struct {
		from, to Status
		allowed  bool
	}{
		{StatusTodo, StatusTodo, false},
		{StatusTodo, StatusInProgress, true},
		{StatusTodo, StatusInReview, false},
		{StatusTodo, StatusDone, false},

		{StatusInProgress, StatusTodo, true},
		{StatusInProgress, StatusInProgress, false},
		{StatusInProgress, StatusInReview, true},
		{StatusInProgress, StatusDone, true},

		{StatusInReview, StatusTodo, false},
		{StatusInReview, StatusInProgress, true},
		{StatusInReview, StatusInReview, false},
		{StatusInReview, StatusDone, true},

		{StatusDone, StatusTodo, false},
		{StatusDone, StatusInProgress, true},
		{StatusDone, StatusInReview, false},
		{StatusDone, StatusDone, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			err := CanMoveTo(tt.from, tt.to, nil)
			if tt.allowed && err != nil {
				t.Errorf("CanMoveTo = %v, want nil", err)
			}
			if !tt.allowed && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("CanMoveTo = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

func TestCanMoveTo_DoneRule(t *testing.T) {
	tests := []struct {
		name     string
		from, to Status
		deps     []Status
		want     error
	}{
		{"done with no deps", StatusInProgress, StatusDone, nil, nil},
		{"done with all deps done", StatusInReview, StatusDone, []Status{StatusDone, StatusDone}, nil},
		{"done with one dep open", StatusInProgress, StatusDone, []Status{StatusDone, StatusInReview}, ErrDependenciesNotDone},
		{"done from review with dep todo", StatusInReview, StatusDone, []Status{StatusTodo}, ErrDependenciesNotDone},
		// Starting work is never blocked.
		{"start with deps open", StatusTodo, StatusInProgress, []Status{StatusTodo}, nil},
		{"review with deps open", StatusInProgress, StatusInReview, []Status{StatusInProgress}, nil},
		{"reopen with deps open", StatusDone, StatusInProgress, []Status{StatusTodo}, nil},
		// The workflow is checked before the dependencies.
		{"illegal move with deps open", StatusTodo, StatusDone, []Status{StatusTodo}, ErrInvalidTransition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanMoveTo(tt.from, tt.to, tt.deps)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Errorf("CanMoveTo = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIsBlocked(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		deps   []Status
		want   bool
	}{
		{"no deps", StatusTodo, nil, false},
		{"all deps done", StatusInProgress, []Status{StatusDone, StatusDone}, false},
		{"one dep open", StatusTodo, []Status{StatusDone, StatusInReview}, true},
		{"in review, dep todo", StatusInReview, []Status{StatusTodo}, true},
		// A Done task stays Done, and is never shown as blocked, even after a
		// dependency is reopened.
		{"done with dep reopened", StatusDone, []Status{StatusInProgress}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBlocked(tt.status, tt.deps); got != tt.want {
				t.Errorf("IsBlocked = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanMoveTo_UnknownStatus(t *testing.T) {
	tests := []struct{ from, to Status }{
		{"archived", StatusTodo},
		{StatusTodo, "archived"},
		{"", StatusInProgress},
	}
	for _, tt := range tests {
		if err := CanMoveTo(tt.from, tt.to, nil); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("CanMoveTo(%q, %q) = %v, want ErrInvalidTransition", tt.from, tt.to, err)
		}
	}
}
