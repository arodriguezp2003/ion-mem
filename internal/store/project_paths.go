package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

// normalizeDir canonicalises a directory path for consistent lookup:
// filepath.Clean removes trailing slashes and resolves "." / ".." segments.
// The result is the canonical form stored in or compared against sessions.directory.
func normalizeDir(dir string) string {
	return filepath.Clean(dir)
}

// ProjectForDirectory returns the project name most recently associated with
// the given directory by looking up sessions whose directory column matches
// (after normalisation). The second return value is false when no session
// has ever been recorded for that directory.
//
// "Most recent" is defined by started_at DESC — the project used in the latest
// session in that directory wins.
func (s *Store) ProjectForDirectory(ctx context.Context, dir string) (string, bool, error) {
	dir = normalizeDir(dir)
	var project string
	err := s.db.QueryRowContext(ctx,
		`SELECT project FROM sessions
		 WHERE directory = ?
		 ORDER BY started_at DESC
		 LIMIT 1`,
		dir,
	).Scan(&project)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store.ProjectForDirectory: %w", err)
	}
	return project, true, nil
}

// ProjectDirectories returns the distinct directories associated with the given
// project, ordered by the most recent session started in each directory
// (most-recently-used first). The result is empty (not nil) when no sessions
// exist for the project.
func (s *Store) ProjectDirectories(ctx context.Context, project string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT directory, MAX(started_at) AS last_used
		 FROM sessions
		 WHERE project = ?
		 GROUP BY directory
		 ORDER BY last_used DESC`,
		project,
	)
	if err != nil {
		return nil, fmt.Errorf("store.ProjectDirectories: %w", err)
	}
	defer rows.Close()

	var dirs []string
	for rows.Next() {
		var dir, lastUsed string
		if err := rows.Scan(&dir, &lastUsed); err != nil {
			return nil, fmt.Errorf("store.ProjectDirectories scan: %w", err)
		}
		dirs = append(dirs, dir)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ProjectDirectories: %w", err)
	}
	if dirs == nil {
		dirs = []string{}
	}
	return dirs, nil
}
