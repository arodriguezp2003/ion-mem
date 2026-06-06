package store

import (
	"database/sql"
	"fmt"
)

// migrationFn is a function that applies one schema migration.
type migrationFn struct {
	version int
	apply   func(db *sql.DB) error
}

// registeredMigrations holds the ordered list of migration functions.
// Each slice registers its migration here via an init-style append in its
// schema_NNNN_*.go file.
var registeredMigrations []migrationFn

// registerMigration adds a migration to the ordered list.
// Migrations must be registered in ascending version order.
func registerMigration(version int, fn func(db *sql.DB) error) {
	registeredMigrations = append(registeredMigrations, migrationFn{version: version, apply: fn})
}

const createSchemaVersionSQL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);`

// migrate creates the schema_version table (if absent) and runs every
// registered migration whose version is not yet recorded. It is idempotent:
// re-running on an up-to-date database is a no-op.
func migrate(db *sql.DB) error {
	// Ensure schema_version table exists.
	if _, err := db.Exec(createSchemaVersionSQL); err != nil {
		return fmt.Errorf("%w: creating schema_version table: %v", ErrMigrationFailed, err)
	}

	// Load applied versions.
	rows, err := db.Query("SELECT version FROM schema_version")
	if err != nil {
		return fmt.Errorf("%w: reading schema_version: %v", ErrMigrationFailed, err)
	}
	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("%w: scanning schema_version: %v", ErrMigrationFailed, err)
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%w: closing schema_version rows: %v", ErrMigrationFailed, err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: iterating schema_version: %v", ErrMigrationFailed, err)
	}

	// Apply any missing migrations in order.
	for _, m := range registeredMigrations {
		if applied[m.version] {
			continue
		}
		if err := m.apply(db); err != nil {
			return fmt.Errorf("%w: applying migration v%d: %v", ErrMigrationFailed, m.version, err)
		}
		if _, err := db.Exec(
			"INSERT OR IGNORE INTO schema_version (version) VALUES (?)", m.version,
		); err != nil {
			return fmt.Errorf("%w: recording migration v%d: %v", ErrMigrationFailed, m.version, err)
		}
	}
	return nil
}
