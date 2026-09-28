package store

import "database/sql"

const ddlPrompts = `
CREATE TABLE IF NOT EXISTS user_prompts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id    TEXT    NOT NULL UNIQUE,
    session_id TEXT    NOT NULL REFERENCES sessions(id),
    content    TEXT    NOT NULL,
    project    TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prompts_session ON user_prompts(session_id);
CREATE INDEX IF NOT EXISTS idx_prompts_project ON user_prompts(project);
CREATE INDEX IF NOT EXISTS idx_prompts_created ON user_prompts(created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS prompts_fts USING fts5(
    content, project,
    content='user_prompts',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS prompts_fts_insert AFTER INSERT ON user_prompts BEGIN
    INSERT INTO prompts_fts(rowid, content, project)
    VALUES (new.id, new.content, new.project);
END;

CREATE TRIGGER IF NOT EXISTS prompts_fts_delete AFTER DELETE ON user_prompts BEGIN
    INSERT INTO prompts_fts(prompts_fts, rowid, content, project)
    VALUES ('delete', old.id, old.content, old.project);
END;

CREATE TRIGGER IF NOT EXISTS prompts_fts_update AFTER UPDATE ON user_prompts BEGIN
    INSERT INTO prompts_fts(prompts_fts, rowid, content, project)
    VALUES ('delete', old.id, old.content, old.project);
    INSERT INTO prompts_fts(rowid, content, project)
    VALUES (new.id, new.content, new.project);
END;
`

// applyMigration0003 creates the user_prompts table, indexes, FTS5 virtual
// table, and sync triggers.
func applyMigration0003(db *sql.DB) error {
	_, err := db.Exec(ddlPrompts)
	return err
}

func init() {
	registerMigration(3, applyMigration0003)
}
