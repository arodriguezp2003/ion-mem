package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Observation represents a row in the observations table.
type Observation struct {
	ID             int64
	SyncID         string
	SessionID      string
	Type           string
	Title          string
	Content        string
	ToolName       *string
	Project        string
	Scope          string
	TopicKey       *string
	NormalizedHash string
	RevisionCount  int
	DuplicateCount int
	LastSeenAt     string
	CreatedAt      string
	UpdatedAt      string
	DeletedAt      *string
}

// AddObservationParams carries the caller-supplied fields for a new observation.
type AddObservationParams struct {
	SessionID string
	Type      string
	Title     string
	Content   string
	ToolName  string // optional; empty = NULL
	Project   string
	Scope     string // defaults to "project" if empty
	TopicKey  string // optional; empty = NULL
}

// UpdateObservationParams carries optional fields to update. Nil pointer fields
// are skipped; updated_at is always set.
type UpdateObservationParams struct {
	Type     *string
	Title    *string
	Content  *string
	ToolName *string
	TopicKey *string
}

// RecentObservationsParams filters for RecentObservations.
type RecentObservationsParams struct {
	Project string // optional
	Scope   string // optional
	Limit   int    // default 50 when <= 0
}

// SearchParams carries filters for Search.
type SearchParams struct {
	Q       string // FTS5 query (required)
	Type    string // optional filter
	Project string // optional filter
	Scope   string // optional filter
	Limit   int    // default 20 when <= 0
}

// SearchResult pairs a matched Observation with its BM25 score.
// Lower score = more relevant in SQLite's BM25 implementation.
type SearchResult struct {
	Observation Observation
	Score       float64
}

// AddObservation inserts or updates an observation following three precedence rules:
//  1. Topic-key upsert (checked first when TopicKey is non-empty).
//  2. Dedup: same (normalized_hash, project, scope, type, title).
//  3. New row.
func (s *Store) AddObservation(ctx context.Context, params AddObservationParams) (Observation, error) {
	scope := normalizeScope(params.Scope)
	hash := computeDedupHash(params.Content)
	now := nowISO()

	// 1. Topic-key upsert.
	if params.TopicKey != "" {
		obs, err := s.topicKeyUpsert(ctx, params, scope, hash, now)
		if err != nil {
			return Observation{}, err
		}
		if obs.ID != 0 {
			return obs, nil
		}
	}

	// 2. Dedup probe.
	obs, err := s.dedupProbe(ctx, params, scope, hash, now)
	if err != nil {
		return Observation{}, err
	}
	if obs.ID != 0 {
		return obs, nil
	}

	// 3. Insert new row.
	return s.insertObservation(ctx, params, scope, hash, now)
}

// topicKeyUpsert checks for an existing non-deleted row with the same
// (project, scope, topic_key). If found, it updates it in place and returns it.
// Returns zero-value Observation (ID=0) when no existing row is found.
func (s *Store) topicKeyUpsert(ctx context.Context, params AddObservationParams, scope, hash, now string) (Observation, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM observations
		WHERE topic_key=? AND project=? AND scope=? AND deleted_at IS NULL
		LIMIT 1`,
		params.TopicKey, params.Project, scope,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, nil
	}
	if err != nil {
		return Observation{}, fmt.Errorf("store.AddObservation topicKey probe: %w", err)
	}

	var toolNameArg interface{}
	if params.ToolName != "" {
		toolNameArg = params.ToolName
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE observations
		SET type=?, title=?, content=?, tool_name=?, normalized_hash=?,
		    revision_count=revision_count+1, last_seen_at=?, updated_at=?
		WHERE id=?`,
		params.Type, params.Title, params.Content, toolNameArg, hash, now, now, id,
	)
	if err != nil {
		return Observation{}, fmt.Errorf("store.AddObservation topicKey update: %w", err)
	}

	return s.getObservationByID(ctx, id)
}

// dedupProbe checks for a non-deleted row matching
// (normalized_hash, project, scope, type, title). If found, increments
// duplicate_count and updates last_seen_at/updated_at; returns that row.
// Returns zero-value Observation (ID=0) when no match found.
func (s *Store) dedupProbe(ctx context.Context, params AddObservationParams, scope, hash, now string) (Observation, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM observations
		WHERE normalized_hash=? AND project=? AND scope=? AND type=? AND title=?
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`,
		hash, params.Project, scope, params.Type, params.Title,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, nil
	}
	if err != nil {
		return Observation{}, fmt.Errorf("store.AddObservation dedup probe: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE observations
		SET duplicate_count=duplicate_count+1, last_seen_at=?, updated_at=?
		WHERE id=?`,
		now, now, id,
	)
	if err != nil {
		return Observation{}, fmt.Errorf("store.AddObservation dedup update: %w", err)
	}

	return s.getObservationByID(ctx, id)
}

// insertObservation inserts a brand-new observation row.
func (s *Store) insertObservation(ctx context.Context, params AddObservationParams, scope, hash, now string) (Observation, error) {
	syncID := generateSyncID("obs-")

	var toolNameArg interface{}
	if params.ToolName != "" {
		toolNameArg = params.ToolName
	}
	var topicKeyArg interface{}
	if params.TopicKey != "" {
		topicKeyArg = params.TopicKey
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO observations
		    (sync_id, session_id, type, title, content, tool_name, project, scope,
		     topic_key, normalized_hash, revision_count, duplicate_count,
		     last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?)`,
		syncID, params.SessionID, params.Type, params.Title, params.Content,
		toolNameArg, params.Project, scope, topicKeyArg, hash,
		now, now, now,
	)
	if err != nil {
		return Observation{}, fmt.Errorf("store.AddObservation insert: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Observation{}, fmt.Errorf("store.AddObservation LastInsertId: %w", err)
	}

	return s.getObservationByID(ctx, id)
}

// GetObservation returns the observation with the given id, or
// ErrObservationNotFound. Soft-deleted observations are excluded.
func (s *Store) GetObservation(ctx context.Context, id int64) (Observation, error) {
	obs, err := s.getObservationByID(ctx, id)
	if errors.Is(err, ErrObservationNotFound) {
		return Observation{}, ErrObservationNotFound
	}
	return obs, err
}

// getObservationByID is the internal helper that also excludes soft-deleted rows.
func (s *Store) getObservationByID(ctx context.Context, id int64) (Observation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, sync_id, session_id, type, title, content, tool_name,
		       project, scope, topic_key, normalized_hash,
		       revision_count, duplicate_count, last_seen_at,
		       created_at, updated_at, deleted_at
		FROM observations
		WHERE id=? AND deleted_at IS NULL`,
		id,
	)
	return scanObservation(row)
}

// UpdateObservation applies a partial update to the observation with the given id.
// Only non-nil fields in params are changed. updated_at is always set.
// Returns ErrObservationNotFound if the id does not exist.
func (s *Store) UpdateObservation(ctx context.Context, id int64, params UpdateObservationParams) (Observation, error) {
	// Check existence first.
	var exists int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE id=? AND deleted_at IS NULL", id,
	).Scan(&exists)
	if err != nil {
		return Observation{}, fmt.Errorf("store.UpdateObservation existence check: %w", err)
	}
	if exists == 0 {
		return Observation{}, ErrObservationNotFound
	}

	now := nowISO()

	// Build dynamic SET clause.
	setClauses := []string{"updated_at=?"}
	args := []interface{}{now}

	if params.Type != nil {
		setClauses = append(setClauses, "type=?")
		args = append(args, *params.Type)
	}
	if params.Title != nil {
		setClauses = append(setClauses, "title=?")
		args = append(args, *params.Title)
	}
	if params.Content != nil {
		setClauses = append(setClauses, "content=?")
		args = append(args, *params.Content)
		// Recompute hash when content changes.
		setClauses = append(setClauses, "normalized_hash=?")
		args = append(args, computeDedupHash(*params.Content))
	}
	if params.ToolName != nil {
		setClauses = append(setClauses, "tool_name=?")
		args = append(args, *params.ToolName)
	}
	if params.TopicKey != nil {
		setClauses = append(setClauses, "topic_key=?")
		args = append(args, *params.TopicKey)
	}
	args = append(args, id)

	query := "UPDATE observations SET "
	for i, c := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += c
	}
	query += " WHERE id=?"

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return Observation{}, fmt.Errorf("store.UpdateObservation update: %w", err)
	}

	return s.getObservationByID(ctx, id)
}

// RecentObservations returns non-deleted observations ordered by created_at DESC.
// Optional filters: Project, Scope. Default Limit is 50 when <= 0.
func (s *Store) RecentObservations(ctx context.Context, params RecentObservationsParams) ([]Observation, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, sync_id, session_id, type, title, content, tool_name,
	                 project, scope, topic_key, normalized_hash,
	                 revision_count, duplicate_count, last_seen_at,
	                 created_at, updated_at, deleted_at
	          FROM observations
	          WHERE deleted_at IS NULL`
	var args []interface{}

	if params.Project != "" {
		query += " AND project=?"
		args = append(args, params.Project)
	}
	if params.Scope != "" {
		query += " AND scope=?"
		args = append(args, params.Scope)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.RecentObservations: %w", err)
	}
	defer rows.Close()

	var obs []Observation
	for rows.Next() {
		o, err := scanObservationRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store.RecentObservations scan: %w", err)
		}
		obs = append(obs, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.RecentObservations: %w", err)
	}
	return obs, nil
}

// DeleteObservation deletes the observation with the given id.
// When hard=false: sets deleted_at (soft delete).
// When hard=true: deletes the row (and FTS via trigger).
// Returns ErrObservationNotFound if the id does not exist.
func (s *Store) DeleteObservation(ctx context.Context, id int64, hard bool) error {
	// Check existence (allow soft-deleted rows to be hard-deleted).
	var count int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE id=?", id,
	).Scan(&count); err != nil {
		return fmt.Errorf("store.DeleteObservation existence check: %w", err)
	}
	if count == 0 {
		return ErrObservationNotFound
	}

	now := nowISO()
	var err error
	if hard {
		_, err = s.db.ExecContext(ctx, "DELETE FROM observations WHERE id=?", id)
	} else {
		_, err = s.db.ExecContext(ctx,
			"UPDATE observations SET deleted_at=?, updated_at=? WHERE id=?",
			now, now, id,
		)
	}
	if err != nil {
		return fmt.Errorf("store.DeleteObservation: %w", err)
	}
	return nil
}

// Search queries observations_fts using BM25 ranking.
// Soft-deleted observations are excluded. Returns empty slice (not error) on no matches.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	sanitized := sanitizeFTS(params.Q)
	if sanitized == "" {
		return nil, nil
	}

	query := `
		SELECT o.id, o.sync_id, o.session_id, o.type, o.title, o.content, o.tool_name,
		       o.project, o.scope, o.topic_key, o.normalized_hash,
		       o.revision_count, o.duplicate_count, o.last_seen_at,
		       o.created_at, o.updated_at, o.deleted_at,
		       bm25(observations_fts) AS score
		FROM observations_fts
		JOIN observations o ON o.id = observations_fts.rowid
		WHERE observations_fts MATCH ?
		  AND o.deleted_at IS NULL`
	args := []interface{}{sanitized}

	if params.Type != "" {
		query += " AND o.type=?"
		args = append(args, params.Type)
	}
	if params.Project != "" {
		query += " AND o.project=?"
		args = append(args, params.Project)
	}
	if params.Scope != "" {
		query += " AND o.scope=?"
		args = append(args, params.Scope)
	}
	query += " ORDER BY score LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.Search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var o Observation
		var toolName, topicKey, deletedAt sql.NullString
		var score float64
		if err := rows.Scan(
			&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content, &toolName,
			&o.Project, &o.Scope, &topicKey, &o.NormalizedHash,
			&o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
			&o.CreatedAt, &o.UpdatedAt, &deletedAt,
			&score,
		); err != nil {
			return nil, fmt.Errorf("store.Search scan: %w", err)
		}
		if toolName.Valid {
			o.ToolName = &toolName.String
		}
		if topicKey.Valid {
			o.TopicKey = &topicKey.String
		}
		if deletedAt.Valid {
			o.DeletedAt = &deletedAt.String
		}
		results = append(results, SearchResult{Observation: o, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.Search: %w", err)
	}
	return results, nil
}

// scanObservation scans a single observation from a *sql.Row.
func scanObservation(row *sql.Row) (Observation, error) {
	var o Observation
	var toolName, topicKey, deletedAt sql.NullString
	err := row.Scan(
		&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content, &toolName,
		&o.Project, &o.Scope, &topicKey, &o.NormalizedHash,
		&o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
		&o.CreatedAt, &o.UpdatedAt, &deletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, ErrObservationNotFound
	}
	if err != nil {
		return Observation{}, fmt.Errorf("store.scanObservation: %w", err)
	}
	if toolName.Valid {
		o.ToolName = &toolName.String
	}
	if topicKey.Valid {
		o.TopicKey = &topicKey.String
	}
	if deletedAt.Valid {
		o.DeletedAt = &deletedAt.String
	}
	return o, nil
}

// scanObservationRow scans an observation from *sql.Rows.
func scanObservationRow(rows *sql.Rows) (Observation, error) {
	var o Observation
	var toolName, topicKey, deletedAt sql.NullString
	if err := rows.Scan(
		&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content, &toolName,
		&o.Project, &o.Scope, &topicKey, &o.NormalizedHash,
		&o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
		&o.CreatedAt, &o.UpdatedAt, &deletedAt,
	); err != nil {
		return Observation{}, fmt.Errorf("store.scanObservationRow: %w", err)
	}
	if toolName.Valid {
		o.ToolName = &toolName.String
	}
	if topicKey.Valid {
		o.TopicKey = &topicKey.String
	}
	if deletedAt.Valid {
		o.DeletedAt = &deletedAt.String
	}
	return o, nil
}
