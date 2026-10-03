package domain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestValidateCustomFieldsPatch(t *testing.T) {
	sets := func(n int) map[string]any {
		m := make(map[string]any, n)
		for i := range n {
			m[fmt.Sprintf("k%d", i)] = 1.0
		}
		return m
	}
	twentySetsFiveRemoves := sets(20)
	for i := range 5 {
		twentySetsFiveRemoves[fmt.Sprintf("gone%d", i)] = nil
	}
	long := strings.Repeat("k", 41)

	tests := []struct {
		name  string
		in    map[string]any
		field string
	}{
		{"none", nil, ""},
		{"set and remove", map[string]any{"team": "core", "old": nil}, ""},
		{"remove only", map[string]any{"old": nil}, ""},
		{"twenty sets, five removes", twentySetsFiveRemoves, ""},
		{"twenty-one sets", sets(21), "customFields"},
		{"empty key", map[string]any{"": nil}, "customFields"},
		{"long key on a set", map[string]any{long: 1.0}, "customFields." + long},
		{"long key on a remove", map[string]any{long: nil}, "customFields." + long},
		{"bad value", map[string]any{"note": map[string]any{}}, "customFields.note"},
		{"long string", map[string]any{"note": strings.Repeat("a", 501)}, "customFields.note"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantField(t, ValidateCustomFieldsPatch(tt.in), tt.field)
		})
	}
}

func TestMergePatch(t *testing.T) {
	current := map[string]any{"team": "core", "points": 3.0, "billable": true}
	patch := map[string]any{"points": 5.0, "billable": nil, "missing": nil, "owner": "ana"}

	got := MergePatch(current, patch)

	want := map[string]any{"team": "core", "points": 5.0, "owner": "ana"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergePatch = %v, want %v", got, want)
	}
	// The input is left alone; the event log and the caller may still hold it.
	if !reflect.DeepEqual(current, map[string]any{"team": "core", "points": 3.0, "billable": true}) {
		t.Errorf("current was modified: %v", current)
	}
}

func TestMergePatch_NilCurrent(t *testing.T) {
	got := MergePatch(nil, map[string]any{"a": 1.0, "b": nil})
	if want := map[string]any{"a": 1.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("MergePatch = %v, want %v", got, want)
	}
}
