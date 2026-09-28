package store

import "database/sql"

const ddlPromptConsumedAt = `ALTER TABLE user_prompts ADD COLUMN consumed_at TEXT DEFAULT NULL;`

// applyMigration0004 adds the consumed_at column to user_prompts.
// Historical rows receive NULL automatically (DEFAULT NULL), which means they
// are unconsumed — no backfill is required.
func applyMigration0004(db *sql.DB) error {
	_, err := db.Exec(ddlPromptConsumedAt)
	return err
}

func init() {
	registerMigration(4, applyMigration0004)
}
