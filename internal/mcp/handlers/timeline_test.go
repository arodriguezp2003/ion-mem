package handlers_test

import (
	"context"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// seedSession creates a new session in the store for testing.
func seedSession(t *testing.T, st *store.Store) string {
	t.Helper()
	sid := "sess-" + t.Name()
	_, err := st.CreateSession(context.Background(), store.CreateSessionParams{
		ID:      sid,
		Project: "ion-mem",
	})
	if err != nil {
		t.Fatalf("seedSession: %v", err)
	}
	return sid
}

// TestIonTimeline_WindowEntries verifies that anchoring at observation 5 with
// before=2, after=2 returns at most 4 surrounding entries (not including anchor).
func TestIonTimeline_WindowEntries(t *testing.T) {
	st := mustStore(t)
	sid := seedSession(t, st)

	// Seed 9 observations in order
	var ids []int64
	for i := 0; i < 9; i++ {
		obs, err := st.AddObservation(context.Background(), store.AddObservationParams{
			SessionID: sid,
			Type:      "manual",
			Title:     "Obs",
			Content:   "content",
			Project:   "ion-mem",
			Scope:     "project",
		})
		if err != nil {
			t.Fatalf("seed obs %d: %v", i, err)
		}
		ids = append(ids, obs.ID)
	}

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	// Anchor at the 5th observation (index 4), before=2, after=2
	anchorID := ids[4]
	res := callTool(t, ts, "ion_timeline", map[string]any{
		"observation_id": float64(anchorID),
		"before":         float64(2),
		"after":          float64(2),
	})
	env := decodeText(t, res)

	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := env[key]; !ok {
			t.Errorf("timeline envelope missing field: %q", key)
		}
	}

	entries, ok := env["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got: %T", env["entries"])
	}
	// before=2 + anchor + after=2 = 5, but store returns window [start:end] inclusive of anchor
	// So entries length should be <= 5
	if len(entries) > 5 {
		t.Errorf("entries: got %d, want <=5 (before=2 + anchor + after=2)", len(entries))
	}
	if len(entries) == 0 {
		t.Error("entries must not be empty when observations exist in session")
	}
}

// TestIonTimeline_EmptyBeforeAfterAreArrays verifies that when the anchor is
// the first observation (no entries before it), the entries slice is still
// a JSON array [] not null.
func TestIonTimeline_EmptyBeforeAfterAreArrays(t *testing.T) {
	st := mustStore(t)
	sid := seedSession(t, st)

	// Seed only 1 observation
	obs, err := st.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sid,
		Type:      "manual",
		Title:     "Only Obs",
		Content:   "only content",
		Project:   "ion-mem",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	// Request before=5 when anchor is the only entry — before window should be empty array
	res := callTool(t, ts, "ion_timeline", map[string]any{
		"observation_id": float64(obs.ID),
		"before":         float64(5),
		"after":          float64(0),
	})
	env := decodeText(t, res)

	// entries should be a non-null JSON array (even if it only contains the anchor)
	entries, ok := env["entries"].([]any)
	if !ok {
		t.Fatalf("entries must be a JSON array, got: %T (%v)", env["entries"], env["entries"])
	}
	// With only one obs and before=5, after=0, we get just the anchor
	if len(entries) == 0 {
		t.Error("entries must contain at least the anchor observation")
	}
}
