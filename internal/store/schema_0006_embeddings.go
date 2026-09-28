package store

import "database/sql"

const ddlEmbeddings = `
CREATE TABLE observation_embeddings (
    observation_id INTEGER PRIMARY KEY REFERENCES observations(id) ON DELETE CASCADE,
    model          TEXT NOT NULL,
    dims           INTEGER NOT NULL,
    vector         BLOB NOT NULL,
    updated_at     TEXT NOT NULL
);`

// applyMigration0006 creates the observation_embeddings table for storing dense
// vector representations of observations produced by a local Ollama model.
func applyMigration0006(db *sql.DB) error {
	_, err := db.Exec(ddlEmbeddings)
	return err
}

func init() {
	registerMigration(6, applyMigration0006)
}
