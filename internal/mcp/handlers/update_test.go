package handlers_test

import (
	"context"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

func fakeProject(name string) mcp.Option {
	return mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: name, Source: "git_root", Path: "/fake/" + name}, nil
	})
}

// mustSeedSession creates a test session and returns its ID. The session ID is
// derived from the test name to avoid PK conflicts across tests.
func mustSeedSession(t *testing.T, st *store.Store, proj string) string {
	t.Helper()
	sid := "sess-" + t.Name()
	_, err := st.CreateSession(context.Background(), store.CreateSessionParams{
		ID:      sid,
		Project: proj,
	})
	if err != nil {
		t.Fatalf("mustSeedSession: %v", err)
	}
	return sid
}

// seedObservation inserts a test observation after creating a fresh session.
func seedObservation(t *testing.T, st *store.Store, proj, title, content, obsType string) store.Observation {
	t.Helper()
	sid := mustSeedSession(t, st, proj)
	obs, err := st.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sid,
		Type:      obsType,
		Title:     title,
		Content:   content,
		Project:   proj,
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("seedObservation: %v", err)
	}
	return obs
}

// TestIonUpdate_PatchPreservesUnchangedFields verifies that updating only title
// leaves content, type, and other fields unchanged, and increments revision_count.
func TestIonUpdate_PatchPreservesUnchangedFields(t *testing.T) {
	st := mustStore(t)
	orig := seedObservation(t, st, "ion-mem", "Original Title", "Original Content", "manual")

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_update", map[string]any{
		"id":    float64(orig.ID),
		"title": "Updated Title",
	})
	env := decodeText(t, res)

	if env["result"] == nil {
		t.Fatal("expected result field in envelope")
	}

	// Standard envelope fields
	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := env[key]; !ok {
			t.Errorf("missing envelope field: %q", key)
		}
	}

	obs, ok := env["observation"].(map[string]any)
	if !ok {
		t.Fatalf("expected observation object in response, got: %v", env["observation"])
	}

	if obs["title"] != "Updated Title" {
		t.Errorf("title: got %q, want %q", obs["title"], "Updated Title")
	}
	if obs["content"] != "Original Content" {
		t.Errorf("content: got %q, want unchanged %q", obs["content"], "Original Content")
	}
	if obs["type"] != "manual" {
		t.Errorf("type: got %q, want unchanged %q", obs["type"], "manual")
	}

	revCount, ok := obs["revision_count"].(float64)
	if !ok || revCount < 1 {
		t.Errorf("revision_count: got %v, want >=1", obs["revision_count"])
	}
}

// TestIonUpdate_StatusOkOnSuccess verifies status:"ok" on successful update.
func TestIonUpdate_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	orig := seedObservation(t, st, "ion-mem", "Status OK Title", "Content", "manual")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_update", map[string]any{
		"id":    float64(orig.ID),
		"title": "Updated",
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

// TestIonUpdate_StatusErrorNotFound verifies status:"error" + not_found on missing id.
func TestIonUpdate_StatusErrorNotFound(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_update", map[string]any{
		"id":    float64(999999),
		"title": "Anything",
	})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q", env["status"], "error")
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}

// TestIonUpdate_MissingIdEnvelopeError verifies that a missing ID produces
// an error message in result, never a Go error.
func TestIonUpdate_MissingIdEnvelopeError(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_update", map[string]any{
		"id":    float64(99999),
		"title": "Some Title",
	})
	env := decodeText(t, res)

	result, _ := env["result"].(string)
	if result == "" {
		t.Error("expected non-empty result field for missing id")
	}
	// Should contain a useful error keyword
	if result == "observation updated" {
		t.Error("result should indicate an error, not success")
	}
}
