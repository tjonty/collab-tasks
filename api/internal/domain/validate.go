package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Priority is how urgent a task is.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// Field limits. Lengths count characters, like Postgres char_length; the
// migration's CHECK constraints repeat the ones it can express.
const (
	MaxDisplayName        = 40
	MaxProjectName        = 100
	MaxProjectDescription = 2_000
	MaxTaskTitle          = 200
	MaxTaskDescription    = 10_000
	MaxCommentContent     = 5_000
	MaxTags               = 20
	MaxTag                = 30
	MaxCustomFields       = 20
	MaxCustomFieldKey     = 40
	MaxCustomFieldString  = 500
)

// ValidationError names the field that broke a limit. It maps to a 422
// validation_failed response.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Valid reports whether s is one of the four workflow statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusInReview, StatusDone:
		return true
	}
	return false
}

// Valid reports whether p is one of the four priorities.
func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return true
	}
	return false
}

// required checks a text that must have at least one non-space character.
func required(field, s string, max int) error {
	if strings.TrimSpace(s) == "" {
		return invalid(field, "is required")
	}
	return optional(field, s, max)
}

// optional checks a text that may be empty.
func optional(field, s string, max int) error {
	if n := utf8.RuneCountInString(s); n > max {
		return invalid(field, "is %d characters, max %d", n, max)
	}
	return nil
}

func ValidateDisplayName(s string) error { return required("displayName", s, MaxDisplayName) }
func ValidateProjectName(s string) error { return required("name", s, MaxProjectName) }
func ValidateProjectDescription(s string) error {
	return optional("description", s, MaxProjectDescription)
}
func ValidateTaskTitle(s string) error       { return required("title", s, MaxTaskTitle) }
func ValidateTaskDescription(s string) error { return optional("description", s, MaxTaskDescription) }
func ValidateCommentContent(s string) error  { return required("content", s, MaxCommentContent) }
func ValidateTag(s string) error             { return required("tag", s, MaxTag) }

// ValidateTags checks a whole tag set: at most MaxTags, each valid, no
// duplicates.
func ValidateTags(tags []string) error {
	if len(tags) > MaxTags {
		return invalid("tags", "has %d tags, max %d", len(tags), MaxTags)
	}
	seen := make(map[string]bool, len(tags))
	for i, tag := range tags {
		field := fmt.Sprintf("tags[%d]", i)
		if err := required(field, tag, MaxTag); err != nil {
			return err
		}
		if seen[tag] {
			return invalid(field, "duplicates %q", tag)
		}
		seen[tag] = true
	}
	return nil
}

// ValidateCustomFields checks a whole custom-field map: at most
// MaxCustomFields keys, and each value a string, number or boolean. Values
// are expected as encoding/json decodes them (float64 or json.Number).
func ValidateCustomFields(fields map[string]any) error {
	if len(fields) > MaxCustomFields {
		return invalid("customFields", "has %d keys, max %d", len(fields), MaxCustomFields)
	}
	for key, value := range fields {
		if err := validateCustomFieldKey(key); err != nil {
			return err
		}
		if err := validateCustomFieldValue(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateCustomFieldKey(key string) error {
	if key == "" {
		return invalid("customFields", "has an empty key")
	}
	if n := utf8.RuneCountInString(key); n > MaxCustomFieldKey {
		return invalid("customFields."+key, "key is %d characters, max %d", n, MaxCustomFieldKey)
	}
	return nil
}

func validateCustomFieldValue(key string, value any) error {
	field := "customFields." + key
	switch v := value.(type) {
	case string:
		return optional(field, v, MaxCustomFieldString)
	case float64, json.Number, bool:
		return nil
	default:
		return invalid(field, "must be a string, number or boolean")
	}
}
