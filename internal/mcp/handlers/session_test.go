package handlers_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// --- ion_session_start ---

func TestSessionStart_new_session_created_true(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_session_start", map[string]any{
		"session_id": "test-start-1",
	})
	env := decodeText(t, res)

	if env["session_id"] != "test-start-1" {
		t.Errorf("session_id = %v, want %q", env["session_id"], "test-start-1")
	}
	if env["created"] != true {
		t.Errorf("created = %v, want true for new session", env["created"])
	}
}

func TestSessionStart_duplicate_id_is_idempotent_created_false(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// First call creates the session.
	callTool(t, ts, "ion_session_start", map[string]any{"session_id": "dup-session"})

	// Second call with the same ID must be idempotent — no error, created:false.
	res := callTool(t, ts, "ion_session_start", map[string]any{"session_id": "dup-session"})
	env := decodeText(t, res)

	if env["session_id"] != "dup-session" {
		t.Errorf("session_id = %v, want %q", env["session_id"], "dup-session")
	}
	if env["created"] != false {
		t.Errorf("created = %v, want false for duplicate session", env["created"])
	}
}

// --- ion_session_end ---

func TestSessionEnd_known_session_returns_ended_at(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Create the session first.
	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "end-me", Project: "myproj"})

	res := callTool(t, ts, "ion_session_end", map[string]any{
		"session_id": "end-me",
		"summary":    "all done",
	})
	env := decodeText(t, res)

	if env["session_id"] != "end-me" {
		t.Errorf("session_id = %v, want %q", env["session_id"], "end-me")
	}
	if env["ended_at"] == "" || env["ended_at"] == nil {
		t.Error("ended_at is empty, want non-empty timestamp")
	}
}

func TestSessionEnd_unknown_session_id_returns_envelope_error_not_go_error(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_session_end", map[string]any{
		"session_id": "does-not-exist",
	})
	env := decodeText(t, res)

	// Must have envelope fields — no Go error.
	if _, ok := env["project"]; !ok {
		t.Fatal("ion_session_end: missing 'project' on error envelope")
	}
	result, _ := env["result"].(string)
	if result == "" {
		t.Error("ion_session_end: result is empty on unknown session_id, want error message")
	}
}

// --- ion_session_summary ---

func TestSessionSummary_with_session_id_stores_observation_type_session_summary(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Create a session to attach the summary to.
	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "summ-sess", Project: "myproj"})

	res := callTool(t, ts, "ion_session_summary", map[string]any{
		"session_id": "summ-sess",
		"summary":    "## Goal\n\nFixed bug.\n\n## Accomplished\n- Fixed it",
	})
	env := decodeText(t, res)

	obsID, ok := env["observation_id"]
	if !ok {
		t.Fatal("ion_session_summary: missing 'observation_id' in response")
	}
	if obsID.(float64) <= 0 {
		t.Errorf("observation_id = %v, want > 0", obsID)
	}

	// Verify the stored observation has type=session_summary.
	storedObs, err := st.GetObservation(ctx, int64(obsID.(float64)))
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if storedObs.Type != "session_summary" {
		t.Errorf("observation type = %q, want %q", storedObs.Type, "session_summary")
	}
}

func TestSessionSummary_with_session_id_also_calls_store_EndSession(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "summ-end-sess", Project: "myproj"})

	callTool(t, ts, "ion_session_summary", map[string]any{
		"session_id": "summ-end-sess",
		"summary":    "all done",
	})

	// After ion_session_summary with session_id, session must be ended.
	sess, err := st.GetSession(ctx, "summ-end-sess")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "ended" {
		t.Errorf("session status = %q, want %q after ion_session_summary", sess.Status, "ended")
	}
}

func TestSessionSummary_without_session_id_auto_creates_session(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_session_summary", map[string]any{
		"summary": "no explicit session",
	})
	env := decodeText(t, res)

	sid, ok := env["session_id"]
	if !ok || sid == "" {
		t.Error("ion_session_summary: session_id absent or empty when auto-created")
	}
	obsID, ok := env["observation_id"]
	if !ok {
		t.Fatal("ion_session_summary: missing 'observation_id'")
	}
	if obsID.(float64) <= 0 {
		t.Errorf("observation_id = %v, want > 0", obsID)
	}
}

// --- Status field assertions ---

func TestSessionStart_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_session_start", map[string]any{"session_id": "status-start-1"})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

func TestSessionEnd_StatusErrorNotFound(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_session_end", map[string]any{"session_id": "does-not-exist-status"})
	env := decodeText(t, res)

	// session_end on unknown session ID → not_found
	if env["status"] != "error" {
		t.Errorf("status = %v, want %q for unknown session", env["status"], "error")
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}

// --- session_ended field in ion_session_summary ---

// TestSessionSummary_with_session_id_returns_session_ended_true verifies that
// the response includes session_ended:true when the session is successfully ended.
func TestSessionSummary_with_session_id_returns_session_ended_true(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "se-ok-sess", Project: "myproj"})

	res := callTool(t, ts, "ion_session_summary", map[string]any{
		"session_id": "se-ok-sess",
		"summary":    "done",
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if env["session_ended"] != true {
		t.Errorf("session_ended = %v, want true when session ends successfully", env["session_ended"])
	}
}

// TestSessionSummary_session_end_failure_still_saves_observation verifies that
// when EndSession fails (injected error), the observation IS still saved
// (status: ok), session_ended is false, and the result text mentions the failure.
func TestSessionSummary_session_end_failure_still_saves_observation(t *testing.T) {
	st := mustStore(t)
	endErr := fmt.Errorf("synthetic end failure")
	_, ts := mustTestServer(t, st,
		mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
			return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
		}),
		mcp.WithEndSessionFn(func(_ context.Context, _, _ string) error {
			return endErr
		}),
	)

	res := callTool(t, ts, "ion_session_summary", map[string]any{
		"session_id": "inject-err-sess",
		"summary":    "done",
	})
	env := decodeText(t, res)

	// Observation must be saved — status ok.
	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q (observation saved despite end failure)", env["status"], "ok")
	}
	// session_ended must be false.
	if env["session_ended"] != false {
		t.Errorf("session_ended = %v, want false when end fails", env["session_ended"])
	}
	// Result text must mention the failure.
	result, _ := env["result"].(string)
	if !strings.Contains(result, "session end failed") {
		t.Errorf("result = %q, want to contain 'session end failed'", result)
	}
}

// --- cross-tool integration for session_summary side effects ---

func TestSessionSummary_result_contains_session_id(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{ID: "sid-check", Project: "myproj"})

	res := callTool(t, ts, "ion_session_summary", map[string]any{
		"session_id": "sid-check",
		"summary":    "done",
	})
	env := decodeText(t, res)

	if env["session_id"] != "sid-check" {
		t.Errorf("session_id = %v, want %q", env["session_id"], "sid-check")
	}
	result, _ := env["result"].(string)
	if !strings.Contains(strings.ToLower(result), "summary") && result == "" {
		t.Error("result is empty, want success message")
	}
}
