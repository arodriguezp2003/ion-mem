package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session represents a row in the sessions table.
type Session struct {
	ID        string
	Project   string
	Directory string
	StartedAt time.Time // stored as RFC3339Nano in SQLite
	EndedAt   *string   // nil when session is still active
	Summary   *string   // nil when no summary has been set
	Status    string    // "active" or "ended"
}

// CreateSessionParams carries the caller-supplied fields for a new session.
type CreateSessionParams struct {
	ID        string
	Project   string
	Directory string
}

// CreateSession inserts a new session row. The StartedAt timestamp is set to
// the current UTC time and Status is "active". Returns an error if the ID
// already exists (primary key conflict).
func (s *Store) CreateSession(ctx context.Context, params CreateSessionParams) (Session, error) {
	now := nowISO()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, project, directory, started_at, status)
		 VALUES (?, ?, ?, ?, 'active')`,
		params.ID, params.Project, params.Directory, now,
	)
	if err != nil {
		return Session{}, fmt.Errorf("store.CreateSession: %w", err)
	}
	t, _ := parseISO(now)
	return Session{
		ID:        params.ID,
		Project:   params.Project,
		Directory: params.Directory,
		StartedAt: t,
		Status:    "active",
	}, nil
}

// GetSession returns the session with the given ID, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, sessionID string) (Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project, directory, started_at, ended_at, summary, status
		 FROM sessions WHERE id=?`,
		sessionID,
	)
	return scanSession(row)
}

// EndSession sets ended_at, status="ended", and summary on the matching row.
// It is idempotent (last-write-wins). Returns ErrNotFound if the session
// does not exist.
func (s *Store) EndSession(ctx context.Context, sessionID, summary string) error {
	now := nowISO()
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at=?, status='ended', summary=? WHERE id=?`,
		now, summary, sessionID,
	)
	if err != nil {
		return fmt.Errorf("store.EndSession: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store.EndSession: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecentSessions returns sessions ordered by started_at DESC. When project is
// empty it returns sessions from all projects. When limit <= 0 it defaults to 50.
func (s *Store) RecentSessions(ctx context.Context, project string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		query string
		args  []interface{}
	)
	if project == "" {
		query = `SELECT id, project, directory, started_at, ended_at, summary, status
		          FROM sessions ORDER BY started_at DESC LIMIT ?`
		args = []interface{}{limit}
	} else {
		query = `SELECT id, project, directory, started_at, ended_at, summary, status
		          FROM sessions WHERE project=? ORDER BY started_at DESC LIMIT ?`
		args = []interface{}{project, limit}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.RecentSessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		sess, err := scanSessionRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store.RecentSessions scan: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.RecentSessions: %w", err)
	}
	return sessions, nil
}

// DeleteSession removes the session row. Returns ErrNotFound when the session
// does not exist. Returns ErrSessionHasObservations when a FK constraint
// prevents deletion (child observations or prompts exist).
func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	// Check existence first so we can distinguish not-found from FK violation.
	var exists int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sessions WHERE id=?", sessionID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("store.DeleteSession: existence check: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id=?", sessionID)
	if err != nil {
		// Map SQLite FK constraint errors to the sentinel.
		if isForeignKeyError(err) {
			return ErrSessionHasObservations
		}
		return fmt.Errorf("store.DeleteSession: %w", err)
	}
	return nil
}

// scanSession scans a single session row from a *sql.Row.
func scanSession(row *sql.Row) (Session, error) {
	var sess Session
	var startedAt string
	var endedAt, summary sql.NullString
	err := row.Scan(&sess.ID, &sess.Project, &sess.Directory,
		&startedAt, &endedAt, &summary, &sess.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("store.scanSession: %w", err)
	}
	t, parseErr := parseISO(startedAt)
	if parseErr != nil {
		// Fallback: try without nanoseconds
		t, parseErr = time.Parse(time.RFC3339, startedAt)
		if parseErr != nil {
			return Session{}, fmt.Errorf("store.scanSession: bad started_at %q: %w", startedAt, parseErr)
		}
	}
	sess.StartedAt = t
	if endedAt.Valid {
		sess.EndedAt = &endedAt.String
	}
	if summary.Valid {
		sess.Summary = &summary.String
	}
	return sess, nil
}

// scanSessionRow scans a session from *sql.Rows.
func scanSessionRow(rows *sql.Rows) (Session, error) {
	var sess Session
	var startedAt string
	var endedAt, summary sql.NullString
	if err := rows.Scan(&sess.ID, &sess.Project, &sess.Directory,
		&startedAt, &endedAt, &summary, &sess.Status); err != nil {
		return Session{}, err
	}
	t, err := parseISO(startedAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, startedAt)
		if err != nil {
			return Session{}, fmt.Errorf("store.scanSessionRow: bad started_at %q: %w", startedAt, err)
		}
	}
	sess.StartedAt = t
	if endedAt.Valid {
		sess.EndedAt = &endedAt.String
	}
	if summary.Valid {
		sess.Summary = &summary.String
	}
	return sess, nil
}

// EndStaleSessions ends all sessions with status=active whose started_at is
// strictly before the cutoff (an RFC3339 string). Returns the number of sessions
// ended. Sessions that are already ended are not touched.
func (s *Store) EndStaleSessions(ctx context.Context, cutoff string) (int64, error) {
	now := nowISO()
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at=?, status='ended'
		 WHERE status='active' AND started_at < ?`,
		now, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("store.EndStaleSessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store.EndStaleSessions rows affected: %w", err)
	}
	return n, nil
}

// isForeignKeyError returns true if err is a SQLite FOREIGN KEY constraint error.
func isForeignKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FOREIGN KEY constraint failed") ||
		strings.Contains(msg, "foreign key constraint")
}
