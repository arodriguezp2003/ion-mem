package store

import (
	"context"
	"fmt"
	"time"
)

// BackdateObservation sets last_seen_at and created_at on the observation
// identified by id to approximately (now - days * 24h). It exists solely for
// eval-corpus seeding (`ion-mem eval --corpus` and the eval regression tests),
// which seed a TEMPORARY store and need to simulate aging so recency decay
// applies realistically. Never call it against a real user store.
//
// Returns ErrObservationNotFound when id does not exist or is soft-deleted.
func (s *Store) BackdateObservation(ctx context.Context, id int64, days int) error {
	if days < 0 {
		return fmt.Errorf("store.BackdateObservation: days must be >= 0, got %d", days)
	}
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
