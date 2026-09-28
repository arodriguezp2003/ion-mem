package handlers_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/mcp"
	"github.com/arodriguezp2003/ion-mem/internal/project"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

func TestSetStatus_SupersededHappyPath(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-set-status", Project: "myproj"})
	oldObs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "old", Content: "old content", Project: "myproj", Scope: "project",
	})
	newObs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "new", Content: "new content", Project: "myproj", Scope: "project",
	})

	res := callTool(t, ts, "ion_set_status", map[string]any{
		"id": oldObs.ID, "status": "superseded", "superseded_by": newObs.ID, "reason": "replaced",
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("ion_set_status: status = %v, want ok (result=%v)", env["status"], env["result"])
	}
	nested, ok := env["observation"].(map[string]any)
	if !ok {
		t.Fatalf("ion_set_status: missing 'observation', got %T", env["observation"])
	}
	if nested["status"] != "superseded" {
		t.Errorf("observation.status = %v, want %q", nested["status"], "superseded")
	}
	if int64(nested["superseded_by"].(float64)) != newObs.ID {
		t.Errorf("observation.superseded_by = %v, want %d", nested["superseded_by"], newObs.ID)
	}
	if nested["status_reason"] != "replaced" {
		t.Errorf("observation.status_reason = %v, want %q", nested["status_reason"], "replaced")
	}
	wantNote := fmt.Sprintf("superseded by #%d", newObs.ID)
	if nested["note"] != wantNote {
		t.Errorf("observation.note = %v, want %q", nested["note"], wantNote)
	}
}

func TestSetStatus_RefusesNonActiveSupersededByTarget(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-set-status-chain", Project: "myproj"})
	obs1, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "obs1", Content: "c1", Project: "myproj", Scope: "project",
	})
	obs2, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "obs2", Content: "c2", Project: "myproj", Scope: "project",
	})
	obs3, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "obs3", Content: "c3", Project: "myproj", Scope: "project",
	})
	if err := st.SetObservationStatus(ctx, obs1.ID, store.StatusSuperseded, &obs2.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus obs1->obs2: %v", err)
	}

	// obs3 -> obs1 must be rejected: obs1 is now superseded, not active.
	res := callTool(t, ts, "ion_set_status", map[string]any{
		"id": obs3.ID, "status": "superseded", "superseded_by": obs1.ID,
	})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Fatalf("ion_set_status: status = %v, want error", env["status"])
	}
	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "invalid_argument")
	}
	wantMsg := fmt.Sprintf("superseded_by target #%d is superseded; point at the newest active row", obs1.ID)
	result, _ := env["result"].(string)
	if !strings.Contains(result, wantMsg) {
		t.Errorf("result = %q, want it to contain %q", result, wantMsg)
	}
}

func TestSetStatus_RefusesObsoleteForBugfix(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-permanent", Project: "myproj"})
	fix, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "bugfix", Title: "fix", Content: "root cause", Project: "myproj", Scope: "project",
	})

	res := callTool(t, ts, "ion_set_status", map[string]any{"id": fix.ID, "status": "obsolete"})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Fatalf("ion_set_status: status = %v, want error", env["status"])
	}
	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "invalid_argument")
	}
	result, _ := env["result"].(string)
	if !strings.Contains(strings.ToLower(result), "permanent") {
		t.Errorf("result %q should mention permanence", result)
	}
}

func TestSetStatus_MissingIDReturnsNotFound(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_set_status", map[string]any{"id": int64(99999), "status": "active"})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Fatalf("ion_set_status: status = %v, want error", env["status"])
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}

func TestSetStatus_InvalidStatusValue(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sess, _ := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-invalid", Project: "myproj"})
	obs, _ := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "t", Content: "c", Project: "myproj", Scope: "project",
	})

	res := callTool(t, ts, "ion_set_status", map[string]any{"id": obs.ID, "status": "archived"})
	env := decodeText(t, res)

	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "invalid_argument")
	}
}
