package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// revisionKeepCount is the maximum number of revisions retained per observation.
// Older revisions beyond this cap are pruned in the same transaction as the capture.
// A settings-based override can be added later.
const revisionKeepCount = 10

// Revision is a historical snapshot of an observation captured immediately before
// a destructive overwrite (topic-key upsert or content/title/type-changing UpdateObservation).
type Revision struct {
	ID            int64
	ObservationID int64
	Revision      int
	Type          string
	Title         string
	Content       string
	ToolName      *string
	CreatedAt     string // when the observation was last written (before overwrite)
	ArchivedAt    string // when the overwrite occurred
}

// ListRevisions returns the revision history for the given observation, newest
// archived_at first. Returns an empty (non-nil) slice when there are no revisions.
// Returns ErrObservationNotFound when the observation does not exist.
func (s *Store) ListRevisions(ctx context.Context, observationID int64) ([]Revision, error) {
	// Verify the observation exists (including soft-deleted rows is fine — the FK
	// can reference rows regardless of deleted_at).
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE id=?", observationID,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("store.ListRevisions existence check: %w", err)
	}
	if count == 0 {
		return nil, ErrObservationNotFound
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, observation_id, revision, type, title, content, tool_name, created_at, archived_at
		FROM observation_revisions
		WHERE observation_id=?
		ORDER BY archived_at DESC, id DESC`,
		observationID,
	)
	if err != nil {
		return nil, fmt.Errorf("store.ListRevisions query: %w", err)
	}
	defer rows.Close()

	revisions := []Revision{} // non-nil empty slice so JSON encodes as []
	for rows.Next() {
		var r Revision
		var toolName sql.NullString
		if err := rows.Scan(
			&r.ID, &r.ObservationID, &r.Revision,
			&r.Type, &r.Title, &r.Content, &toolName,
			&r.CreatedAt, &r.ArchivedAt,
		); err != nil {
			return nil, fmt.Errorf("store.ListRevisions scan: %w", err)
		}
		if toolName.Valid {
			r.ToolName = &toolName.String
		}
		revisions = append(revisions, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ListRevisions: %w", err)
	}
	return revisions, nil
}

// captureRevision inserts a BEFORE-image of the given observation into
// observation_revisions within the provided transaction. It then prunes old
// revisions for that observation, keeping only the newest revisionKeepCount.
// captureRevision is called only when content, title, or type actually changes.
func captureRevision(ctx context.Context, tx *sql.Tx, obs Observation, archivedAt string) error {
	var toolNameArg interface{}
	if obs.ToolName != nil {
		toolNameArg = *obs.ToolName
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO observation_revisions
		    (observation_id, revision, type, title, content, tool_name, created_at, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		obs.ID, obs.RevisionCount,
		obs.Type, obs.Title, obs.Content, toolNameArg,
		obs.UpdatedAt, // updated_at before the overwrite = when this version was last written
		archivedAt,
	)
	if err != nil {
		return fmt.Errorf("store.captureRevision insert: %w", err)
	}

	// Prune: keep newest revisionKeepCount revisions; delete the rest.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM observation_revisions
		WHERE observation_id=?
		  AND id NOT IN (
		      SELECT id FROM observation_revisions
		      WHERE observation_id=?
		      ORDER BY archived_at DESC, id DESC
		      LIMIT ?
		  )`,
		obs.ID, obs.ID, revisionKeepCount,
	)
	if err != nil {
		return fmt.Errorf("store.captureRevision prune: %w", err)
	}
	return nil
}

// readCurrentObservation fetches the current (non-deleted) observation row by id
// using the provided transaction. Returns ErrObservationNotFound if absent.
func readCurrentObservation(ctx context.Context, tx *sql.Tx, id int64) (Observation, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, sync_id, session_id, type, title, content, tool_name,
		       project, scope, topic_key, normalized_hash,
		       revision_count, duplicate_count, last_seen_at,
		       created_at, updated_at, deleted_at
		FROM observations
		WHERE id=? AND deleted_at IS NULL`,
		id,
	)
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
		return Observation{}, fmt.Errorf("store.readCurrentObservation: %w", err)
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
