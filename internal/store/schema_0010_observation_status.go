package store

import "database/sql"

const ddlObservationStatus = `
-- status tracks observation lifecycle: 'active' (default), 'superseded'
-- (replaced by a newer observation but kept as history), or 'obsolete'
-- (no longer relevant, but never hard-deleted for permanent types — see
-- Permanent and PruneDeletedObs).
ALTER TABLE observations ADD COLUMN status TEXT NOT NULL DEFAULT 'active';

-- superseded_by points at the observation id that replaced this one. Only
-- meaningful when status='superseded'. SQLite's ALTER TABLE cannot add a
-- FOREIGN KEY constraint to an existing table, so referential integrity for
-- this column is enforced in Go (see SetObservationStatus).
ALTER TABLE observations ADD COLUMN superseded_by INTEGER;

-- status_reason is a short human-readable note explaining the status change
-- (e.g. "replaced by newer approach after perf regression").
ALTER TABLE observations ADD COLUMN status_reason TEXT;

-- status_changed_at records when status last changed (RFC3339Nano, Go-side).
ALTER TABLE observations ADD COLUMN status_changed_at TEXT;

-- Search and curation filter by (project, status) together; index it so
-- ListObservationsByStatus and status-filtered search avoid a full scan.
CREATE INDEX IF NOT EXISTS idx_obs_project_status ON observations(project, status);
`

// applyMigration0010 adds observation status/supersession columns
// (status, superseded_by, status_reason, status_changed_at) and a
// (project, status) index. Existing rows default to status='active'.
func applyMigration0010(db *sql.DB) error {
	_, err := db.Exec(ddlObservationStatus)
	return err
}

func init() {
	registerMigration(10, applyMigration0010)
}
