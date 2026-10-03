package changes

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/domain"
)

// decode turns the wire union into its concrete change type, e.g.
// apigen.TaskSetStatus. Re-encoding that value, as the event log does, drops
// any fields the contract doesn't define.
func decode(c apigen.Change) (any, error) {
	change, err := c.ValueByDiscriminator()
	if err != nil {
		return nil, invalid("type", "is not a known change type")
	}
	return change, nil
}

// validate checks a change's shape and field limits: everything that can be
// judged without the database. Failures are *domain.ValidationError (422).
func validate(change any) error {
	switch c := change.(type) {
	case apigen.ProjectUpdate:
		if c.Name == nil && c.Description == nil && c.Metadata == nil {
			return invalid("change", "has nothing to update")
		}
		if c.Name != nil {
			if err := domain.ValidateProjectName(*c.Name); err != nil {
				return err
			}
		}
		if c.Description != nil {
			return domain.ValidateProjectDescription(*c.Description)
		}
		return nil

	case apigen.TaskCreate:
		if err := requireID("id", c.Id); err != nil {
			return err
		}
		if err := domain.ValidateTaskTitle(c.Title); err != nil {
			return err
		}
		if c.Configuration != nil {
			if err := prefixed("configuration", validateConfiguration(*c.Configuration)); err != nil {
				return err
			}
		}
		if c.AssignedTo != nil {
			if err := validateIDs("assignedTo", *c.AssignedTo, uuid.Nil); err != nil {
				return err
			}
		}
		if c.Dependencies != nil {
			return validateIDs("dependencies", *c.Dependencies, c.Id)
		}
		return nil

	case apigen.TaskUpdate:
		if err := requireID("taskId", c.TaskId); err != nil {
			return err
		}
		if c.Title == nil && c.Priority == nil && c.Description == nil && c.CustomFields == nil {
			return invalid("change", "has nothing to update")
		}
		if c.Title != nil {
			if err := domain.ValidateTaskTitle(*c.Title); err != nil {
				return err
			}
		}
		if c.Priority != nil && !domain.Priority(*c.Priority).Valid() {
			return invalid("priority", "must be low, medium, high or urgent")
		}
		if c.Description != nil {
			if err := domain.ValidateTaskDescription(*c.Description); err != nil {
				return err
			}
		}
		if c.CustomFields != nil {
			return domain.ValidateCustomFieldsPatch(patchMap(*c.CustomFields))
		}
		return nil

	case apigen.TaskSetStatus:
		if err := requireID("taskId", c.TaskId); err != nil {
			return err
		}
		if !domain.Status(c.To).Valid() {
			return invalid("to", "must be todo, in_progress, in_review or done")
		}
		return nil

	case apigen.TaskDelete:
		return requireID("taskId", c.TaskId)

	case apigen.TaskAddAssignee:
		return requireIDs("taskId", c.TaskId, "userId", c.UserId)
	case apigen.TaskRemoveAssignee:
		return requireIDs("taskId", c.TaskId, "userId", c.UserId)

	case apigen.TaskAddTag:
		if err := requireID("taskId", c.TaskId); err != nil {
			return err
		}
		return domain.ValidateTag(c.Tag)
	case apigen.TaskRemoveTag:
		if err := requireID("taskId", c.TaskId); err != nil {
			return err
		}
		return domain.ValidateTag(c.Tag)

	case apigen.TaskAddDependency:
		return validateLink(c.TaskId, c.DependsOnId)
	case apigen.TaskRemoveDependency:
		return validateLink(c.TaskId, c.DependsOnId)

	case apigen.CommentCreate:
		if err := requireIDs("id", c.Id, "taskId", c.TaskId); err != nil {
			return err
		}
		return domain.ValidateCommentContent(c.Content)
	}
	return invalid("type", "is not a known change type")
}

func validateConfiguration(c apigen.TaskCreateConfiguration) error {
	if c.Priority != nil && !domain.Priority(*c.Priority).Valid() {
		return invalid("priority", "must be low, medium, high or urgent")
	}
	if c.Description != nil {
		if err := domain.ValidateTaskDescription(*c.Description); err != nil {
			return err
		}
	}
	if c.Tags != nil {
		if err := domain.ValidateTags(*c.Tags); err != nil {
			return err
		}
	}
	if c.CustomFields != nil {
		return domain.ValidateCustomFields(*c.CustomFields)
	}
	return nil
}

func validateLink(taskID, dependsOnID uuid.UUID) error {
	if err := requireIDs("taskId", taskID, "dependsOnId", dependsOnID); err != nil {
		return err
	}
	if taskID == dependsOnID {
		return invalid("dependsOnId", "a task can't depend on itself")
	}
	return nil
}

// validateIDs checks a list of ids: none zero, none repeated, and none equal
// to self (pass uuid.Nil when any id is allowed).
func validateIDs(field string, ids []uuid.UUID, self uuid.UUID) error {
	seen := make(map[uuid.UUID]bool, len(ids))
	for i, id := range ids {
		item := fmt.Sprintf("%s[%d]", field, i)
		switch {
		case id == uuid.Nil:
			return invalid(item, "is required")
		case seen[id]:
			return invalid(item, "is listed twice")
		case self != uuid.Nil && id == self:
			return invalid(item, "a task can't depend on itself")
		}
		seen[id] = true
	}
	return nil
}

func requireID(field string, id uuid.UUID) error {
	if id == uuid.Nil {
		return invalid(field, "is required")
	}
	return nil
}

func requireIDs(field1 string, id1 uuid.UUID, field2 string, id2 uuid.UUID) error {
	if err := requireID(field1, id1); err != nil {
		return err
	}
	return requireID(field2, id2)
}

func invalid(field, message string) error {
	return &domain.ValidationError{Field: field, Message: message}
}

// prefixed nests a validation error's field under prefix, e.g. "tags[1]"
// becomes "configuration.tags[1]".
func prefixed(prefix string, err error) error {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return &domain.ValidationError{Field: prefix + "." + ve.Field, Message: ve.Message}
	}
	return err
}

// patchMap converts the wire form of a custom-field patch, where JSON null
// arrives as a nil pointer, into the domain's form, where it is a nil value.
func patchMap(patch apigen.CustomFieldsPatch) map[string]any {
	m := make(map[string]any, len(patch))
	for key, value := range patch {
		if value == nil {
			m[key] = nil
		} else {
			m[key] = *value
		}
	}
	return m
}
