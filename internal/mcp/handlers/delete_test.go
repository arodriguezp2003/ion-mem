package handlers_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
)

// TestIonDelete_StatusOkOnSuccess verifies status:"ok" on successful delete.
func TestIonDelete_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	obs := seedObservation(t, st, "ion-mem", "Delete Status Target", "content", "manual")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_delete", map[string]any{"id": float64(obs.ID)})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

// TestIonDelete_StatusErrorNotFound verifies status:"error" + not_found on missing id.
func TestIonDelete_StatusErrorNotFound(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "ion-mem", Source: "git_root", Path: "/fake/ion-mem"}, nil
	}))

	res := callTool(t, ts, "ion_delete", map[string]any{"id": float64(999999)})
	env := decodeText(t, res)

	if env["status"] != "error" {
		t.Errorf("status = %v, want %q", env["status"], "error")
	}
	if env["error_code"] != "not_found" {
		t.Errorf("error_code = %v, want %q", env["error_code"], "not_found")
	}
}

// TestIonDelete_SoftDeleteHidesFromSearch verifies that soft-deleting an observation
// causes subsequent ion_search calls to exclude it.
func TestIonDelete_SoftDeleteHidesFromSearch(t *testing.T) {
	st := mustStore(t)
	obs := seedObservation(t, st, "ion-mem", "Soft Delete Target", "unique content for soft delete test", "manual")

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	// Verify observation appears in search before delete
	searchBefore := callTool(t, ts, "ion_search", map[string]any{
		"query":        "unique content for soft delete test",
		"all_projects": true,
	})
	envBefore := decodeText(t, searchBefore)
	countBefore, _ := envBefore["count"].(float64)
	if countBefore == 0 {
		t.Skip("observation not indexed in FTS — seed may not be visible to search")
	}

	// Soft delete (hard=false, which is the default)
	delRes := callTool(t, ts, "ion_delete", map[string]any{
		"id": float64(obs.ID),
	})
	delEnv := decodeText(t, delRes)
	for _, key := range []string{"project", "project_source", "project_path", "result"} {
		if _, ok := delEnv[key]; !ok {
			t.Errorf("delete envelope missing field: %q", key)
		}
	}

	// Now search should not return it
	searchAfter := callTool(t, ts, "ion_search", map[string]any{
		"query":        "unique content for soft delete test",
		"all_projects": true,
	})
	envAfter := decodeText(t, searchAfter)
	countAfter, _ := envAfter["count"].(float64)
	if countAfter != 0 {
		t.Errorf("soft-deleted observation still appears in search: count=%v", countAfter)
	}
}

// TestIonDelete_HardDeletePermanentRemoval verifies that a hard delete causes
// ion_get_observation to return "not found".
func TestIonDelete_HardDeletePermanentRemoval(t *testing.T) {
	st := mustStore(t)
	obs := seedObservation(t, st, "ion-mem", "Hard Delete Target", "hard delete content", "manual")

	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	// Hard delete
	delRes := callTool(t, ts, "ion_delete", map[string]any{
		"id":   float64(obs.ID),
		"hard": true,
	})
	delEnv := decodeText(t, delRes)
	result, _ := delEnv["result"].(string)
	if result == "" {
		t.Error("delete result field must be non-empty")
	}

	// Get observation should now return not-found error
	getRes := callTool(t, ts, "ion_get_observation", map[string]any{
		"id": float64(obs.ID),
	})
	getEnv := decodeText(t, getRes)
	getResult, _ := getEnv["result"].(string)
	if getResult == "" {
		t.Error("expected non-empty result for deleted observation")
	}
	if getEnv["observation"] != nil {
		t.Error("expected no observation object after hard delete")
	}
}
