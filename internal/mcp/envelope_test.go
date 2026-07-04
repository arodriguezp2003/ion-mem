package mcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

func TestBuild_required_fields_present(t *testing.T) {
	det := project.DetectionResult{
		Project: "ion-mem",
		Source:  "git_remote",
		Path:    "/repo",
	}
	raw := Build(det, "ok", nil)

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Build returned invalid JSON: %v", err)
	}

	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing required field: %q", key)
		}
	}
	if m["project"] != "ion-mem" {
		t.Errorf("project = %q, want %q", m["project"], "ion-mem")
	}
	if m["project_source"] != "git_remote" {
		t.Errorf("project_source = %q, want %q", m["project_source"], "git_remote")
	}
	if m["project_path"] != "/repo" {
		t.Errorf("project_path = %q, want %q", m["project_path"], "/repo")
	}
	if m["result"] != "ok" {
		t.Errorf("result = %q, want %q", m["result"], "ok")
	}
}

func TestBuild_extensions_merged_at_top_level(t *testing.T) {
	det := project.DetectionResult{
		Project: "ion-mem",
		Source:  "git_root",
		Path:    "/repo",
	}
	extras := map[string]any{
		"id":    int64(42),
		"count": 3,
	}
	raw := Build(det, "saved", extras)

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Build returned invalid JSON: %v", err)
	}

	// Extensions must be at top level, NOT under a "data" key.
	if _, hasData := m["data"]; hasData {
		t.Error("extensions must not be nested under 'data'")
	}
	if _, hasID := m["id"]; !hasID {
		t.Error("extension field 'id' missing from top level")
	}
	if _, hasCount := m["count"]; !hasCount {
		t.Error("extension field 'count' missing from top level")
	}
}

func TestBuild_no_extensions_is_valid(t *testing.T) {
	det := project.DetectionResult{
		Project: "myproj",
		Source:  "dir_basename",
		Path:    "/tmp/myproj",
	}
	raw := Build(det, "done", nil)

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Build returned invalid JSON: %v", err)
	}
	// Five standard keys: project, project_source, project_path, result, status.
	if len(m) != 5 {
		t.Errorf("expected exactly 5 keys when no extensions, got %d: %v", len(m), m)
	}
}

// --- Task 1.1: RED tests for status field and BuildError ---

func TestBuild_StatusOk(t *testing.T) {
	det := project.DetectionResult{Project: "p", Source: "git_root", Path: "/repo"}
	raw := Build(det, "some result", nil)

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Build returned invalid JSON: %v", err)
	}
	if m["status"] != "ok" {
		t.Errorf("status = %v, want %q", m["status"], "ok")
	}
	if _, hasCode := m["error_code"]; hasCode {
		t.Error("Build must not include error_code on success")
	}
}

func TestBuild_NoExtensions_Has5Keys(t *testing.T) {
	det := project.DetectionResult{Project: "p", Source: "src", Path: "/p"}
	raw := Build(det, "done", nil)

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Now five required keys: project, project_source, project_path, result, status.
	if len(m) != 5 {
		t.Errorf("expected 5 keys with no extras, got %d: %v", len(m), m)
	}
}

var buildErrorCases = []struct {
	name      string
	code      string
	result    string
}{
	{"not_found code", CodeNotFound, "observation 99 not found"},
	{"db_error code", CodeDBError, "db failure"},
	{"invalid_argument code", CodeInvalidArgument, "empty content"},
	{"project_ambiguous code", CodeProjectAmbiguous, "ambiguous project"},
	{"internal code", CodeInternal, "unexpected error"},
}

func TestBuildError_SetsStatusErrorAndCode(t *testing.T) {
	det := project.DetectionResult{Project: "p", Source: "git_root", Path: "/repo"}

	for _, tc := range buildErrorCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := BuildError(det, tc.code, tc.result)

			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("BuildError returned invalid JSON: %v", err)
			}
			if m["status"] != "error" {
				t.Errorf("status = %v, want %q", m["status"], "error")
			}
			if m["error_code"] != tc.code {
				t.Errorf("error_code = %v, want %q", m["error_code"], tc.code)
			}
			if m["result"] != tc.result {
				t.Errorf("result = %v, want %q", m["result"], tc.result)
			}
			// Standard fields must still be present.
			for _, key := range []string{"project", "project_source", "project_path"} {
				if _, ok := m[key]; !ok {
					t.Errorf("missing required field: %q", key)
				}
			}
		})
	}
}

func TestErrorCode_SentinelMapping(t *testing.T) {
	cases := []struct {
		err      error
		wantCode string
	}{
		{store.ErrObservationNotFound, CodeNotFound},
		{store.ErrNotFound, CodeNotFound},
		{store.ErrPromptNotFound, CodeNotFound},
		{errors.New("some random db error"), CodeDBError},
	}

	for _, tc := range cases {
		got := errorCode(tc.err)
		if got != tc.wantCode {
			t.Errorf("errorCode(%v) = %q, want %q", tc.err, got, tc.wantCode)
		}
	}
}

// --- End Task 1.1 tests ---

func TestBuild_ambiguous_project_shape(t *testing.T) {
	det := project.DetectionResult{
		Project:           "",
		Source:            "ambiguous",
		Path:              "/workspace",
		AvailableProjects: []string{"alpha", "beta"},
	}
	extras := map[string]any{
		"available_projects": det.AvailableProjects,
	}
	raw := Build(det, "ambiguous project — call ion_current_project", extras)

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Build returned invalid JSON: %v", err)
	}
	if m["project"] != "" {
		t.Errorf("project = %q, want empty string for ambiguous", m["project"])
	}
	if m["project_source"] != "ambiguous" {
		t.Errorf("project_source = %q, want %q", m["project_source"], "ambiguous")
	}
	if _, ok := m["available_projects"]; !ok {
		t.Error("available_projects missing from ambiguous envelope")
	}
}
