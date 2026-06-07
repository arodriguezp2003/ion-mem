package mcp_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
)

// TestIonFullLifecycle_E2E exercises the full session lifecycle in order:
// session_start → save_prompt → save (prompt attached) → search → get_observation →
// context → session_summary (ends session) → session_end (already ended) → stats.
// Asserts each step returns a well-formed envelope and the final stats match.
func TestIonFullLifecycle_E2E(t *testing.T) {
	st := mustStore(t)
	_, detOpt := mustFakeProject("ion-mem")
	_, ts := mustTestServer(t, st, detOpt)

	const sessionID = "e2e-lifecycle-session"

	// Step 1: ion_session_start
	r1 := mustEnvelope(t, mustCall(t, ts, "ion_session_start", map[string]any{
		"session_id": sessionID,
	}))
	assertEnvelopeShape(t, "ion_session_start", r1)
	if r1["created"] != true {
		t.Errorf("ion_session_start: created must be true for new session, got %v", r1["created"])
	}

	// Step 2: ion_save_prompt
	r2 := mustEnvelope(t, mustCall(t, ts, "ion_save_prompt", map[string]any{
		"session_id": sessionID,
		"content":    "Tell me about persistent memory in Go",
	}))
	assertEnvelopeShape(t, "ion_save_prompt", r2)
	if r2["id"] == nil {
		t.Error("ion_save_prompt: expected id in response")
	}

	// Step 3: ion_save with capture_prompt=true — prompt should attach
	r3 := mustEnvelope(t, mustCall(t, ts, "ion_save", map[string]any{
		"session_id":     sessionID,
		"title":          "E2E Lifecycle Observation",
		"content":        "Test observation for lifecycle test",
		"capture_prompt": true,
	}))
	assertEnvelopeShape(t, "ion_save", r3)
	obsID, ok := r3["id"].(float64)
	if !ok || obsID == 0 {
		t.Fatalf("ion_save: expected valid id, got %v", r3["id"])
	}
	if r3["prompt_attached"] != true {
		t.Errorf("ion_save: prompt_attached must be true when prompt was buffered, got %v", r3["prompt_attached"])
	}

	// Step 4: ion_search
	r4 := mustEnvelope(t, mustCall(t, ts, "ion_search", map[string]any{
		"query":        "lifecycle observation",
		"all_projects": true,
	}))
	assertEnvelopeShape(t, "ion_search", r4)
	if r4["results"] == nil {
		t.Error("ion_search: expected results field")
	}

	// Step 5: ion_get_observation
	r5 := mustEnvelope(t, mustCall(t, ts, "ion_get_observation", map[string]any{
		"id": obsID,
	}))
	assertEnvelopeShape(t, "ion_get_observation", r5)
	obs, ok := r5["observation"].(map[string]any)
	if !ok {
		t.Fatalf("ion_get_observation: expected observation object, got %v", r5["observation"])
	}
	if obs["title"] != "E2E Lifecycle Observation" {
		t.Errorf("ion_get_observation: title mismatch: %v", obs["title"])
	}

	// Step 6: ion_context
	r6 := mustEnvelope(t, mustCall(t, ts, "ion_context", map[string]any{}))
	assertEnvelopeShape(t, "ion_context", r6)
	result6, _ := r6["result"].(string)
	if result6 == "" {
		t.Error("ion_context: result must be non-empty markdown")
	}

	// Step 7: ion_session_summary (also ends the session)
	r7 := mustEnvelope(t, mustCall(t, ts, "ion_session_summary", map[string]any{
		"session_id": sessionID,
		"content":    "Session completed: tested full lifecycle",
	}))
	assertEnvelopeShape(t, "ion_session_summary", r7)
	if r7["observation_id"] == nil {
		t.Error("ion_session_summary: expected observation_id in response")
	}

	// Step 8: ion_session_end (already ended via summary — envelope.result confirms)
	r8 := mustEnvelope(t, mustCall(t, ts, "ion_session_end", map[string]any{
		"session_id": sessionID,
	}))
	assertEnvelopeShape(t, "ion_session_end", r8)
	// May succeed (idempotent) or return an error message — either is acceptable.
	result8, _ := r8["result"].(string)
	if result8 == "" {
		t.Error("ion_session_end: result field must be non-empty")
	}

	// Step 9: ion_stats — assert final counts
	r9 := mustEnvelope(t, mustCall(t, ts, "ion_stats", map[string]any{}))
	assertEnvelopeShape(t, "ion_stats", r9)
	stats, ok := r9["stats"].(map[string]any)
	if !ok {
		t.Fatalf("ion_stats: expected stats object, got %v", r9["stats"])
	}

	// 1 session started
	if totalSessions, _ := stats["total_sessions"].(float64); totalSessions < 1 {
		t.Errorf("ion_stats: total_sessions=%v, want >=1", totalSessions)
	}

	// 2 observations: one from ion_save, one from ion_session_summary
	if totalObs, _ := stats["total_observations"].(float64); totalObs < 2 {
		t.Errorf("ion_stats: total_observations=%v, want >=2", totalObs)
	}

	// 1 prompt from ion_save_prompt
	if totalPrompts, _ := stats["total_prompts"].(float64); totalPrompts < 1 {
		t.Errorf("ion_stats: total_prompts=%v, want >=1", totalPrompts)
	}
}

// assertEnvelopeShape verifies the 4 required standard envelope fields are present.
func assertEnvelopeShape(t *testing.T, tool string, env map[string]any) {
	t.Helper()
	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := env[key]; !ok {
			t.Errorf("%s: envelope missing field %q", tool, key)
		}
	}
}

func TestServer_AgentAndAllProfileExactlyFourteenTools(t *testing.T) {
	st := mustStore(t)
	_, detOpt := mustFakeProject("ion-mem")

	for _, profile := range []string{"agent", "all"} {
		t.Run("profile="+profile, func(t *testing.T) {
			ionSrv := mcp.New(st, mcp.WithProfile(profile), detOpt)
			tools := ionSrv.ServerTools()
			if len(tools) != 14 {
				t.Errorf("profile %q: got %d tools, want 14", profile, len(tools))
			}
			names := make(map[string]bool)
			for _, tool := range tools {
				names[tool.Tool.GetName()] = true
			}
			expectedTools := []string{
				"ion_current_project",
				"ion_save",
				"ion_search",
				"ion_context",
				"ion_get_observation",
				"ion_session_start",
				"ion_session_end",
				"ion_session_summary",
				"ion_save_prompt",
				"ion_suggest_topic_key",
				"ion_update",
				"ion_delete",
				"ion_timeline",
				"ion_stats",
			}
			for _, expected := range expectedTools {
				if !names[expected] {
					t.Errorf("missing expected tool: %q", expected)
				}
			}
		})
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
