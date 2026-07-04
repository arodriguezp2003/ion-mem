package handlers_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
)

func TestCurrentProject_returns_detection_result_directly(t *testing.T) {
	st := mustStore(t)
	det := project.DetectionResult{
		Project: "ion-mem",
		Source:  "git_remote",
		Path:    "/repo/ion-mem",
	}
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, nil
	}))

	res := callTool(t, ts, "ion_current_project", map[string]any{})
	m := decodeText(t, res)

	// ion_current_project returns DetectionResult directly — no standard "result" wrapper.
	if m["project"] != "ion-mem" {
		t.Errorf("project = %q, want %q", m["project"], "ion-mem")
	}
	if m["project_source"] != "git_remote" {
		t.Errorf("project_source = %q, want %q", m["project_source"], "git_remote")
	}
}

func TestCurrentProject_ambiguous_cwd_returns_error_in_body_not_go_error(t *testing.T) {
	st := mustStore(t)
	det := project.DetectionResult{
		Project:           "",
		Source:            "ambiguous",
		Path:              "/workspace",
		AvailableProjects: []string{"alpha", "beta"},
	}
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, project.ErrAmbiguousProject
	}))

	// Must NOT return a Go-level error — error must be in the body.
	res := callTool(t, ts, "ion_current_project", map[string]any{})
	if res == nil {
		t.Fatal("expected non-nil result even for ambiguous project")
	}

	m := decodeText(t, res)
	if m["project"] != "" {
		t.Errorf("ambiguous project should have empty project, got %q", m["project"])
	}
	if m["error"] != "project_ambiguous" {
		t.Errorf("error = %q, want %q", m["error"], "project_ambiguous")
	}
	if _, ok := m["available_projects"]; !ok {
		t.Error("available_projects missing from ambiguous response")
	}
}

// TestCurrentProject_AmbiguousErrorValue asserts the aligned vocabulary value
// "project_ambiguous" (not the pre-change "ambiguous_project").
// This test drives task 1.3/1.4: RED here, GREEN after renaming the literal.
func TestCurrentProject_AmbiguousErrorValue(t *testing.T) {
	st := mustStore(t)
	det := project.DetectionResult{
		Project:           "",
		Source:            "ambiguous",
		Path:              "/workspace",
		AvailableProjects: []string{"alpha", "beta"},
	}
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, project.ErrAmbiguousProject
	}))

	res := callTool(t, ts, "ion_current_project", map[string]any{})
	m := decodeText(t, res)

	// R-ENV-03 / R-TOOL-CURRENT-03: error value must be "project_ambiguous".
	if m["error"] != "project_ambiguous" {
		t.Errorf("error = %q, want %q", m["error"], "project_ambiguous")
	}
	// Flat shape: no status, no error_code, no result envelope fields.
	if _, ok := m["status"]; ok {
		t.Error("ion_current_project must not include 'status' envelope field")
	}
	if _, ok := m["error_code"]; ok {
		t.Error("ion_current_project must not include 'error_code' envelope field")
	}
}

func TestCurrentProject_cwd_argument_used_for_detection(t *testing.T) {
	st := mustStore(t)
	var capturedCwd string
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(cwd string) (project.DetectionResult, error) {
		capturedCwd = cwd
		return project.DetectionResult{Project: "from-cwd", Source: "git_root", Path: cwd}, nil
	}))

	callTool(t, ts, "ion_current_project", map[string]any{"cwd": "/some/path"})
	if capturedCwd != "/some/path" {
		t.Errorf("detect called with cwd=%q, want %q", capturedCwd, "/some/path")
	}
}
