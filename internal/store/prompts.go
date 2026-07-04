package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Prompt represents a row in the user_prompts table.
type Prompt struct {
	ID         int64
	SyncID     string
	SessionID  string
	Content    string
	Project    string
	CreatedAt  string
	ConsumedAt sql.NullString
}

// AddPromptParams carries the caller-supplied fields for a new prompt.
type AddPromptParams struct {
	SessionID string
	Content   string
	Project   string
}

// SearchPromptsParams carries filters for SearchPrompts.
type SearchPromptsParams struct {
	Q       string // FTS5 query (required)
	Project string // optional filter
	Limit   int    // default 20 when <= 0
}

// AddPromptIfMissing inserts a new prompt or returns the existing one.
// Deduplication is keyed on SHA-256 of (content + session_id).
// sync_id prefix is "pr-".
func (s *Store) AddPromptIfMissing(ctx context.Context, params AddPromptParams) (Prompt, error) {
	// Probe for existing row matching (session_id, content).
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM user_prompts WHERE session_id=? AND content=? LIMIT 1`,
		params.SessionID, params.Content,
	).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Prompt{}, fmt.Errorf("store.AddPromptIfMissing probe: %w", err)
	}
	if err == nil {
		// Existing row found — return it.
		return s.getPromptByID(ctx, id)
	}

	// Insert new row.
	syncID := generateSyncID("pr-")
	now := nowISO()

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO user_prompts (sync_id, session_id, content, project, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		syncID, params.SessionID, params.Content, params.Project, now,
	)
	if err != nil {
		return Prompt{}, fmt.Errorf("store.AddPromptIfMissing insert: %w", err)
	}

	newID, err := res.LastInsertId()
	if err != nil {
		return Prompt{}, fmt.Errorf("store.AddPromptIfMissing LastInsertId: %w", err)
	}

	return s.getPromptByID(ctx, newID)
}

// getPromptByID fetches a single prompt row by primary key.
func (s *Store) getPromptByID(ctx context.Context, id int64) (Prompt, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, sync_id, session_id, content, project, created_at, consumed_at
		 FROM user_prompts WHERE id=?`,
		id,
	)
	return scanPrompt(row)
}

// RecentPrompts returns prompts ordered by created_at DESC. When project is
// empty it returns prompts from all projects. When limit <= 0 it defaults to 50.
func (s *Store) RecentPrompts(ctx context.Context, project string, limit int) ([]Prompt, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		query string
		args  []interface{}
	)
	if project == "" {
		query = `SELECT id, sync_id, session_id, content, project, created_at, consumed_at
		          FROM user_prompts ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{limit}
	} else {
		query = `SELECT id, sync_id, session_id, content, project, created_at, consumed_at
		          FROM user_prompts WHERE project=? ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{project, limit}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.RecentPrompts: %w", err)
	}
	defer rows.Close()

	var prompts []Prompt
	for rows.Next() {
		p, err := scanPromptRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store.RecentPrompts scan: %w", err)
		}
		prompts = append(prompts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.RecentPrompts: %w", err)
	}
	return prompts, nil
}

// SearchPrompts queries prompts_fts using BM25 ranking.
// Returns empty slice (not error) on no matches.
func (s *Store) SearchPrompts(ctx context.Context, params SearchPromptsParams) ([]Prompt, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	sanitized := sanitizeFTS(params.Q)
	if sanitized == "" {
		return nil, nil
	}

	query := `
		SELECT p.id, p.sync_id, p.session_id, p.content, p.project, p.created_at, p.consumed_at
		FROM prompts_fts
		JOIN user_prompts p ON p.id = prompts_fts.rowid
		WHERE prompts_fts MATCH ?`
	args := []interface{}{sanitized}

	if params.Project != "" {
		query += " AND p.project=?"
		args = append(args, params.Project)
	}
	query += " ORDER BY bm25(prompts_fts) LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.SearchPrompts: %w", err)
	}
	defer rows.Close()

	var prompts []Prompt
	for rows.Next() {
		p, err := scanPromptRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store.SearchPrompts scan: %w", err)
		}
		prompts = append(prompts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.SearchPrompts: %w", err)
	}
	return prompts, nil
}

// DeletePrompt removes the prompt row (hard delete only).
// The FTS entry is removed via the delete trigger.
// Returns ErrPromptNotFound when id does not exist.
func (s *Store) DeletePrompt(ctx context.Context, id int64) error {
	// Check existence.
	var count int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_prompts WHERE id=?", id,
	).Scan(&count); err != nil {
		return fmt.Errorf("store.DeletePrompt existence check: %w", err)
	}
	if count == 0 {
		return ErrPromptNotFound
	}

	if _, err := s.db.ExecContext(ctx, "DELETE FROM user_prompts WHERE id=?", id); err != nil {
		return fmt.Errorf("store.DeletePrompt: %w", err)
	}
	return nil
}

// scanPrompt scans a single prompt from a *sql.Row.
func scanPrompt(row *sql.Row) (Prompt, error) {
	var p Prompt
	err := row.Scan(&p.ID, &p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt, &p.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Prompt{}, ErrPromptNotFound
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("store.scanPrompt: %w", err)
	}
	return p, nil
}

// scanPromptRow scans a prompt from *sql.Rows.
func scanPromptRow(rows *sql.Rows) (Prompt, error) {
	var p Prompt
	if err := rows.Scan(&p.ID, &p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt, &p.ConsumedAt); err != nil {
		return Prompt{}, fmt.Errorf("store.scanPromptRow: %w", err)
	}
	return p, nil
}

// ConsumeLatestPrompt atomically fetches and marks consumed the latest
// unconsumed user_prompts row for sessionID. Within a single transaction it
// SELECTs the row with consumed_at IS NULL (ORDER BY created_at DESC, id DESC
// LIMIT 1) and, when found, UPDATEs consumed_at to the current UTC time.
//
// Returns (row, true, nil) when a row is found and consumed.
// Returns (Prompt{}, false, nil) when no unconsumed row exists.
// Returns (Prompt{}, false, err) on database errors.
func (s *Store) ConsumeLatestPrompt(ctx context.Context, sessionID string) (Prompt, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Prompt{}, false, fmt.Errorf("store.ConsumeLatestPrompt begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var p Prompt
	err = tx.QueryRowContext(ctx,
		`SELECT id, sync_id, session_id, content, project, created_at, consumed_at
		 FROM user_prompts
		 WHERE session_id = ? AND consumed_at IS NULL
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		sessionID,
	).Scan(&p.ID, &p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt, &p.ConsumedAt)

	if errors.Is(err, sql.ErrNoRows) {
		// No unconsumed row — commit the (read-only) transaction and return not-found.
		if commitErr := tx.Commit(); commitErr != nil {
			return Prompt{}, false, fmt.Errorf("store.ConsumeLatestPrompt commit (not found): %w", commitErr)
		}
		return Prompt{}, false, nil
	}
	if err != nil {
		return Prompt{}, false, fmt.Errorf("store.ConsumeLatestPrompt select: %w", err)
	}

	// Mark the row consumed.
	if _, err := tx.ExecContext(ctx,
		`UPDATE user_prompts SET consumed_at = datetime('now') WHERE id = ?`,
		p.ID,
	); err != nil {
		return Prompt{}, false, fmt.Errorf("store.ConsumeLatestPrompt update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Prompt{}, false, fmt.Errorf("store.ConsumeLatestPrompt commit: %w", err)
	}

	return p, true, nil
}
