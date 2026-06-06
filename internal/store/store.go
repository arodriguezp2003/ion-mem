package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// Store is the local persistence engine backed by a single SQLite database
// file. It is safe for concurrent use from a single process (WAL mode,
// one connection).
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the ion-mem database at <dataDir>/ion-mem.db.
//
// dataDir must be an absolute path. Open creates the directory (and any
// missing parents) if it does not exist. It returns an error if dataDir is
// a relative path or if the path exists as a regular file.
//
// Open applies all pending schema migrations before returning.
func Open(dataDir string) (*Store, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, fmt.Errorf("store.Open: dataDir must be absolute, got %q", dataDir)
	}

	info, err := os.Stat(dataDir)
	if err == nil && !info.IsDir() {
		return nil, fmt.Errorf("store.Open: dataDir %q exists but is not a directory", dataDir)
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("store.Open: creating dataDir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "ion-mem.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store.Open: sql.Open: %w", err)
	}
	db.SetMaxOpenConns(1)

	// Apply pragmas in the required order before migrations.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("store.Open: %s: %w", p, err)
		}
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection and releases the file lock.
// Calling Close on an already-closed Store is safe; the second call may return
// an error from the driver.
func (s *Store) Close() error {
	return s.db.Close()
}

// SchemaVersion returns the highest migration version that has been applied,
// or 0 if no migrations have been applied yet.
func (s *Store) SchemaVersion() int {
	var v int
	err := s.db.QueryRow(
		"SELECT COALESCE(MAX(version),0) FROM schema_version",
	).Scan(&v)
	if err != nil {
		return 0
	}
	return v
}

// DB returns the underlying *sql.DB for use in tests.
// This method exposes internal state and should not be called from production code.
func (s *Store) DB() *sql.DB {
	return s.db
}
