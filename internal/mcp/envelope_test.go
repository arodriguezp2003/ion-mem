package mcp

import (
	"encoding/json"
	"testing"

	"github.com/ionix/ion-mem/internal/project"
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
	if len(m) != 4 {
		t.Errorf("expected exactly 4 keys when no extensions, got %d: %v", len(m), m)
	}
}

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
