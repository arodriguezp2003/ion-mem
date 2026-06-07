package mcp_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
)

func TestServer_profile_agent_registers_exactly_3_tools(t *testing.T) {
	st := mustStore(t)
	_, detOpt := mustFakeProject("ion-mem")
	ionSrv := mcp.New(st, mcp.WithProfile("agent"), detOpt)
	tools := ionSrv.ServerTools()
	if len(tools) != 3 {
		t.Errorf("agent profile: got %d tools, want 3", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Tool.GetName()] = true
	}
	for _, expected := range []string{"ion_current_project", "ion_save", "ion_search"} {
		if !names[expected] {
			t.Errorf("missing expected tool: %q", expected)
		}
	}
}

func TestServer_standard_envelope_fields_present(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "ion-mem", Source: "git_root", Path: "/repo"}, nil
	}))

	res := mustCall(t, ts, "ion_search", map[string]any{"query": "anything"})
	env := mustEnvelope(t, res)

	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := env[key]; !ok {
			t.Errorf("standard envelope field missing: %q", key)
		}
	}
	if _, hasData := env["data"]; hasData {
		t.Error("envelope must not have 'data' wrapper")
	}
}

func TestServer_ion_save_response_has_extra_fields(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "ion-mem", Source: "git_root", Path: "/repo"}, nil
	}))

	res := mustCall(t, ts, "ion_save", map[string]any{
		"title":   "Test observation",
		"content": "Some content",
	})
	env := mustEnvelope(t, res)

	for _, key := range []string{"id", "sync_id", "revision_count", "duplicate_count", "prompt_attached"} {
		if _, ok := env[key]; !ok {
			t.Errorf("ion_save response missing field: %q", key)
		}
	}
}
