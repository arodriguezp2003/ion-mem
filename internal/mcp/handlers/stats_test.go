package handlers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestIonStats_ReflectsCurrentState verifies that ion_stats returns counts
// matching the store state: 2 sessions, 5 observations, 1 prompt.
func TestIonStats_ReflectsCurrentState(t *testing.T) {
	st := mustStore(t)

	// Seed 2 sessions
	sid1 := "stats-session-1"
	sid2 := "stats-session-2"
	for _, sid := range []string{sid1, sid2} {
		if _, err := st.CreateSession(context.Background(), store.CreateSessionParams{
			ID:      sid,
			Project: "ion-mem",
		}); err != nil {
			t.Fatalf("CreateSession(%q): %v", sid, err)
		}
	}

	// Seed 5 observations (different content to avoid dedup)
	for i := 0; i < 5; i++ {
		_, err := st.AddObservation(context.Background(), store.AddObservationParams{
			SessionID: sid1,
			Type:      "manual",
			Title:     "Obs",
			Content:   fmt.Sprintf("content-%d", i),
			Project:   "ion-mem",
			Scope:     "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}

	// Seed 1 prompt
	_, err := st.AddPromptIfMissing(context.Background(), store.AddPromptParams{
		SessionID: sid1,
		Content:   "test prompt content",
		Project:   "ion-mem",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_stats", map[string]any{})
	env := decodeText(t, res)

	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := env[key]; !ok {
			t.Errorf("stats envelope missing field: %q", key)
		}
	}

	stats, ok := env["stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected stats object in response, got: %v", env["stats"])
	}

	totalSessions, _ := stats["total_sessions"].(float64)
	if totalSessions != 2 {
		t.Errorf("total_sessions: got %v, want 2", totalSessions)
	}

	totalObservations, _ := stats["total_observations"].(float64)
	if totalObservations != 5 {
		t.Errorf("total_observations: got %v, want 5", totalObservations)
	}

	totalPrompts, _ := stats["total_prompts"].(float64)
	if totalPrompts != 1 {
		t.Errorf("total_prompts: got %v, want 1", totalPrompts)
	}

	if _, ok := stats["by_project"]; !ok {
		t.Error("stats must contain by_project array")
	}
}
