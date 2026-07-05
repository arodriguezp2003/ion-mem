package store_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestMigration0007_CreatesRevisionTable verifies that migration 0007 creates the
// observation_revisions table and the required index.
func TestMigration0007_CreatesRevisionTable(t *testing.T) {
	s := mustOpen(t)

	v := s.SchemaVersion()
	if v < 7 {
		t.Fatalf("expected SchemaVersion >= 7, got %d", v)
	}

	tables := sqliteMasterNames(t, s, "table")
	if !contains(tables, "observation_revisions") {
		t.Fatalf("expected observation_revisions table in sqlite_master, got: %v", tables)
	}

	indexes := sqliteMasterNames(t, s, "index")
	if !contains(indexes, "idx_obs_revisions_obs") {
		t.Fatalf("expected idx_obs_revisions_obs index in sqlite_master, got: %v", indexes)
	}
}

// TestMigration0007_Idempotent verifies that opening the same dataDir twice does not
// duplicate the migration record or fail.
func TestMigration0007_Idempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	v1 := s1.SchemaVersion()
	_ = s1.Close()

	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	v2 := s2.SchemaVersion()
	if v2 != v1 {
		t.Fatalf("SchemaVersion mismatch after idempotent open: first=%d second=%d", v1, v2)
	}
}
