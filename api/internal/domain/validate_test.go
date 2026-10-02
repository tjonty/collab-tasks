package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// wantField checks that err is a *ValidationError naming field, or nil when
// field is "".
func wantField(t *testing.T, err error, field string) {
	t.Helper()
	if field == "" {
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		return
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError for %q", err, field)
	}
	if ve.Field != field {
		t.Errorf("Field = %q, want %q", ve.Field, field)
	}
}

func TestTextLimits(t *testing.T) {
	tests := []struct {
		name  string
		check func(string) error
		in    string
		field string // "" means valid
	}{
		{"display name ok", ValidateDisplayName, "Ana", ""},
		{"display name empty", ValidateDisplayName, "", "displayName"},
		{"display name blank", ValidateDisplayName, "   ", "displayName"},
		{"display name 40", ValidateDisplayName, strings.Repeat("a", 40), ""},
		{"display name 41", ValidateDisplayName, strings.Repeat("a", 41), "displayName"},
		// Limits count characters, not bytes, like Postgres char_length.
		{"display name 40 multibyte", ValidateDisplayName, strings.Repeat("é", 40), ""},

		{"project name ok", ValidateProjectName, "Launch", ""},
		{"project name empty", ValidateProjectName, "", "name"},
		{"project name 100", ValidateProjectName, strings.Repeat("a", 100), ""},
		{"project name 101", ValidateProjectName, strings.Repeat("a", 101), "name"},

		{"project description empty", ValidateProjectDescription, "", ""},
		{"project description 2000", ValidateProjectDescription, strings.Repeat("a", 2000), ""},
		{"project description 2001", ValidateProjectDescription, strings.Repeat("a", 2001), "description"},

		{"title ok", ValidateTaskTitle, "Write tests", ""},
		{"title empty", ValidateTaskTitle, "", "title"},
		{"title blank", ValidateTaskTitle, "\t\n", "title"},
		{"title 200", ValidateTaskTitle, strings.Repeat("a", 200), ""},
		{"title 201", ValidateTaskTitle, strings.Repeat("a", 201), "title"},

		{"task description empty", ValidateTaskDescription, "", ""},
		{"task description 10000", ValidateTaskDescription, strings.Repeat("a", 10000), ""},
		{"task description 10001", ValidateTaskDescription, strings.Repeat("a", 10001), "description"},

		{"comment ok", ValidateCommentContent, "Looks good", ""},
		{"comment empty", ValidateCommentContent, "", "content"},
		{"comment blank", ValidateCommentContent, " ", "content"},
		{"comment 5000", ValidateCommentContent, strings.Repeat("a", 5000), ""},
		{"comment 5001", ValidateCommentContent, strings.Repeat("a", 5001), "content"},

		{"tag ok", ValidateTag, "backend", ""},
		{"tag empty", ValidateTag, "", "tag"},
		{"tag blank", ValidateTag, "  ", "tag"},
		{"tag 30", ValidateTag, strings.Repeat("a", 30), ""},
		{"tag 31", ValidateTag, strings.Repeat("a", 31), "tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantField(t, tt.check(tt.in), tt.field)
		})
	}
}

func TestValidateTags(t *testing.T) {
	twenty := make([]string, 20)
	for i := range twenty {
		twenty[i] = fmt.Sprintf("t%d", i)
	}
	tests := []struct {
		name  string
		in    []string
		field string
	}{
		{"none", nil, ""},
		{"some", []string{"api", "web"}, ""},
		{"twenty", twenty, ""},
		{"twenty-one", append(append([]string{}, twenty...), "extra"), "tags"},
		{"duplicate", []string{"api", "web", "api"}, "tags[2]"},
		{"bad item", []string{"api", ""}, "tags[1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantField(t, ValidateTags(tt.in), tt.field)
		})
	}
}

func TestValidateCustomFields(t *testing.T) {
	twenty := make(map[string]any, 20)
	for i := range 20 {
		twenty[fmt.Sprintf("k%d", i)] = true
	}
	twentyOne := map[string]any{"k20": true}
	for k, v := range twenty {
		twentyOne[k] = v
	}
	tests := []struct {
		name  string
		in    map[string]any
		field string
	}{
		{"none", nil, ""},
		{"string number bool", map[string]any{"estimate": 3.5, "team": "core", "billable": false}, ""},
		{"json number", map[string]any{"points": json.Number("8")}, ""},
		{"twenty keys", twenty, ""},
		{"twenty-one keys", twentyOne, "customFields"},
		{"empty key", map[string]any{"": "x"}, "customFields"},
		{"key 40", map[string]any{strings.Repeat("k", 40): 1.0}, ""},
		{"key 41", map[string]any{strings.Repeat("k", 41): 1.0}, "customFields." + strings.Repeat("k", 41)},
		{"string 500", map[string]any{"note": strings.Repeat("a", 500)}, ""},
		{"string 501", map[string]any{"note": strings.Repeat("a", 501)}, "customFields.note"},
		{"null value", map[string]any{"note": nil}, "customFields.note"},
		{"object value", map[string]any{"note": map[string]any{"a": 1.0}}, "customFields.note"},
		{"array value", map[string]any{"note": []any{"a"}}, "customFields.note"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantField(t, ValidateCustomFields(tt.in), tt.field)
		})
	}
}

func TestEnumsValid(t *testing.T) {
	for _, s := range []Status{StatusTodo, StatusInProgress, StatusInReview, StatusDone} {
		if !s.Valid() {
			t.Errorf("Status(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []Status{"", "archived", "Done"} {
		if s.Valid() {
			t.Errorf("Status(%q).Valid() = true, want false", s)
		}
	}
	for _, p := range []Priority{PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent} {
		if !p.Valid() {
			t.Errorf("Priority(%q).Valid() = false, want true", p)
		}
	}
	for _, p := range []Priority{"", "critical", "HIGH"} {
		if p.Valid() {
			t.Errorf("Priority(%q).Valid() = true, want false", p)
		}
	}
}
