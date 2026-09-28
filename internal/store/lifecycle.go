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

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
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
		       revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at,
		       status, superseded_by, status_reason, status_changed_at
		FROM observations ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("store.Export observations query: %w", err)
	}
	defer rows.Close()

	type row struct {
		ID              int64   `json:"id"`
		SyncID          string  `json:"sync_id"`
		SessionID       string  `json:"session_id"`
		Type            string  `json:"type"`
		Title           string  `json:"title"`
		Content         string  `json:"content"`
		ToolName        *string `json:"tool_name"`
		Project         string  `json:"project"`
		Scope           string  `json:"scope"`
		TopicKey        *string `json:"topic_key"`
		NormalizedHash  string  `json:"normalized_hash"`
		RevisionCount   int     `json:"revision_count"`
		DuplicateCount  int     `json:"duplicate_count"`
		LastSeenAt      string  `json:"last_seen_at"`
		CreatedAt       string  `json:"created_at"`
		UpdatedAt       string  `json:"updated_at"`
		DeletedAt       *string `json:"deleted_at"`
		Status          string  `json:"status"`
		SupersededBy    *int64  `json:"superseded_by"`
		StatusReason    *string `json:"status_reason"`
		StatusChangedAt *string `json:"status_changed_at"`
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
			&r.Status, &r.SupersededBy, &r.StatusReason, &r.StatusChangedAt,
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
//
// Permanent-type rows (see Permanent: bugfix, discovery) are never
// hard-deleted here, even when a human soft-deleted them and they are older
// than cutoff — they are the project's durable learning record. Such rows
// are counted in skipped instead of being removed.
//
// Returns the number of observation rows deleted and the number of
// permanent-type rows that matched the cutoff but were skipped.
func (s *Store) PruneDeletedObs(ctx context.Context, cutoff string) (deleted int64, skipped int64, err error) {
	permanentPlaceholders, permanentArgs := permanentTypesInClause()

	skippedArgs := append([]interface{}{cutoff}, permanentArgs...)
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE deleted_at IS NOT NULL AND deleted_at < ? AND type IN ("+permanentPlaceholders+")",
		skippedArgs...,
	).Scan(&skipped)
	if err != nil {
		return 0, 0, fmt.Errorf("store.PruneDeletedObs count skipped: %w", err)
	}

	deleteArgs := append([]interface{}{cutoff}, permanentArgs...)
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM observations WHERE deleted_at IS NOT NULL AND deleted_at < ? AND type NOT IN ("+permanentPlaceholders+")",
		deleteArgs...,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("store.PruneDeletedObs: %w", err)
	}
	deleted, err = res.RowsAffected()
	if err != nil {
		return 0, 0, fmt.Errorf("store.PruneDeletedObs rows affected: %w", err)
	}
	return deleted, skipped, nil
}

// permanentTypesInClause builds a "?,?,..." placeholder string and the
// matching []interface{} argument slice for permanentObservationTypes, for
// use in a SQL "type IN (...)" clause.
func permanentTypesInClause() (placeholders string, args []interface{}) {
	args = make([]interface{}, 0, len(permanentObservationTypes))
	for typ := range permanentObservationTypes {
		if placeholders != "" {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, typ)
	}
	return placeholders, args
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

// ─── ExportProject / ImportProject ───────────────────────────────────────────
//
// These build and consume the portable per-project bundle format (see
// internal/bundle): a single file a developer can hand to a teammate on the
// same repo. Unlike Export (whole-store, local-id JSONL dump used for
// operator backups), ExportProject/ImportProject key everything by sync_id
// so the bundle can be merged into any checkout without id collisions, and
// ImportProject never overwrites or deletes a teammate's existing rows
// unless they explicitly opt into PreferBundle.

// ExportProjectOptions controls what ExportProject includes.
type ExportProjectOptions struct {
	// IncludeDeleted includes soft-deleted observations (default: excluded).
	IncludeDeleted bool
	// IncludePrompts includes that project's user_prompts rows (default:
	// excluded — prompts are the most likely place for stray secrets, so
	// they are opt-in only).
	IncludePrompts bool
}

// ExportProject builds a bundle.Bundle containing only the given project's
// observations (their revisions, and optionally prompts). Every
// cross-reference (superseded_by) is resolved to sync_id at export time;
// a target that is deleted, in another project, or otherwise absent from
// the exported set resolves to nil (see bundle.Observation.SupersededBySyncID).
func (s *Store) ExportProject(ctx context.Context, project string, opts ExportProjectOptions) (bundle.Bundle, error) {
	deletedClause := ""
	deletedClauseQualified := ""
	if !opts.IncludeDeleted {
		deletedClause = " AND deleted_at IS NULL"
		deletedClauseQualified = " AND o.deleted_at IS NULL"
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+observationColumns+`
		FROM observations
		WHERE project=?`+deletedClause+`
		ORDER BY id`, project)
	if err != nil {
		return bundle.Bundle{}, fmt.Errorf("store.ExportProject observations query: %w", err)
	}

	var obs []Observation
	idToSync := make(map[int64]string)
	for rows.Next() {
		o, scanErr := scanObservationRow(rows)
		if scanErr != nil {
			rows.Close()
			return bundle.Bundle{}, fmt.Errorf("store.ExportProject scan observation: %w", scanErr)
		}
		obs = append(obs, o)
		idToSync[o.ID] = o.SyncID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return bundle.Bundle{}, fmt.Errorf("store.ExportProject observations rows: %w", err)
	}
	rows.Close()

	bundleObs := make([]bundle.Observation, 0, len(obs))
	for _, o := range obs {
		var supersededSync *string
		if o.SupersededBy != nil {
			if sy, ok := idToSync[*o.SupersededBy]; ok {
				supersededSync = &sy
			}
		}
		bundleObs = append(bundleObs, bundle.Observation{
			SyncID:             o.SyncID,
			SessionID:          o.SessionID,
			Type:               o.Type,
			Title:              o.Title,
			Content:            o.Content,
			ToolName:           o.ToolName,
			Project:            o.Project,
			Scope:              o.Scope,
			TopicKey:           o.TopicKey,
			NormalizedHash:     o.NormalizedHash,
			RevisionCount:      o.RevisionCount,
			DuplicateCount:     o.DuplicateCount,
			LastSeenAt:         o.LastSeenAt,
			CreatedAt:          o.CreatedAt,
			UpdatedAt:          o.UpdatedAt,
			Status:             o.Status,
			SupersededBySyncID: supersededSync,
			StatusReason:       o.StatusReason,
			StatusChangedAt:    o.StatusChangedAt,
		})
	}

	revRows, err := s.db.QueryContext(ctx, `
		SELECT r.observation_id, r.revision, r.type, r.title, r.content, r.tool_name, r.created_at, r.archived_at
		FROM observation_revisions r
		JOIN observations o ON o.id = r.observation_id
		WHERE o.project=?`+deletedClauseQualified+`
		ORDER BY r.id`, project)
	if err != nil {
		return bundle.Bundle{}, fmt.Errorf("store.ExportProject revisions query: %w", err)
	}
	defer revRows.Close()

	bundleRevs := []bundle.Revision{}
	for revRows.Next() {
		var obsID int64
		var revNum int
		var typ, title, content, createdAt, archivedAt string
		var toolName sql.NullString
		if scanErr := revRows.Scan(&obsID, &revNum, &typ, &title, &content, &toolName, &createdAt, &archivedAt); scanErr != nil {
			return bundle.Bundle{}, fmt.Errorf("store.ExportProject scan revision: %w", scanErr)
		}
		syncID, ok := idToSync[obsID]
		if !ok {
			continue
		}
		var toolNamePtr *string
		if toolName.Valid {
			toolNamePtr = &toolName.String
		}
		bundleRevs = append(bundleRevs, bundle.Revision{
			ObservationSyncID: syncID,
			Revision:          revNum,
			Type:              typ,
			Title:             title,
			Content:           content,
			ToolName:          toolNamePtr,
			CreatedAt:         createdAt,
			ArchivedAt:        archivedAt,
		})
	}
	if err := revRows.Err(); err != nil {
		return bundle.Bundle{}, fmt.Errorf("store.ExportProject revisions rows: %w", err)
	}

	bundlePrompts := []bundle.Prompt{}
	if opts.IncludePrompts {
		pRows, err := s.db.QueryContext(ctx, `
			SELECT sync_id, session_id, content, project, created_at
			FROM user_prompts
			WHERE project=?
			ORDER BY id`, project)
		if err != nil {
			return bundle.Bundle{}, fmt.Errorf("store.ExportProject prompts query: %w", err)
		}
		defer pRows.Close()
		for pRows.Next() {
			var p bundle.Prompt
			if scanErr := pRows.Scan(&p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt); scanErr != nil {
				return bundle.Bundle{}, fmt.Errorf("store.ExportProject scan prompt: %w", scanErr)
			}
			bundlePrompts = append(bundlePrompts, p)
		}
		if err := pRows.Err(); err != nil {
			return bundle.Bundle{}, fmt.Errorf("store.ExportProject prompts rows: %w", err)
		}
	}

	sourceHost, _ := os.Hostname()

	manifest := bundle.Manifest{
		FormatVersion:   bundle.FormatVersion,
		Project:         project,
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		SourceHost:      sourceHost,
		IncludesPrompts: opts.IncludePrompts,
		Counts: bundle.Counts{
			Observations: len(bundleObs),
			Revisions:    len(bundleRevs),
			Prompts:      len(bundlePrompts),
		},
	}

	return bundle.Bundle{
		Manifest:     manifest,
		Observations: bundleObs,
		Revisions:    bundleRevs,
		Prompts:      bundlePrompts,
	}, nil
}

// ImportOptions controls how ImportProject merges a bundle into the local
// store.
type ImportOptions struct {
	// TargetProject is the local project name every imported row is written
	// under. Required. May differ from bundle.Manifest.Project (callers are
	// expected to surface that mismatch loudly before calling).
	TargetProject string
	// DryRun computes and returns the same ImportReport that a real import
	// would produce, without writing anything. Implemented by running the
	// exact same transactional logic and rolling back at the end instead of
	// committing, so dry-run and apply can never drift apart.
	DryRun bool
	// PreferBundle resolves sync_id conflicts (existing local row whose
	// normalized_hash/status/title differs from the bundle) in favor of the
	// bundle's version, capturing the overwritten local content as a
	// revision first. Default (false): the local row always wins.
	PreferBundle bool
	// IncludePrompts imports bundle.Prompts (only meaningful when the
	// bundle itself was exported --with-prompts).
	IncludePrompts bool
}

// Conflict describes one sync_id present in both the bundle and the local
// store whose normalized_hash, status, or title differ.
type Conflict struct {
	SyncID string
	Title  string
	Reason string
}

// StatusDowngrade describes one imported row whose BUNDLED status
// (superseded or obsolete) was rejected by the same invariants
// SetObservationStatus enforces — cross-project or non-active
// superseded_by target, a permanent type (bugfix/discovery) marked
// obsolete, or a superseded_by chain that would form a cycle. The row is
// still inserted/updated with its content, just left at status=active with
// Reason recorded in its status_reason column, so nothing is ever refused
// wholesale over a bad status pointer.
type StatusDowngrade struct {
	SyncID string
	Wanted string
	Reason string
}

// ImportReport summarizes what ImportProject did (or, under DryRun, would do).
type ImportReport struct {
	Inserted  int
	Skipped   int
	Updated   int
	Conflicts []Conflict
	// UnresolvedSupersedes lists sync_ids of imported rows whose
	// superseded_by target does not resolve to ANY local row (a dangling
	// pointer — the target was excluded from the bundle, or never
	// imported). Distinct from StatusDowngrades, which is for a target that
	// DOES resolve but fails a status invariant.
	UnresolvedSupersedes []string
	StatusDowngrades     []StatusDowngrade
	Prompts              int
	// SessionID is the synthetic "import-<timestamp>" session created to
	// own every row inserted by this call (see sessions.session_id FK).
	// Empty when nothing was actually inserted (see ensureImportSession).
	SessionID string
}

// ImportProject merges bundle b into the local store under
// opts.TargetProject, matching rows by sync_id. It never deletes anything
// and never overwrites an existing row unless opts.PreferBundle is set. The
// whole operation runs in one transaction; when opts.DryRun is set, that
// transaction is rolled back instead of committed, so DryRun and a real
// import share identical logic and can never disagree with each other.
func (s *Store) ImportProject(ctx context.Context, b bundle.Bundle, opts ImportOptions) (ImportReport, error) {
	if opts.TargetProject == "" {
		return ImportReport{}, fmt.Errorf("store.ImportProject: TargetProject must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportReport{}, fmt.Errorf("store.ImportProject: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	report := ImportReport{
		Conflicts:            []Conflict{},
		UnresolvedSupersedes: []string{},
	}

	// The synthetic "import-<timestamp>" session is created lazily, on the
	// first row this import actually inserts — a no-op import (everything
	// skipped or conflicted) must not leave a dangling empty session behind.
	var sessionID string
	sessionCreated := false
	ensureImportSession := func() (string, error) {
		if sessionCreated {
			return sessionID, nil
		}
		sessionID = "import-" + time.Now().UTC().Format("20060102-150405.000000000")
		now := nowISO()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (id, project, directory, started_at, status)
			VALUES (?, ?, ?, ?, 'active')`,
			sessionID, opts.TargetProject, "(import)", now,
		); err != nil {
			return "", fmt.Errorf("store.ImportProject: create session: %w", err)
		}
		sessionCreated = true
		report.SessionID = sessionID
		return sessionID, nil
	}

	// insertedSyncToID scopes revision carry-over to rows newly inserted by
	// THIS import (unchanged rule). touchedLocalID/pendingBundleObs are
	// broader — every row this import wrote content for, inserted OR
	// PreferBundle-updated — and drive the status-application pass below,
	// since either kind of write needs its bundled status validated the
	// same way.
	insertedSyncToID := make(map[string]int64)
	touchedLocalID := make(map[string]int64)
	pendingBundleObs := make(map[string]bundle.Observation)

	for _, bo := range b.Observations {
		var existingID int64
		var existingHash, existingStatus, existingTitle string
		probeErr := tx.QueryRowContext(ctx,
			`SELECT id, normalized_hash, status, title FROM observations WHERE sync_id=? AND project=?`,
			bo.SyncID, opts.TargetProject,
		).Scan(&existingID, &existingHash, &existingStatus, &existingTitle)

		if probeErr != nil && !errors.Is(probeErr, sql.ErrNoRows) {
			return ImportReport{}, fmt.Errorf("store.ImportProject: probe sync_id %s: %w", bo.SyncID, probeErr)
		}

		if errors.Is(probeErr, sql.ErrNoRows) {
			// Not found under TargetProject. sync_id is globally UNIQUE at the
			// schema level, so it may still exist under a DIFFERENT project —
			// inserting would violate that UNIQUE constraint, and silently
			// updating a foreign-project row would bleed content across
			// projects. Check before deciding this is genuinely new.
			var foreignProject string
			foreignErr := tx.QueryRowContext(ctx,
				`SELECT project FROM observations WHERE sync_id=?`, bo.SyncID,
			).Scan(&foreignProject)
			if foreignErr == nil {
				report.Conflicts = append(report.Conflicts, Conflict{
					SyncID: bo.SyncID, Title: bo.Title,
					Reason: fmt.Sprintf("sync_id exists under project %q", foreignProject),
				})
				continue
			}
			if !errors.Is(foreignErr, sql.ErrNoRows) {
				return ImportReport{}, fmt.Errorf("store.ImportProject: probe foreign-project sync_id %s: %w", bo.SyncID, foreignErr)
			}

			// Genuinely new: insert as status=active. Its bundled status
			// (if any) is applied afterward by the status-application pass
			// below, through the same invariants SetObservationStatus
			// enforces — never written directly here.
			sid, sessErr := ensureImportSession()
			if sessErr != nil {
				return ImportReport{}, sessErr
			}
			newID, insertErr := importInsertObservation(ctx, tx, sid, opts.TargetProject, bo)
			if insertErr != nil {
				return ImportReport{}, insertErr
			}
			insertedSyncToID[bo.SyncID] = newID
			touchedLocalID[bo.SyncID] = newID
			pendingBundleObs[bo.SyncID] = bo
			report.Inserted++
			continue
		}

		// Existing row IS in the target project — normal skip/update/conflict logic.
		if existingHash == bo.NormalizedHash && existingStatus == bo.Status && existingTitle == bo.Title {
			report.Skipped++
			continue
		}
		if !opts.PreferBundle {
			report.Conflicts = append(report.Conflicts, Conflict{
				SyncID: bo.SyncID, Title: bo.Title, Reason: "differs from bundle; kept local",
			})
			continue
		}

		current, curErr := readCurrentObservation(ctx, tx, existingID)
		if curErr != nil {
			report.Conflicts = append(report.Conflicts, Conflict{
				SyncID: bo.SyncID, Title: bo.Title, Reason: "local row unavailable for update: " + curErr.Error(),
			})
			continue
		}
		nowUpd := nowISO()
		if err := captureRevision(ctx, tx, current, nowUpd); err != nil {
			return ImportReport{}, err
		}
		if err := importUpdateObservation(ctx, tx, existingID, bo, nowUpd); err != nil {
			return ImportReport{}, err
		}
		report.Updated++
		report.Conflicts = append(report.Conflicts, Conflict{
			SyncID: bo.SyncID, Title: bo.Title, Reason: "updated from bundle (PreferBundle)",
		})
		touchedLocalID[bo.SyncID] = existingID
		pendingBundleObs[bo.SyncID] = bo
	}

	// Revisions carry over only for observations newly inserted by this import.
	for _, br := range b.Revisions {
		localID, ok := insertedSyncToID[br.ObservationSyncID]
		if !ok {
			continue
		}
		var toolNameArg interface{}
		if br.ToolName != nil {
			toolNameArg = *br.ToolName
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO observation_revisions
			    (observation_id, revision, type, title, content, tool_name, created_at, archived_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			localID, br.Revision, br.Type, br.Title, br.Content, toolNameArg, br.CreatedAt, br.ArchivedAt,
		); err != nil {
			return ImportReport{}, fmt.Errorf("store.ImportProject: insert revision for %s: %w", br.ObservationSyncID, err)
		}
	}

	// Apply each touched row's BUNDLED status (superseded/obsolete) through
	// the exact same invariants SetObservationStatus enforces (see
	// validateStatusTransition). Every touched row was just written as
	// status=active with no superseded_by (importInsertObservation /
	// importUpdateObservation), so this pass is the ONLY place import ever
	// changes status/superseded_by — never bypassed, never written raw.
	//
	// A cycle pre-scan runs first, purely against the BUNDLE's own declared
	// superseded_by_sync_id graph rather than live DB state: checking
	// sequentially against the database would let whichever row of a
	// mutual/cyclic pair happens to be processed first "win" (the DB only
	// "sees" the cycle once the first side has already been mutated),
	// silently breaking symmetry. Validating against the bundle's static
	// declarations instead rejects BOTH sides of a genuine cycle up front.
	cycleReason := make(map[string]string, len(pendingBundleObs))
	for syncID, bo := range pendingBundleObs {
		if bo.Status != StatusSuperseded || bo.SupersededBySyncID == nil {
			continue
		}
		if detectBundleCycle(pendingBundleObs, syncID, *bo.SupersededBySyncID) {
			cycleReason[syncID] = fmt.Sprintf("would create a cycle with %s", *bo.SupersededBySyncID)
		}
	}

	for _, bo := range b.Observations {
		localID, ok := touchedLocalID[bo.SyncID]
		if !ok || bo.Status == "" || bo.Status == StatusActive {
			continue // not written this round, or already active — nothing to apply
		}

		if reason, rejected := cycleReason[bo.SyncID]; rejected {
			if err := applyStatusDowngrade(ctx, tx, localID, "import: "+reason); err != nil {
				return ImportReport{}, err
			}
			report.StatusDowngrades = append(report.StatusDowngrades, StatusDowngrade{
				SyncID: bo.SyncID, Wanted: bo.Status, Reason: reason,
			})
			continue
		}

		var supersededByLocalID *int64
		if bo.Status == StatusSuperseded {
			if bo.SupersededBySyncID == nil {
				reason := "status=superseded requires a superseded_by target"
				if err := applyStatusDowngrade(ctx, tx, localID, "import: "+reason); err != nil {
					return ImportReport{}, err
				}
				report.StatusDowngrades = append(report.StatusDowngrades, StatusDowngrade{
					SyncID: bo.SyncID, Wanted: bo.Status, Reason: reason,
				})
				continue
			}
			var targetID int64
			targetErr := tx.QueryRowContext(ctx,
				`SELECT id FROM observations WHERE sync_id=?`, *bo.SupersededBySyncID,
			).Scan(&targetID)
			if errors.Is(targetErr, sql.ErrNoRows) {
				// Dangling pointer — the target sync_id doesn't resolve to
				// ANY local row (excluded from the bundle, or never
				// imported). Distinct from a resolved-but-invalid target,
				// which lands in StatusDowngrades below via
				// validateStatusTransition.
				if err := applyStatusDowngrade(ctx, tx, localID, "import: target unavailable"); err != nil {
					return ImportReport{}, err
				}
				report.UnresolvedSupersedes = append(report.UnresolvedSupersedes, bo.SyncID)
				continue
			}
			if targetErr != nil {
				return ImportReport{}, fmt.Errorf("store.ImportProject: resolve superseded_by target for %s: %w", bo.SyncID, targetErr)
			}
			supersededByLocalID = &targetID
		}

		current, curErr := readCurrentObservation(ctx, tx, localID)
		if curErr != nil {
			return ImportReport{}, fmt.Errorf("store.ImportProject: read current for status apply %s: %w", bo.SyncID, curErr)
		}

		target, valErr := validateStatusTransition(ctx, tx, current, bo.Status, supersededByLocalID)
		if valErr != nil {
			reason := valErr.Error()
			if err := applyStatusDowngrade(ctx, tx, localID, "import: "+reason); err != nil {
				return ImportReport{}, err
			}
			report.StatusDowngrades = append(report.StatusDowngrades, StatusDowngrade{
				SyncID: bo.SyncID, Wanted: bo.Status, Reason: reason,
			})
			continue
		}

		var supersededByArg interface{}
		if target != nil {
			supersededByArg = target.ID
		}
		var reasonArg interface{}
		if bo.StatusReason != nil && *bo.StatusReason != "" {
			reasonArg = *bo.StatusReason
		}
		changedAt := nowISO()
		if bo.StatusChangedAt != nil && *bo.StatusChangedAt != "" {
			changedAt = *bo.StatusChangedAt
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE observations SET status=?, superseded_by=?, status_reason=?, status_changed_at=?
			WHERE id=?`,
			bo.Status, supersededByArg, reasonArg, changedAt, localID,
		); err != nil {
			return ImportReport{}, fmt.Errorf("store.ImportProject: apply status for %s: %w", bo.SyncID, err)
		}
	}

	if opts.IncludePrompts && b.Manifest.IncludesPrompts {
		for _, bp := range b.Prompts {
			var existingID int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM user_prompts WHERE sync_id=?`, bp.SyncID).Scan(&existingID)
			if err == nil {
				continue // already present locally; prompts are never overwritten
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return ImportReport{}, fmt.Errorf("store.ImportProject: probe prompt sync_id %s: %w", bp.SyncID, err)
			}
			sid, sessErr := ensureImportSession()
			if sessErr != nil {
				return ImportReport{}, sessErr
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO user_prompts (sync_id, session_id, content, project, created_at)
				VALUES (?, ?, ?, ?, ?)`,
				bp.SyncID, sid, bp.Content, opts.TargetProject, bp.CreatedAt,
			); err != nil {
				return ImportReport{}, fmt.Errorf("store.ImportProject: insert prompt %s: %w", bp.SyncID, err)
			}
			report.Prompts++
		}
	}

	if opts.DryRun {
		// Rolled back via the deferred tx.Rollback(); report reflects what
		// WOULD have happened.
		return report, nil
	}
	if err := tx.Commit(); err != nil {
		return ImportReport{}, fmt.Errorf("store.ImportProject: commit: %w", err)
	}
	return report, nil
}

// importInsertObservation inserts a brand-new observation row sourced from a
// bundle record, preserving its sync_id, timestamps, and content verbatim
// (this is a cross-checkout MERGE, not a fresh local write).
//
// It is ALWAYS inserted as status=active with superseded_by/status_reason/
// status_changed_at left at their column defaults (NULL), regardless of
// bo.Status. The bundle's declared status is applied afterward by
// ImportProject's status-application pass, which runs it through the same
// invariants SetObservationStatus enforces (cross-project/non-active
// target, permanent-type rule, cycle detection) — writing it here directly
// would bypass all of that.
func importInsertObservation(ctx context.Context, tx *sql.Tx, sessionID, targetProject string, bo bundle.Observation) (int64, error) {
	var toolNameArg, topicKeyArg interface{}
	if bo.ToolName != nil {
		toolNameArg = *bo.ToolName
	}
	if bo.TopicKey != nil {
		topicKeyArg = *bo.TopicKey
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO observations
		    (sync_id, session_id, type, title, content, tool_name, project, scope,
		     topic_key, normalized_hash, revision_count, duplicate_count,
		     last_seen_at, created_at, updated_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bo.SyncID, sessionID, bo.Type, bo.Title, bo.Content, toolNameArg, targetProject, bo.Scope,
		topicKeyArg, bo.NormalizedHash, bo.RevisionCount, bo.DuplicateCount,
		bo.LastSeenAt, bo.CreatedAt, bo.UpdatedAt, StatusActive,
	)
	if err != nil {
		return 0, fmt.Errorf("store.ImportProject: insert observation %s: %w", bo.SyncID, err)
	}
	return res.LastInsertId()
}

// importUpdateObservation overwrites an existing local row's content fields
// with the bundle's version (PreferBundle path). Caller is responsible for
// capturing a revision of the pre-update row first.
//
// Synced from the bundle: type, title, content, tool_name, scope, topic_key,
// normalized_hash, last_seen_at — everything that describes WHAT the
// observation is and WHEN it was last seen by the exporting side.
//
// Deliberately left untouched (kept at their current local value):
// session_id and duplicate_count. Both are local bookkeeping about how THIS
// store came to have the row (which of its own sessions wrote/deduped it),
// not portable facts about the observation itself — overwriting them from
// the bundle would make local session/dedup accounting describe a session
// that may not even exist here, or double-count another store's dedup hits
// as this store's own.
//
// status/superseded_by/status_reason/status_changed_at are ALWAYS reset to
// active/NULL/NULL/NULL here, regardless of bo.Status — same reasoning as
// importInsertObservation: the bundle's declared status is applied
// afterward through the validated status-application pass, never written
// directly. Resetting superseded_by here also matters on its own: leaving a
// stale pointer on a row now marked active would violate the invariant that
// an active row's superseded_by is always nil (see detectSupersededByCycle).
func importUpdateObservation(ctx context.Context, tx *sql.Tx, id int64, bo bundle.Observation, now string) error {
	var toolNameArg, topicKeyArg interface{}
	if bo.ToolName != nil {
		toolNameArg = *bo.ToolName
	}
	if bo.TopicKey != nil {
		topicKeyArg = *bo.TopicKey
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE observations
		SET type=?, title=?, content=?, tool_name=?, scope=?, topic_key=?, normalized_hash=?,
		    last_seen_at=?, status=?, superseded_by=NULL, status_reason=NULL, status_changed_at=NULL,
		    revision_count=revision_count+1, updated_at=?
		WHERE id=?`,
		bo.Type, bo.Title, bo.Content, toolNameArg, bo.Scope, topicKeyArg, bo.NormalizedHash,
		bo.LastSeenAt, StatusActive, now, id,
	)
	if err != nil {
		return fmt.Errorf("store.ImportProject: update observation %s: %w", bo.SyncID, err)
	}
	return nil
}

// applyStatusDowngrade resets an imported row to status=active with the
// given (already "import: "-prefixed) reason recorded, clearing any
// superseded_by pointer. Used by the status-application pass in
// ImportProject when the bundle's declared status is rejected by
// validateStatusTransition, a cycle pre-scan, or an unresolved
// superseded_by target — the row's CONTENT is still imported; only its
// status is downgraded.
func applyStatusDowngrade(ctx context.Context, tx *sql.Tx, localID int64, reason string) error {
	now := nowISO()
	_, err := tx.ExecContext(ctx, `
		UPDATE observations
		SET status=?, superseded_by=NULL, status_reason=?, status_changed_at=?
		WHERE id=?`,
		StatusActive, reason, now, localID,
	)
	if err != nil {
		return fmt.Errorf("store.ImportProject: downgrade to active: %w", err)
	}
	return nil
}

// detectBundleCycle reports whether pointing startSync's superseded_by at
// targetSync would create a cycle, walking PURELY the bundle's own declared
// superseded_by_sync_id graph (via pending) rather than live database
// state. Only edges belonging to rows THIS import is writing (present in
// pending) with a declared status of StatusSuperseded are followed; a
// target outside pending (a pre-existing local row, or simply absent) ends
// the walk with "no cycle" — that case is validated against real DB state
// separately (see validateStatusTransition).
//
// This must run before any real status transition is written: checking
// against the database sequentially would let whichever row of a mutual or
// cyclic pair is processed first "win" (only after committing the first
// side does the DB even start to look cyclic to the second), silently
// breaking symmetry. Pre-scanning the bundle's static declarations instead
// catches a genuine cycle before either side is touched.
func detectBundleCycle(pending map[string]bundle.Observation, startSync, targetSync string) bool {
	cursor := targetSync
	for hops := 0; hops < maxSupersededByChainHops; hops++ {
		if cursor == startSync {
			return true
		}
		next, ok := pending[cursor]
		if !ok || next.Status != StatusSuperseded || next.SupersededBySyncID == nil {
			return false
		}
		cursor = *next.SupersededBySyncID
	}
	return false
}
