package handlers_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/mcp"
	"github.com/arodriguezp2003/ion-mem/internal/project"
	"github.com/arodriguezp2003/ion-mem/internal/store"
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

func TestGetObservation_SupersededObservationIncludesStatusAndNote(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-getobs-status", Project: "myproj"})
	oldObs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "old", Content: "old", Project: "myproj", Scope: "project",
	})
	newObs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "new", Content: "new", Project: "myproj", Scope: "project",
	})
	if err := st.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "replaced"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	res := callTool(t, ts, "ion_get_observation", map[string]any{"id": oldObs.ID})
	env := decodeText(t, res)
	nested, ok := env["observation"].(map[string]any)
	if !ok {
		t.Fatalf("ion_get_observation: missing 'observation', got %T", env["observation"])
	}
	if nested["status"] != "superseded" {
		t.Errorf("observation.status = %v, want %q", nested["status"], "superseded")
	}
	if int64(nested["superseded_by"].(float64)) != newObs.ID {
		t.Errorf("observation.superseded_by = %v, want %d", nested["superseded_by"], newObs.ID)
	}
	wantNote := fmt.Sprintf("superseded by #%d", newObs.ID)
	if nested["note"] != wantNote {
		t.Errorf("observation.note = %v, want %q", nested["note"], wantNote)
	}
}

// TestGetObservation_DanglingSupersededByLabelsNoteUnavailable verifies that
// when the observation a row points at via superseded_by has itself since
// been soft-deleted, the note says so instead of implying the replacement
// still exists.
func TestGetObservation_DanglingSupersededByLabelsNoteUnavailable(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-getobs-dangling", Project: "myproj"})
	oldObs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "old", Content: "old", Project: "myproj", Scope: "project",
	})
	newObs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "new", Content: "new", Project: "myproj", Scope: "project",
	})
	if err := st.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "replaced"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}
	// The replacement itself is later soft-deleted — the pointer dangles.
	if err := st.DeleteObservation(ctx, newObs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	res := callTool(t, ts, "ion_get_observation", map[string]any{"id": oldObs.ID})
	env := decodeText(t, res)
	nested, ok := env["observation"].(map[string]any)
	if !ok {
		t.Fatalf("ion_get_observation: missing 'observation', got %T", env["observation"])
	}
	wantNote := fmt.Sprintf("superseded by #%d (target no longer available)", newObs.ID)
	if nested["note"] != wantNote {
		t.Errorf("observation.note = %v, want %q", nested["note"], wantNote)
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

func TestGetObservation_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-status-ok", Project: "myproj"})
	obs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "manual", Title: "Status Test", Content: "c", Project: "myproj", Scope: "project",
	})

	res := callTool(t, ts, "ion_get_observation", map[string]any{"id": obs.ID})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q on success", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

func TestGetObservation_StatusErrorNotFound(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_get_observation", map[string]any{"id": int64(99999)})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q for not-found", env["status"], "error")
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}
