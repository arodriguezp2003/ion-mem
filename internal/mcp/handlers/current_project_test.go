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
	if m["error"] != "ambiguous_project" {
		t.Errorf("error = %q, want %q", m["error"], "ambiguous_project")
	}
	if _, ok := m["available_projects"]; !ok {
		t.Error("available_projects missing from ambiguous response")
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
