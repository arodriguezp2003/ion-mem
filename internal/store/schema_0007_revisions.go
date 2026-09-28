package store

import "database/sql"

const ddlRevisions = `
CREATE TABLE observation_revisions (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  observation_id  INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
  revision        INTEGER NOT NULL,
  type            TEXT NOT NULL,
  title           TEXT NOT NULL,
  content         TEXT NOT NULL,
  tool_name       TEXT,
  created_at      TEXT NOT NULL,
  archived_at     TEXT NOT NULL
);
CREATE INDEX idx_obs_revisions_obs ON observation_revisions(observation_id, revision);`

// applyMigration0007 creates the observation_revisions table for storing historical
// snapshots of observations captured before destructive overwrites.
func applyMigration0007(db *sql.DB) error {
	_, err := db.Exec(ddlRevisions)
	return err
}

func init() {
	registerMigration(7, applyMigration0007)
}
