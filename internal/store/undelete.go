package store

import (
	"context"
	"fmt"
)

// UndeleteObservation clears deleted_at on a soft-deleted observation, making it
// visible to search and retrieval again. The FTS update trigger re-indexes the row
// automatically on the UPDATE (the obs_fts_update trigger fires and re-inserts the
// FTS row), so Search will find it after this call.
//
// Returns ErrObservationNotFound when:
//   - the row does not exist (never inserted or hard-deleted), OR
//   - the row exists but is NOT soft-deleted (deleted_at IS NULL).
func (s *Store) UndeleteObservation(ctx context.Context, id int64) error {
	now := nowISO()
	res, err := s.db.ExecContext(ctx,
		"UPDATE observations SET deleted_at=NULL, updated_at=? WHERE id=? AND deleted_at IS NOT NULL",
		now, id,
	)
	if err != nil {
		return fmt.Errorf("store.UndeleteObservation: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store.UndeleteObservation rows affected: %w", err)
	}
	if rows == 0 {
		return ErrObservationNotFound
	}
	return nil
}
