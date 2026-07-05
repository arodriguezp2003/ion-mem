// Package store — lifecycle operations: backup, export, prune, rename-project.
//
// These methods live in a dedicated file to keep observations.go untouched.
// They are the only store file an agent is permitted to write/modify as part of
// the data-lifecycle CLI suite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ─── Backup ───────────────────────────────────────────────────────────────────

// Backup writes a compact, consistent copy of the database to destPath using
// SQLite's VACUUM INTO statement. The destination must not already exist; Backup
// returns an error rather than overwriting an existing file.
//
// After a successful backup, the setting "backup.last_at" is updated to the
// current UTC time in RFC3339 format.
func (s *Store) Backup(ctx context.Context, destPath string) error {
	// Refuse to overwrite an existing file.
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("store.Backup: destination %q already exists", destPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store.Backup: stat destination: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("store.Backup: VACUUM INTO: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.SetSetting(ctx, "backup.last_at", now); err != nil {
		return fmt.Errorf("store.Backup: record last_at: %w", err)
	}
	return nil
}

// OpenRaw opens a SQLite database at the given absolute file path without
// running migrations. Used by tests to validate backup files.
func OpenRaw(dbPath string) (*Store, error) {
	db, err := newSQLiteConn(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store.OpenRaw: %w", err)
	}
	return &Store{db: db}, nil
}

// newSQLiteConn opens a raw *sql.DB at path with the standard ion-mem pragmas
// (WAL, foreign keys, busy timeout). Callers are responsible for Close.
func newSQLiteConn(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	return db, nil
}

// ─── Export ───────────────────────────────────────────────────────────────────

// ExportManifest describes the export run written to manifest.json.
type ExportManifest struct {
	SchemaVersion int            `json:"schema_version"`
	ExportedAt    string         `json:"exported_at"`
	Counts        map[string]int `json:"counts"`
	Note          string         `json:"note"`
}

// Export writes JSONL dumps of all tables (except embeddings) into outDir and
// creates a manifest.json describing the export. Returns the manifest so callers
// can inspect counts without parsing files.
func (s *Store) Export(ctx context.Context, outDir string) (ExportManifest, error) {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return ExportManifest{}, fmt.Errorf("store.Export: mkdir: %w", err)
	}

	counts := make(map[string]int)

	// observations (ALL, including soft-deleted)
	obsCount, err := s.exportObservations(ctx, outDir)
	if err != nil {
		return ExportManifest{}, err
	}
	counts["observations"] = obsCount

	// prompts
	promptCount, err := s.exportPrompts(ctx, outDir)
	if err != nil {
		return ExportManifest{}, err
	}
	counts["prompts"] = promptCount

	// sessions
	sessCount, err := s.exportSessions(ctx, outDir)
	if err != nil {
		return ExportManifest{}, err
	}
	counts["sessions"] = sessCount

	// revisions
	revCount, err := s.exportRevisions(ctx, outDir)
	if err != nil {
		return ExportManifest{}, err
	}
	counts["revisions"] = revCount

	// settings
	settCount, err := s.exportSettings(ctx, outDir)
	if err != nil {
		return ExportManifest{}, err
	}
	counts["settings"] = settCount

	manifest := ExportManifest{
		SchemaVersion: 8,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Counts:        counts,
		Note:          "observation_embeddings not exported (regenerable from observations)",
	}

	mPath := filepath.Join(outDir, "manifest.json")
	mData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ExportManifest{}, fmt.Errorf("store.Export: marshal manifest: %w", err)
	}
	if err := os.WriteFile(mPath, mData, 0o600); err != nil {
		return ExportManifest{}, fmt.Errorf("store.Export: write manifest: %w", err)
	}

	return manifest, nil
}

func (s *Store) exportObservations(ctx context.Context, outDir string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, sync_id, session_id, type, title, content,
		       tool_name, project, scope, topic_key, normalized_hash,
		       revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("store.Export observations query: %w", err)
	}
	defer rows.Close()

	type row struct {
		ID             int64   `json:"id"`
		SyncID         string  `json:"sync_id"`
		SessionID      string  `json:"session_id"`
		Type           string  `json:"type"`
		Title          string  `json:"title"`
		Content        string  `json:"content"`
		ToolName       *string `json:"tool_name"`
		Project        string  `json:"project"`
		Scope          string  `json:"scope"`
		TopicKey       *string `json:"topic_key"`
		NormalizedHash string  `json:"normalized_hash"`
		RevisionCount  int     `json:"revision_count"`
		DuplicateCount int     `json:"duplicate_count"`
		LastSeenAt     string  `json:"last_seen_at"`
		CreatedAt      string  `json:"created_at"`
		UpdatedAt      string  `json:"updated_at"`
		DeletedAt      *string `json:"deleted_at"`
	}

	f, err := os.Create(filepath.Join(outDir, "observations.jsonl"))
	if err != nil {
		return 0, fmt.Errorf("store.Export create observations.jsonl: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	var count int
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.SyncID, &r.SessionID, &r.Type, &r.Title, &r.Content,
			&r.ToolName, &r.Project, &r.Scope, &r.TopicKey, &r.NormalizedHash,
			&r.RevisionCount, &r.DuplicateCount, &r.LastSeenAt, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt,
		); err != nil {
			return 0, fmt.Errorf("store.Export scan observation: %w", err)
		}
		if err := enc.Encode(r); err != nil {
			return 0, fmt.Errorf("store.Export encode observation: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

func (s *Store) exportPrompts(ctx context.Context, outDir string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, sync_id, session_id, content, project, created_at
		FROM user_prompts ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("store.Export prompts query: %w", err)
	}
	defer rows.Close()

	type row struct {
		ID        int64  `json:"id"`
		SyncID    string `json:"sync_id"`
		SessionID string `json:"session_id"`
		Content   string `json:"content"`
		Project   string `json:"project"`
		CreatedAt string `json:"created_at"`
	}

	f, err := os.Create(filepath.Join(outDir, "prompts.jsonl"))
	if err != nil {
		return 0, fmt.Errorf("store.Export create prompts.jsonl: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	var count int
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.SyncID, &r.SessionID, &r.Content, &r.Project, &r.CreatedAt); err != nil {
			return 0, fmt.Errorf("store.Export scan prompt: %w", err)
		}
		if err := enc.Encode(r); err != nil {
			return 0, fmt.Errorf("store.Export encode prompt: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

func (s *Store) exportSessions(ctx context.Context, outDir string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project, directory, started_at, ended_at, summary, status
		FROM sessions ORDER BY started_at`)
	if err != nil {
		return 0, fmt.Errorf("store.Export sessions query: %w", err)
	}
	defer rows.Close()

	type row struct {
		ID        string  `json:"id"`
		Project   string  `json:"project"`
		Directory string  `json:"directory"`
		StartedAt string  `json:"started_at"`
		EndedAt   *string `json:"ended_at"`
		Summary   *string `json:"summary"`
		Status    string  `json:"status"`
	}

	f, err := os.Create(filepath.Join(outDir, "sessions.jsonl"))
	if err != nil {
		return 0, fmt.Errorf("store.Export create sessions.jsonl: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	var count int
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Project, &r.Directory, &r.StartedAt, &r.EndedAt, &r.Summary, &r.Status); err != nil {
			return 0, fmt.Errorf("store.Export scan session: %w", err)
		}
		if err := enc.Encode(r); err != nil {
			return 0, fmt.Errorf("store.Export encode session: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

func (s *Store) exportRevisions(ctx context.Context, outDir string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, observation_id, revision, type, title, content, tool_name, created_at, archived_at
		FROM observation_revisions ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("store.Export revisions query: %w", err)
	}
	defer rows.Close()

	type row struct {
		ID            int64   `json:"id"`
		ObservationID int64   `json:"observation_id"`
		Revision      int     `json:"revision"`
		Type          string  `json:"type"`
		Title         string  `json:"title"`
		Content       string  `json:"content"`
		ToolName      *string `json:"tool_name"`
		CreatedAt     string  `json:"created_at"`
		ArchivedAt    string  `json:"archived_at"`
	}

	f, err := os.Create(filepath.Join(outDir, "revisions.jsonl"))
	if err != nil {
		return 0, fmt.Errorf("store.Export create revisions.jsonl: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	var count int
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.ObservationID, &r.Revision, &r.Type, &r.Title, &r.Content,
			&r.ToolName, &r.CreatedAt, &r.ArchivedAt,
		); err != nil {
			return 0, fmt.Errorf("store.Export scan revision: %w", err)
		}
		if err := enc.Encode(r); err != nil {
			return 0, fmt.Errorf("store.Export encode revision: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

func (s *Store) exportSettings(ctx context.Context, outDir string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return 0, fmt.Errorf("store.Export settings query: %w", err)
	}
	defer rows.Close()

	type row struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		UpdatedAt string `json:"updated_at"`
	}

	f, err := os.Create(filepath.Join(outDir, "settings.jsonl"))
	if err != nil {
		return 0, fmt.Errorf("store.Export create settings.jsonl: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	var count int
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Key, &r.Value, &r.UpdatedAt); err != nil {
			return 0, fmt.Errorf("store.Export scan setting: %w", err)
		}
		if err := enc.Encode(r); err != nil {
			return 0, fmt.Errorf("store.Export encode setting: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

// ─── Prune ────────────────────────────────────────────────────────────────────

// CountPrunablePrompts returns the number of user_prompts rows with created_at
// strictly before the cutoff (RFC3339 string). These are candidates for deletion
// in the prune dry-run.
func (s *Store) CountPrunablePrompts(ctx context.Context, cutoff string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_prompts WHERE created_at < ?", cutoff,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store.CountPrunablePrompts: %w", err)
	}
	return n, nil
}

// PrunePrompts hard-deletes user_prompts rows created before cutoff (RFC3339).
// Returns the number of rows deleted.
func (s *Store) PrunePrompts(ctx context.Context, cutoff string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM user_prompts WHERE created_at < ?", cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("store.PrunePrompts: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store.PrunePrompts rows affected: %w", err)
	}
	return n, nil
}

// CountPrunableDeletedObs returns the number of soft-deleted observations with
// deleted_at strictly before the cutoff (RFC3339).
func (s *Store) CountPrunableDeletedObs(ctx context.Context, cutoff string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE deleted_at IS NOT NULL AND deleted_at < ?", cutoff,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store.CountPrunableDeletedObs: %w", err)
	}
	return n, nil
}

// PruneDeletedObs hard-deletes observations whose deleted_at is before cutoff
// (RFC3339). The obs_fts_delete trigger cleans the FTS index, and ON DELETE
// CASCADE propagates to observation_embeddings and observation_revisions.
// Returns the number of observation rows deleted.
func (s *Store) PruneDeletedObs(ctx context.Context, cutoff string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM observations WHERE deleted_at IS NOT NULL AND deleted_at < ?", cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("store.PruneDeletedObs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store.PruneDeletedObs rows affected: %w", err)
	}
	return n, nil
}

// ─── RenameProject ────────────────────────────────────────────────────────────

// RenameProject renames all rows across observations, sessions, and user_prompts
// from oldName to newName in a single transaction. Returns the total number of
// rows updated across all three tables, or an error if oldName does not exist
// (0 rows found), or if either name is empty, or if they are equal.
func (s *Store) RenameProject(ctx context.Context, oldName, newName string) (int64, error) {
	if oldName == "" {
		return 0, fmt.Errorf("store.RenameProject: old project name must not be empty")
	}
	if newName == "" {
		return 0, fmt.Errorf("store.RenameProject: new project name must not be empty")
	}
	if oldName == newName {
		return 0, fmt.Errorf("store.RenameProject: old and new names are identical (%q)", oldName)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store.RenameProject: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	tables := []string{"observations", "sessions", "user_prompts"}
	var total int64
	for _, tbl := range tables {
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET project=? WHERE project=?", tbl), //nolint:gosec
			newName, oldName,
		)
		if err != nil {
			return 0, fmt.Errorf("store.RenameProject: update %s: %w", tbl, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}

	if total == 0 {
		return 0, fmt.Errorf("store.RenameProject: project %q not found", oldName)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store.RenameProject: commit: %w", err)
	}
	return total, nil
}
