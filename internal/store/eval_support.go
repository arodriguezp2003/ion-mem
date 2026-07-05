package store

import (
	"context"
	"fmt"
	"time"
)

// BackdateObservation sets last_seen_at and created_at on the observation
// identified by id to approximately (now - days * 24h). It is intended for
// test-support use only: the eval harness needs to simulate aging so that the
// recency decay in SearchWithFallback applies realistically to corpus fixtures.
//
// Returns ErrObservationNotFound when id does not exist or is soft-deleted.
func (s *Store) BackdateObservation(ctx context.Context, id int64, days int) error {
	// Verify existence.
	var exists int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE id=? AND deleted_at IS NULL", id,
	).Scan(&exists); err != nil {
		return fmt.Errorf("store.BackdateObservation existence check: %w", err)
	}
	if exists == 0 {
		return ErrObservationNotFound
	}

	backdated := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		"UPDATE observations SET last_seen_at=?, created_at=?, updated_at=? WHERE id=?",
		backdated, backdated, backdated, id,
	)
	if err != nil {
		return fmt.Errorf("store.BackdateObservation update: %w", err)
	}
	return nil
}
