package handlers_test

import (
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

func TestGetObservation_happy_path_returns_observation_fields(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Seed one observation.
	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-getobs", Project: "myproj"})
	obs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "Auth model",
		Content:   "Use JWT",
		Project:   "myproj",
		Scope:     "project",
	})

	res := callTool(t, ts, "ion_get_observation", map[string]any{"id": obs.ID})
	env := decodeText(t, res)

	nested, ok := env["observation"].(map[string]any)
	if !ok {
		t.Fatalf("ion_get_observation: missing or wrong type for 'observation', got %T", env["observation"])
	}
	if nested["title"] != "Auth model" {
		t.Errorf("observation.title = %q, want %q", nested["title"], "Auth model")
	}
	if nested["content"] != "Use JWT" {
		t.Errorf("observation.content = %q, want %q", nested["content"], "Use JWT")
	}
}

func TestGetObservation_missing_id_returns_envelope_error_not_go_error(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_get_observation", map[string]any{"id": int64(9999)})
	env := decodeText(t, res)

	// Must still have envelope fields — no Go error.
	if _, ok := env["project"]; !ok {
		t.Fatal("ion_get_observation: missing 'project' on error envelope")
	}
	result, _ := env["result"].(string)
	if !strings.Contains(strings.ToLower(result), "not found") {
		t.Errorf("ion_get_observation: result %q should contain 'not found'", result)
	}
}
