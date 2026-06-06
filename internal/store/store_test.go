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
// produce duplicate schema_version rows.
func TestOpen_IsIdempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	v := s2.SchemaVersion()
	if v != 1 {
		t.Fatalf("expected exactly SchemaVersion=1 after idempotent open, got %d", v)
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

// contains reports whether slice contains s.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
