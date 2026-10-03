package changes

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/domain"
)

func ptr[T any](v T) *T { return &v }

func wantInvalid(t *testing.T, err error, field string) {
	t.Helper()
	if field == "" {
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		return
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *domain.ValidationError for %q", err, field)
	}
	if ve.Field != field {
		t.Errorf("Field = %q (%s), want %q", ve.Field, ve.Message, field)
	}
}

func TestDecode(t *testing.T) {
	var c apigen.Change
	if err := json.Unmarshal([]byte(`{"type":"task.setStatus","taskId":"0190b9a0-0000-7000-8000-000000000001","to":"done"}`), &c); err != nil {
		t.Fatal(err)
	}
	got, err := decode(c)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got.(apigen.TaskSetStatus); !ok || s.To != apigen.Done {
		t.Errorf("decode = %#v, want TaskSetStatus to done", got)
	}
}

func TestDecode_UnknownType(t *testing.T) {
	for _, raw := range []string{`{"type":"task.archive","taskId":"x"}`, `{"taskId":"x"}`} {
		var c apigen.Change
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatal(err)
		}
		_, err := decode(c)
		wantInvalid(t, err, "type")
	}
}

func TestValidate(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	task, user, other := id(), id(), id()
	long := func(n int) string { return strings.Repeat("a", n) }

	tests := []struct {
		name   string
		change any
		field  string // "" means valid
	}{
		// project.update
		{"project rename", apigen.ProjectUpdate{Name: ptr("Launch v2")}, ""},
		{"project metadata only", apigen.ProjectUpdate{Metadata: &apigen.MetadataPatch{"color": "red", "old": nil}}, ""},
		{"project nothing to change", apigen.ProjectUpdate{}, "change"},
		{"project blank name", apigen.ProjectUpdate{Name: ptr(" ")}, "name"},
		{"project long description", apigen.ProjectUpdate{Description: ptr(long(2001))}, "description"},

		// task.create
		{"create minimal", apigen.TaskCreate{Id: task, Title: "Write tests"}, ""},
		{"create full", apigen.TaskCreate{Id: task, Title: "T",
			Configuration: &apigen.TaskCreateConfiguration{
				Priority: ptr(apigen.High), Description: ptr("d"), Tags: &[]string{"api"},
				CustomFields: &apigen.CustomFields{"points": 3.0},
			},
			AssignedTo: &[]uuid.UUID{user}, Dependencies: &[]uuid.UUID{other}}, ""},
		{"create no id", apigen.TaskCreate{Title: "T"}, "id"},
		{"create no title", apigen.TaskCreate{Id: task}, "title"},
		{"create bad priority", apigen.TaskCreate{Id: task, Title: "T",
			Configuration: &apigen.TaskCreateConfiguration{Priority: ptr(apigen.Priority("critical"))}}, "configuration.priority"},
		{"create long description", apigen.TaskCreate{Id: task, Title: "T",
			Configuration: &apigen.TaskCreateConfiguration{Description: ptr(long(10001))}}, "configuration.description"},
		{"create duplicate tag", apigen.TaskCreate{Id: task, Title: "T",
			Configuration: &apigen.TaskCreateConfiguration{Tags: &[]string{"a", "a"}}}, "configuration.tags[1]"},
		{"create null custom field", apigen.TaskCreate{Id: task, Title: "T",
			Configuration: &apigen.TaskCreateConfiguration{CustomFields: &apigen.CustomFields{"x": nil}}}, "configuration.customFields.x"},
		{"create zero assignee", apigen.TaskCreate{Id: task, Title: "T", AssignedTo: &[]uuid.UUID{user, uuid.Nil}}, "assignedTo[1]"},
		{"create duplicate assignee", apigen.TaskCreate{Id: task, Title: "T", AssignedTo: &[]uuid.UUID{user, user}}, "assignedTo[1]"},
		{"create depends on itself", apigen.TaskCreate{Id: task, Title: "T", Dependencies: &[]uuid.UUID{other, task}}, "dependencies[1]"},
		{"create duplicate dependency", apigen.TaskCreate{Id: task, Title: "T", Dependencies: &[]uuid.UUID{other, other}}, "dependencies[1]"},

		// task.update
		{"update title", apigen.TaskUpdate{TaskId: task, Title: ptr("New")}, ""},
		{"update custom fields", apigen.TaskUpdate{TaskId: task, CustomFields: &apigen.CustomFieldsPatch{"points": ptr[any](5.0), "old": nil}}, ""},
		{"update no task", apigen.TaskUpdate{Title: ptr("New")}, "taskId"},
		{"update nothing to change", apigen.TaskUpdate{TaskId: task}, "change"},
		{"update empty title", apigen.TaskUpdate{TaskId: task, Title: ptr("")}, "title"},
		{"update bad priority", apigen.TaskUpdate{TaskId: task, Priority: ptr(apigen.Priority("p0"))}, "priority"},
		{"update long description", apigen.TaskUpdate{TaskId: task, Description: ptr(long(10001))}, "description"},
		{"update bad custom field", apigen.TaskUpdate{TaskId: task, CustomFields: &apigen.CustomFieldsPatch{"x": ptr[any]([]any{})}}, "customFields.x"},

		// task.setStatus, task.delete
		{"set status", apigen.TaskSetStatus{TaskId: task, To: apigen.InReview}, ""},
		{"set unknown status", apigen.TaskSetStatus{TaskId: task, To: "archived"}, "to"},
		{"set status no task", apigen.TaskSetStatus{To: apigen.Done}, "taskId"},
		{"delete", apigen.TaskDelete{TaskId: task}, ""},
		{"delete no task", apigen.TaskDelete{}, "taskId"},

		// assignees
		{"add assignee", apigen.TaskAddAssignee{TaskId: task, UserId: user}, ""},
		{"add assignee no user", apigen.TaskAddAssignee{TaskId: task}, "userId"},
		{"remove assignee no task", apigen.TaskRemoveAssignee{UserId: user}, "taskId"},

		// tags
		{"add tag", apigen.TaskAddTag{TaskId: task, Tag: "api"}, ""},
		{"add blank tag", apigen.TaskAddTag{TaskId: task, Tag: " "}, "tag"},
		{"add long tag", apigen.TaskAddTag{TaskId: task, Tag: long(31)}, "tag"},
		{"remove tag", apigen.TaskRemoveTag{TaskId: task, Tag: "api"}, ""},
		{"remove tag no task", apigen.TaskRemoveTag{Tag: "api"}, "taskId"},

		// dependencies
		{"add dependency", apigen.TaskAddDependency{TaskId: task, DependsOnId: other}, ""},
		{"add dependency on itself", apigen.TaskAddDependency{TaskId: task, DependsOnId: task}, "dependsOnId"},
		{"add dependency no target", apigen.TaskAddDependency{TaskId: task}, "dependsOnId"},
		{"remove dependency", apigen.TaskRemoveDependency{TaskId: task, DependsOnId: other}, ""},
		{"remove dependency no task", apigen.TaskRemoveDependency{DependsOnId: other}, "taskId"},

		// comment.create
		{"comment", apigen.CommentCreate{Id: id(), TaskId: task, Content: "Looks good"}, ""},
		{"comment no id", apigen.CommentCreate{TaskId: task, Content: "hi"}, "id"},
		{"comment no task", apigen.CommentCreate{Id: id(), Content: "hi"}, "taskId"},
		{"comment blank", apigen.CommentCreate{Id: id(), TaskId: task, Content: "  "}, "content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantInvalid(t, validate(tt.change), tt.field)
		})
	}
}
