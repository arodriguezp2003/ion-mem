package store_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestOpen_CreatesSchemaVersionRow verifies that Open creates the schema_version
// table and records at least one row (version=1 for migration 0001).
func TestOpen_CreatesSchemaVersionRow(t *testing.T) {
	s := mustOpen(t)
	v := s.SchemaVersion()
	if v < 1 {
		t.Fatalf("expected SchemaVersion >= 1, got %d", v)
	}
}

// TestOpen_IsIdempotent verifies that opening the same dataDir twice does not
// produce duplicate schema_version rows (INSERT OR IGNORE is idempotent).
func TestOpen_IsIdempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	vFirst := s1.SchemaVersion()
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	vSecond := s2.SchemaVersion()
	if vSecond != vFirst {
		t.Fatalf("expected same SchemaVersion after idempotent open: first=%d second=%d", vFirst, vSecond)
	}

	// Verify no duplicate rows: count of rows must equal max version.
	var rowCount int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&rowCount); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if rowCount != vSecond {
		t.Fatalf("expected %d rows in schema_version (one per migration), got %d", vSecond, rowCount)
	}
}

// TestOpen_RejectsRegularFilePath verifies that Open returns an error when
// dataDir points to an existing regular file.
func TestOpen_RejectsRegularFilePath(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file at the path we'll try to open.
	filePath := dir + "/not-a-dir"
	if err := createFile(t, filePath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	s, err := store.Open(filePath)
	if err == nil {
		s.Close()
		t.Fatal("expected non-nil error when dataDir is a regular file, got nil")
	}
}

// TestOpen_RejectsRelativePath verifies that Open returns an error when given
// a relative path.
func TestOpen_RejectsRelativePath(t *testing.T) {
	s, err := store.Open("./data")
	if err == nil {
		s.Close()
		t.Fatal("expected non-nil error for relative path, got nil")
	}
}

// TestOpen_WALEnabled verifies that the WAL journal mode is active after Open.
func TestOpen_WALEnabled(t *testing.T) {
	s := mustOpen(t)
	mode := queryPragmaString(t, s, "journal_mode")
	if mode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", mode)
	}
}

// TestOpen_BusyTimeout verifies that busy_timeout is set to 5000 ms.
func TestOpen_BusyTimeout(t *testing.T) {
	s := mustOpen(t)
	v := queryPragmaInt(t, s, "busy_timeout")
	if v != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", v)
	}
}

// TestOpen_ForeignKeysOn verifies that foreign_keys pragma is ON (1).
func TestOpen_ForeignKeysOn(t *testing.T) {
	s := mustOpen(t)
	v := queryPragmaInt(t, s, "foreign_keys")
	if v != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", v)
	}
}

// TestMigration0001_CreatesSessionsTable verifies that migration 0001 creates
// the sessions table and both required indexes.
func TestMigration0001_CreatesSessionsTable(t *testing.T) {
	s := mustOpen(t)

	tables := sqliteMasterNames(t, s, "table")
	if !contains(tables, "sessions") {
		t.Fatalf("expected sessions table in sqlite_master, got: %v", tables)
	}

	indexes := sqliteMasterNames(t, s, "index")
	for _, want := range []string{"idx_sessions_project", "idx_sessions_started"} {
		if !contains(indexes, want) {
			t.Fatalf("expected index %q in sqlite_master, got: %v", want, indexes)
		}
	}
}

// TestMigration0002_AppliesOnTopOf0001 verifies that migration 0002 applies
// cleanly on top of migration 0001, creating the observations table, FTS5
// virtual table, and recording version=2 in schema_version.
func TestMigration0002_AppliesOnTopOf0001(t *testing.T) {
	s := mustOpen(t)

	// schema_version must have rows for version 1 and 2.
	v := s.SchemaVersion()
	if v < 2 {
		t.Fatalf("expected SchemaVersion >= 2, got %d", v)
	}

	// Verify both versions are recorded.
	rows, err := s.DB().Query("SELECT version FROM schema_version ORDER BY version")
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var ver int
		if err := rows.Scan(&ver); err != nil {
			t.Fatalf("scan schema_version: %v", err)
		}
		versions = append(versions, ver)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("schema_version rows.Err: %v", err)
	}
	if !containsInt(versions, 1) || !containsInt(versions, 2) {
		t.Fatalf("expected versions [1 2] in schema_version, got: %v", versions)
	}

	// Verify observations table exists.
	tables := sqliteMasterNames(t, s, "table")
	if !contains(tables, "observations") {
		t.Fatalf("expected observations table in sqlite_master, got: %v", tables)
	}

	// Verify observations_fts virtual table exists.
	if !contains(tables, "observations_fts") {
		t.Fatalf("expected observations_fts in sqlite_master tables, got: %v", tables)
	}

	// Verify all 8 indexes exist.
	indexes := sqliteMasterNames(t, s, "index")
	wantIndexes := []string{
		"idx_obs_session", "idx_obs_type", "idx_obs_project", "idx_obs_scope",
		"idx_obs_created", "idx_obs_deleted", "idx_obs_topic", "idx_obs_dedupe",
	}
	for _, want := range wantIndexes {
		if !contains(indexes, want) {
			t.Errorf("expected index %q in sqlite_master, got: %v", want, indexes)
		}
	}
}

// contains reports whether slice contains s.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// containsInt reports whether slice contains n.
func containsInt(slice []int, n int) bool {
	for _, v := range slice {
		if v == n {
			return true
		}
	}
	return false
}
