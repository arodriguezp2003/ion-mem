package store

import "database/sql"

const ddlSessions = `
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    project    TEXT NOT NULL,
    directory  TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at   TEXT,
    summary    TEXT,
    status     TEXT NOT NULL DEFAULT 'active'
);

CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC);
`

// applyMigration0001 creates the sessions table and its indexes.
func applyMigration0001(db *sql.DB) error {
	_, err := db.Exec(ddlSessions)
	return err
}

func init() {
	registerMigration(1, applyMigration0001)
}
