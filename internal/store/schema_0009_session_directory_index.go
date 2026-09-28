package store

import "database/sql"

const ddlSessionDirectoryIndex = `
-- Index on sessions(directory) so ProjectForDirectory can resolve the most
-- recent project for a given directory without a full table scan.
CREATE INDEX IF NOT EXISTS idx_sessions_directory ON sessions(directory);`

// applyMigration0009 adds an index on sessions.directory to support
// path-aware project identity lookups (ProjectForDirectory).
func applyMigration0009(db *sql.DB) error {
	_, err := db.Exec(ddlSessionDirectoryIndex)
	return err
}

func init() {
	registerMigration(9, applyMigration0009)
}
