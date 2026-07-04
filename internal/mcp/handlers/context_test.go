package handlers_test

import (
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

func TestContext_non_empty_markdown_when_observations_exist(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Seed 3 observations so context has something to show.
	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-ctx-1", Project: "myproj"})
	for _, title := range []string{"alpha", "beta", "gamma"} {
		_, _ = st.AddObservation(ctx, store.AddObservationParams{
			SessionID: sess.ID,
			Type:      "manual",
			Title:     title,
			Content:   "content of " + title,
			Project:   "myproj",
			Scope:     "project",
		})
	}

	res := callTool(t, ts, "ion_context", map[string]any{})
	env := decodeText(t, res)

	result, _ := env["result"].(string)
	if result == "" {
		t.Error("ion_context: result is empty, want non-empty markdown")
	}
}

func TestContext_empty_store_returns_valid_empty_markdown_not_error(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_context", map[string]any{})
	env := decodeText(t, res)

	// Must return envelope with standard fields — no Go-level error.
	if _, ok := env["project"]; !ok {
		t.Fatal("ion_context: missing 'project' field — envelope not returned")
	}
	if _, ok := env["result"]; !ok {
		t.Fatal("ion_context: missing 'result' field — envelope not returned")
	}
}

func TestContext_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_context", map[string]any{})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

func TestContext_result_contains_section_headers(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Seed a session and observation so we get markdown content.
	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-ctx-hdr", Project: "myproj"})
	_, _ = st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "manual",
		Title:     "test obs",
		Content:   "test content",
		Project:   "myproj",
		Scope:     "project",
	})

	res := callTool(t, ts, "ion_context", map[string]any{})
	env := decodeText(t, res)

	result, _ := env["result"].(string)
	if !strings.Contains(result, "#") {
		t.Errorf("ion_context: result %q has no markdown headers", result)
	}
}
