package store

import "database/sql"

const ddlSettings = `
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

// applyMigration0005 creates the settings table for persistent key/value
// configuration (e.g. embeddings enabled, Ollama URL, model name).
func applyMigration0005(db *sql.DB) error {
	_, err := db.Exec(ddlSettings)
	return err
}

func init() {
	registerMigration(5, applyMigration0005)
}
