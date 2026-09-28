package store

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigrateTo_PreExistingRowUpgradesToActiveStatus is a migration realism
// test for schema_0010_observation_status: it builds a fixture at schema
// v9 (before the status/superseded_by/status_reason/status_changed_at
// columns existed), inserts an observation row shaped like the pre-v10
// schema, then runs the real migrate() upgrade path (via Open, which calls
// migrate = migrateTo(db, math.MaxInt)) and asserts the pre-existing row
// reads back with status "active" and a nil superseded_by — i.e. migration
// 0010's ALTER TABLE ... DEFAULT 'active' actually backfills real rows, not
// just rows inserted after the migration already ran.
func TestMigrateTo_PreExistingRowUpgradesToActiveStatus(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ion-mem.db")

	// Build the v9 fixture directly against the raw DB, bypassing Open (which
	// would run every migration, including 0010).
	raw, err := OpenRaw(dbPath)
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	if err := migrateTo(raw.DB(), 9); err != nil {
		t.Fatalf("migrateTo(9): %v", err)
	}
	if v := raw.SchemaVersion(); v != 9 {
		t.Fatalf("fixture SchemaVersion = %d, want 9", v)
	}

	now := nowISO()
	if _, err := raw.DB().Exec(`
		INSERT INTO sessions (id, project, directory, started_at, status)
		VALUES ('fixture-sess', 'fixture-proj', '/tmp/fixture', ?, 'active')`,
		now,
	); err != nil {
		t.Fatalf("insert v9 session fixture: %v", err)
	}
	if _, err := raw.DB().Exec(`
		INSERT INTO observations
		    (sync_id, session_id, type, title, content, project, scope,
		     normalized_hash, revision_count, duplicate_count,
		     last_seen_at, created_at, updated_at)
		VALUES ('obs-fixture-v9', 'fixture-sess', 'decision', 'pre-migration title',
		        'pre-migration content', 'fixture-proj', 'project',
		        'fixturehash', 1, 0, ?, ?, ?)`,
		now, now, now,
	); err != nil {
		t.Fatalf("insert v9 observation fixture: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close fixture store: %v", err)
	}

	// Now open normally: this runs migrate() (= migrateTo(db, math.MaxInt)),
	// applying migration 0010 (and anything after it) on top of the v9
	// fixture data built above.
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open after fixture: %v", err)
	}
	defer s.Close()

	var id int64
	if err := s.db.QueryRow(
		"SELECT id FROM observations WHERE sync_id='obs-fixture-v9'",
	).Scan(&id); err != nil {
		t.Fatalf("find fixture observation after upgrade: %v", err)
	}

	obs, err := s.GetObservation(context.Background(), id)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if obs.Status != StatusActive {
		t.Errorf("pre-existing row Status = %q, want %q (migration 0010's DEFAULT 'active' backfill)", obs.Status, StatusActive)
	}
	if obs.SupersededBy != nil {
		t.Errorf("pre-existing row SupersededBy = %v, want nil", obs.SupersededBy)
	}
	if obs.StatusReason != nil {
		t.Errorf("pre-existing row StatusReason = %v, want nil", obs.StatusReason)
	}
	if obs.Title != "pre-migration title" {
		t.Errorf("pre-existing row Title = %q, want %q (upgrade must not touch unrelated columns)", obs.Title, "pre-migration title")
	}
}
