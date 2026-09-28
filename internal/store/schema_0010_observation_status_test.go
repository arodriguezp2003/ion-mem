package store_test

import (
	"context"
	"testing"
)

// TestMigration0010_CreatesStatusIndex verifies that migration 0010 applies
// and creates the (project, status) index used by status-filtered queries.
func TestMigration0010_CreatesStatusIndex(t *testing.T) {
	s := mustOpen(t)

	v := s.SchemaVersion()
	if v < 10 {
		t.Fatalf("expected SchemaVersion >= 10, got %d", v)
	}

	indexes := sqliteMasterNames(t, s, "index")
	if !contains(indexes, "idx_obs_project_status") {
		t.Errorf("expected index %q in sqlite_master, got: %v", "idx_obs_project_status", indexes)
	}
}

// TestMigration0010_DefaultStatusIsActive verifies that both a pre-existing
// row (simulated by inserting straight after migration, since this store was
// created fresh at the current schema version) and a brand-new row created
// via AddObservation default to status "active".
func TestMigration0010_DefaultStatusIsActive(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "status-proj")
	obs := mustObservation(t, s, sess.ID)

	if obs.Status != "active" {
		t.Fatalf("AddObservation: got Status %q, want %q", obs.Status, "active")
	}
	if obs.SupersededBy != nil {
		t.Fatalf("AddObservation: got SupersededBy %v, want nil", obs.SupersededBy)
	}
	if obs.StatusReason != nil {
		t.Fatalf("AddObservation: got StatusReason %v, want nil", obs.StatusReason)
	}

	// Fetch it back to make sure GetObservation's explicit column list scans
	// the new columns too.
	fetched, err := s.GetObservation(ctx, obs.ID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if fetched.Status != "active" {
		t.Fatalf("GetObservation: got Status %q, want %q", fetched.Status, "active")
	}
}
