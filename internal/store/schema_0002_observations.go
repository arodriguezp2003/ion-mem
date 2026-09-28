package store

import "database/sql"

const ddlObservations = `
CREATE TABLE IF NOT EXISTS observations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id         TEXT    NOT NULL UNIQUE,
    session_id      TEXT    NOT NULL REFERENCES sessions(id),
    type            TEXT    NOT NULL,
    title           TEXT    NOT NULL,
    content         TEXT    NOT NULL,
    tool_name       TEXT,
    project         TEXT    NOT NULL,
    scope           TEXT    NOT NULL DEFAULT 'project',
    topic_key       TEXT,
    normalized_hash TEXT    NOT NULL,
    revision_count  INTEGER NOT NULL DEFAULT 1,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    last_seen_at    TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    deleted_at      TEXT
);

CREATE INDEX IF NOT EXISTS idx_obs_session ON observations(session_id);
CREATE INDEX IF NOT EXISTS idx_obs_type    ON observations(type);
CREATE INDEX IF NOT EXISTS idx_obs_project ON observations(project);
CREATE INDEX IF NOT EXISTS idx_obs_scope   ON observations(scope);
CREATE INDEX IF NOT EXISTS idx_obs_created ON observations(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_obs_deleted ON observations(deleted_at);
CREATE INDEX IF NOT EXISTS idx_obs_topic   ON observations(topic_key, project, scope, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_obs_dedupe  ON observations(normalized_hash, project, scope, type, title, created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
    title, content, tool_name, type, project, topic_key,
    content='observations',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS obs_fts_insert AFTER INSERT ON observations BEGIN
    INSERT INTO observations_fts(rowid, title, content, tool_name, type, project, topic_key)
    VALUES (new.id, new.title, new.content, new.tool_name, new.type, new.project, new.topic_key);
END;

CREATE TRIGGER IF NOT EXISTS obs_fts_delete AFTER DELETE ON observations BEGIN
    INSERT INTO observations_fts(observations_fts, rowid, title, content, tool_name, type, project, topic_key)
    VALUES ('delete', old.id, old.title, old.content, old.tool_name, old.type, old.project, old.topic_key);
END;

CREATE TRIGGER IF NOT EXISTS obs_fts_update AFTER UPDATE ON observations BEGIN
    INSERT INTO observations_fts(observations_fts, rowid, title, content, tool_name, type, project, topic_key)
    VALUES ('delete', old.id, old.title, old.content, old.tool_name, old.type, old.project, old.topic_key);
    INSERT INTO observations_fts(rowid, title, content, tool_name, type, project, topic_key)
    VALUES (new.id, new.title, new.content, new.tool_name, new.type, new.project, new.topic_key);
END;
`

// applyMigration0002 creates the observations table, indexes, FTS5 virtual
// table, and sync triggers.
func applyMigration0002(db *sql.DB) error {
	_, err := db.Exec(ddlObservations)
	return err
}

func init() {
	registerMigration(2, applyMigration0002)
}
