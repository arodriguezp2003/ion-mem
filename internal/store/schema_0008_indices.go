package store

import "database/sql"

const ddlHotPathIndices = `
-- Model is filtered on every hybrid VectorSearch / MissingEmbeddings /
-- EmbeddingCoverage call. A plain index on the column avoids full-table scans.
CREATE INDEX IF NOT EXISTS idx_oe_model ON observation_embeddings(model);

-- Partial index on (session_id, created_at DESC) WHERE consumed_at IS NULL
-- serves ConsumeLatestPrompt on every ion_save. The WHERE clause keeps it
-- small: only unconsumed rows are indexed, matching the query predicate.
CREATE INDEX IF NOT EXISTS idx_prompts_unconsumed
    ON user_prompts(session_id, created_at DESC)
    WHERE consumed_at IS NULL;`

// applyMigration0008 adds two hot-path indices:
//   - idx_oe_model: speeds up model-filtered queries on observation_embeddings
//     (VectorSearch, MissingEmbeddings, EmbeddingCoverage).
//   - idx_prompts_unconsumed: partial index that accelerates ConsumeLatestPrompt
//     by indexing only the unconsumed subset of user_prompts.
func applyMigration0008(db *sql.DB) error {
	_, err := db.Exec(ddlHotPathIndices)
	return err
}

func init() {
	registerMigration(8, applyMigration0008)
}
