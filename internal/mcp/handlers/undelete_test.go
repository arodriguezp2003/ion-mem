package handlers_test

import (
	"testing"
)

// TestIonUndelete_StatusOkOnSuccess verifies that ion_undelete returns status:"ok"
// when an observation is soft-deleted and then restored.
func TestIonUndelete_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	obs := seedObservation(t, st, "ion-mem", "Undelete Target", "content to restore", "manual")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	// Soft delete first.
	callTool(t, ts, "ion_delete", map[string]any{"id": float64(obs.ID)})

	// Now undelete.
	res := callTool(t, ts, "ion_undelete", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

// TestIonUndelete_NotFoundForMissingID verifies that ion_undelete returns
// status:"error" + error_code:"not_found" for a non-existent observation.
func TestIonUndelete_NotFoundForMissingID(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_undelete", map[string]any{"id": float64(999999)})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q", env["status"], "error")
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}

// TestIonUndelete_NotFoundForNonDeletedObservation verifies that calling ion_undelete
// on a non-deleted observation returns not_found (it is not soft-deleted).
func TestIonUndelete_NotFoundForNonDeletedObservation(t *testing.T) {
	st := mustStore(t)
	obs := seedObservation(t, st, "ion-mem", "Non-deleted Target", "active content", "manual")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_undelete", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q", env["status"], "error")
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}

// TestIonUndelete_SoftDeletedBecomesSearchableAgain verifies the full cycle:
// add → soft-delete (hidden) → undelete → visible again via ion_get_observation.
func TestIonUndelete_SoftDeletedBecomesSearchableAgain(t *testing.T) {
	st := mustStore(t)
	obs := seedObservation(t, st, "ion-mem", "Restore Cycle", "content for restore cycle", "manual")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	// Soft delete.
	callTool(t, ts, "ion_delete", map[string]any{"id": float64(obs.ID)})

	// Undelete.
	res := callTool(t, ts, "ion_undelete", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("undelete status = %v, want ok", env["status"])
	}

	// ion_get_observation must succeed now.
	getRes := callTool(t, ts, "ion_get_observation", map[string]any{"id": float64(obs.ID)})
	getEnv := decodeText(t, getRes)
	if getEnv["observation"] == nil {
		t.Error("expected observation object after undelete, got nil")
	}
}
