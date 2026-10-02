// Package domain holds the task rules that don't need a database: the status
// workflow, the Done rule, Blocked, and field limits.
package domain

import "errors"

// Status is where a task is in the workflow.
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusDone       Status = "done"
)

var (
	// ErrInvalidTransition means the workflow doesn't allow the move.
	ErrInvalidTransition = errors.New("invalid_transition")
	// ErrDependenciesNotDone means a task can't become Done yet because at
	// least one of its dependencies isn't Done.
	ErrDependenciesNotDone = errors.New("dependencies_not_done")
)

// transitions lists the allowed moves out of each status. Staying put is not
// a move.
var transitions = map[Status][]Status{
	StatusTodo:       {StatusInProgress},
	StatusInProgress: {StatusTodo, StatusInReview, StatusDone},
	StatusInReview:   {StatusInProgress, StatusDone},
	StatusDone:       {StatusInProgress},
}

// CanMoveTo reports whether a task may move from current to to, given the
// statuses of the tasks it depends on.
// Only a move into Done looks at the dependencies; starting work never does.
func CanMoveTo(current, to Status, depStatuses []Status) error {
	if !allowed(current, to) {
		return ErrInvalidTransition
	}
	if to == StatusDone && !allDone(depStatuses) {
		return ErrDependenciesNotDone
	}
	return nil
}

// IsBlocked reports whether a task is waiting on another one. It's computed
// on read, never stored.
func IsBlocked(status Status, depStatuses []Status) bool {
	return status != StatusDone && !allDone(depStatuses)
}

func allowed(current, to Status) bool {
	for _, next := range transitions[current] {
		if next == to {
			return true
		}
	}
	return false
}

func allDone(statuses []Status) bool {
	for _, s := range statuses {
		if s != StatusDone {
			return false
		}
	}
	return true
}
