package handlers_test

import (
	"context"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// seedTopicObs upserts a topic-key observation n times with changing titles/content,
// returning the final observation. All upserts go to the same topic key within project "ion-mem".
func seedTopicObs(t *testing.T, st *store.Store, sessID, topic string, n int) store.Observation {
	t.Helper()
	var obs store.Observation
	for i := 0; i < n; i++ {
		var err error
		obs, err = st.AddObservation(context.Background(), store.AddObservationParams{
			SessionID: sessID,
			Type:      "architecture",
			Title:     "Title v" + intToDecStr(i+1),
			Content:   "Content v" + intToDecStr(i+1),
			Project:   "ion-mem",
			Scope:     "project",
			TopicKey:  topic,
		})
		if err != nil {
			t.Fatalf("seedTopicObs upsert %d: %v", i, err)
		}
	}
	return obs
}

// intToDecStr converts a small int to a decimal string without importing strconv.
func intToDecStr(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}

// TestIonHistory_ThreeUpsertsTwoRevisions verifies that 3 upserts produce 2
// revisions returned newest-first with the OLD titles.
func TestIonHistory_ThreeUpsertsTwoRevisions(t *testing.T) {
	st := mustStore(t)
	sid := mustSeedSession(t, st, "ion-mem")
	obs := seedTopicObs(t, st, sid, "history/three", 3)

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_history", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; full: %v", env["status"], env)
	}

	revisions, ok := env["revisions"].([]any)
	if !ok {
		t.Fatalf("revisions must be array, got %T", env["revisions"])
	}
	if len(revisions) != 2 {
		t.Fatalf("expected 2 revisions for 3 upserts, got %d", len(revisions))
	}

	// Newest-first: revisions[0] should be Title v2 (archived when v3 was written).
	rev0, ok := revisions[0].(map[string]any)
	if !ok {
		t.Fatalf("revisions[0] must be object, got %T", revisions[0])
	}
	if rev0["title"] != "Title v2" {
		t.Errorf("revisions[0].title = %q, want %q", rev0["title"], "Title v2")
	}

	rev1, ok := revisions[1].(map[string]any)
	if !ok {
		t.Fatalf("revisions[1] must be object, got %T", revisions[1])
	}
	if rev1["title"] != "Title v1" {
		t.Errorf("revisions[1].title = %q, want %q", rev1["title"], "Title v1")
	}
}

// TestIonHistory_MissingIDNotFound verifies that a non-existent id produces
// status:"error" + error_code:"not_found" and no Go error.
func TestIonHistory_MissingIDNotFound(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_history", map[string]any{"id": float64(999999)})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want error", env["status"])
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want not_found", env["error_code"])
	}
}

// TestIonHistory_NoRevisions verifies that a freshly created observation (no
// subsequent upserts) returns status:"ok" with an empty revisions array (not null).
func TestIonHistory_NoRevisions(t *testing.T) {
	st := mustStore(t)
	sid := mustSeedSession(t, st, "ion-mem")
	obs := seedTopicObs(t, st, sid, "history/single", 1)

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_history", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok", env["status"])
	}
	revisions, ok := env["revisions"].([]any)
	if !ok {
		t.Fatalf("revisions must be array (even empty), got %T (%v)", env["revisions"], env["revisions"])
	}
	if len(revisions) != 0 {
		t.Errorf("expected 0 revisions for fresh obs, got %d", len(revisions))
	}

	count, _ := env["count"].(float64)
	if count != 0 {
		t.Errorf("count = %v, want 0", count)
	}
}

// TestIonHistory_ContentPreviewCapped verifies that long content is truncated
// to at most 300 characters in the content_preview field.
func TestIonHistory_ContentPreviewCapped(t *testing.T) {
	st := mustStore(t)
	sid := mustSeedSession(t, st, "ion-mem")

	// First upsert: long content that should be capped.
	longContent := ""
	for i := 0; i < 350; i++ {
		longContent += "x"
	}
	obs, err := st.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sid,
		Type:      "architecture",
		Title:     "Long Content v1",
		Content:   longContent,
		Project:   "ion-mem",
		Scope:     "project",
		TopicKey:  "history/longcontent",
	})
	if err != nil {
		t.Fatalf("first AddObservation: %v", err)
	}
	// Second upsert: triggers capture of longContent.
	obs2, err := st.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sid,
		Type:      "architecture",
		Title:     "Long Content v2",
		Content:   "short",
		Project:   "ion-mem",
		Scope:     "project",
		TopicKey:  "history/longcontent",
	})
	if err != nil {
		t.Fatalf("second AddObservation: %v", err)
	}
	_ = obs

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_history", map[string]any{"id": float64(obs2.ID)})
	env := decodeText(t, res)

	revisions, ok := env["revisions"].([]any)
	if !ok || len(revisions) == 0 {
		t.Fatalf("expected at least 1 revision, got: %v", env["revisions"])
	}
	rev, ok := revisions[0].(map[string]any)
	if !ok {
		t.Fatalf("revision must be object, got %T", revisions[0])
	}
	preview, ok := rev["content_preview"].(string)
	if !ok {
		t.Fatalf("content_preview must be string, got %T", rev["content_preview"])
	}
	if len(preview) > 300 {
		t.Errorf("content_preview length = %d, want <= 300", len(preview))
	}
}

// TestIonHistory_EnvelopeFieldsPresent verifies the standard envelope fields and
// the observation sub-object are present on a successful call.
func TestIonHistory_EnvelopeFieldsPresent(t *testing.T) {
	st := mustStore(t)
	sid := mustSeedSession(t, st, "ion-mem")
	obs := seedTopicObs(t, st, sid, "history/envelope", 2)

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_history", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)

	for _, key := range []string{"project", "project_source", "project_path", "result", "status"} {
		if _, ok := env[key]; !ok {
			t.Errorf("missing envelope field: %q", key)
		}
	}

	obsObj, ok := env["observation"].(map[string]any)
	if !ok {
		t.Fatalf("observation must be object, got %T", env["observation"])
	}
	for _, key := range []string{"id", "title", "type", "revision_count", "updated_at"} {
		if _, ok := obsObj[key]; !ok {
			t.Errorf("observation missing field: %q", key)
		}
	}
}
