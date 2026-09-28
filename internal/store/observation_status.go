package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// maxSupersededByChainHops bounds detectSupersededByCycle's walk so a
// malformed or adversarial chain can never turn a status change into an
// unbounded loop or an expensive scan.
const maxSupersededByChainHops = 50

// detectSupersededByCycle walks the superseded_by chain starting at target
// (the observation the caller wants id to be superseded by), following each
// hop's superseded_by pointer up to maxSupersededByChainHops times. If the
// chain ever loops back to id, marking id as superseded by target would
// create a cycle (A superseded_by B superseded_by ... superseded_by A), so
// it returns an error instead of silently corrupting the lineage graph.
// A dangling pointer partway up the chain (the referenced observation no
// longer exists) simply stops the walk — that's a pre-existing state this
// call didn't create, not a cycle.
//
// In steady state this never actually walks more than zero hops: the
// superseded_by target must be status=active (see SetObservationStatus),
// and an active row always has a nil superseded_by (StatusActive clears it).
// It stays in place as defense-in-depth against rows that predate that rule
// or were written outside this API (direct SQL, a restored backup, etc.).
func detectSupersededByCycle(ctx context.Context, tx *sql.Tx, id int64, target Observation) error {
	cursor := target
	for hops := 0; hops < maxSupersededByChainHops; hops++ {
		if cursor.SupersededBy == nil {
			return nil
		}
		next := *cursor.SupersededBy
		if next == id {
			return fmt.Errorf(
				"store.SetObservationStatus: superseded_by chain from observation %d would create a cycle back to observation %d",
				target.ID, id)
		}
		nextObs, err := readCurrentObservation(ctx, tx, next)
		if err != nil {
			return nil // dangling pointer further up the chain; not this call's problem
		}
		cursor = nextObs
	}
	return nil
}

// validateStatusTransition applies the exact invariant checks
// SetObservationStatus enforces to a status change on current, without
// writing anything to the database. supersededBy is the LOCAL id of the
// intended superseded_by target (already resolved by the caller — e.g. from
// a sync_id lookup when called from ImportProject); only meaningful for
// status=StatusSuperseded.
//
// Returns (target, nil) when the transition is allowed — target is the
// resolved superseded_by row for StatusSuperseded, nil for every other
// status — or (nil, err) describing why it is rejected. Extracted from
// SetObservationStatus so ImportProject can apply the SAME rules to a
// bundled status inside its own transaction, rather than writing
// status/superseded_by directly and bypassing them.
func validateStatusTransition(ctx context.Context, tx *sql.Tx, current Observation, status string, supersededBy *int64) (*Observation, error) {
	if !IsValidObservationStatus(status) {
		return nil, fmt.Errorf("invalid status %q (want %q, %q, or %q)",
			status, StatusActive, StatusSuperseded, StatusObsolete)
	}

	switch status {
	case StatusActive:
		return nil, nil

	case StatusSuperseded:
		if supersededBy == nil {
			return nil, fmt.Errorf("status=%s requires supersededBy", StatusSuperseded)
		}
		if *supersededBy == current.ID {
			return nil, fmt.Errorf("observation %d cannot supersede itself", current.ID)
		}
		target, err := readCurrentObservation(ctx, tx, *supersededBy)
		if err != nil {
			if errors.Is(err, ErrObservationNotFound) {
				return nil, fmt.Errorf("superseded_by observation %d not found or deleted", *supersededBy)
			}
			return nil, err
		}
		if target.Status != StatusActive {
			return nil, fmt.Errorf("superseded_by target #%d is %s; point at the newest active row",
				*supersededBy, target.Status)
		}
		if target.Project != current.Project {
			return nil, fmt.Errorf("superseded_by observation %d is in project %q, want %q",
				*supersededBy, target.Project, current.Project)
		}
		if Permanent(current.Type) && !Permanent(target.Type) {
			return nil, fmt.Errorf(
				"%s observations are permanent; superseded_by must also be a permanent type (bugfix or discovery), got %q",
				current.Type, target.Type)
		}
		if err := detectSupersededByCycle(ctx, tx, current.ID, target); err != nil {
			return nil, err
		}
		return &target, nil

	case StatusObsolete:
		if Permanent(current.Type) {
			return nil, fmt.Errorf(
				"%s observations are permanent; use status=%s with a superseded_by pointer if a newer fix replaces it, not %s",
				current.Type, StatusSuperseded, StatusObsolete)
		}
		return nil, nil
	}
	return nil, nil
}

// SetObservationStatus transitions the observation with the given id to
// status (one of StatusActive, StatusSuperseded, StatusObsolete).
//
// Memory hygiene means changing state, not deleting: this is the only
// supported way to mark a decision superseded or an observation obsolete —
// there is no bulk curation delete path. superseded_by/status_reason are
// only meaningful for a non-active status:
//   - StatusSuperseded requires supersededBy to point at a different,
//     non-deleted, status=active observation in the SAME project. A
//     superseded/obsolete target is a dead pointer — the lineage chain must
//     always end at the newest active row, so pointing at anything else is
//     refused with the target's actual status in the error.
//   - StatusActive clears any prior supersededBy/reason (moving an
//     observation back to active un-supersedes it).
//   - StatusObsolete is refused for permanent types (see Permanent):
//     bugfix and discovery observations are the durable learning record and
//     must never be marked obsolete. They CAN be marked superseded, but only
//     when supersededBy also points at a permanent-type observation — i.e. a
//     newer fix/discovery replacing an older one, never a demotion to a
//     lesser-permanence type.
//
// Design note on revision history: SetObservationStatus deliberately does
// NOT write a row to observation_revisions. That table snapshots
// type/title/content/tool_name immediately before a destructive overwrite so
// the previous text can be recovered; a status transition changes none of
// those fields; the well-defined and cheap alternative that reuses TDD is
// stamping status_changed_at/status_reason on the observation itself, which
// ion_get_observation and ion_history's observation summary both surface —
// full lineage without a duplicate, content-free revision row.
func (s *Store) SetObservationStatus(ctx context.Context, id int64, status string, supersededBy *int64, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store.SetObservationStatus begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	current, err := readCurrentObservation(ctx, tx, id)
	if err != nil {
		return err // ErrObservationNotFound propagates correctly
	}

	target, err := validateStatusTransition(ctx, tx, current, status, supersededBy)
	if err != nil {
		return fmt.Errorf("store.SetObservationStatus: %w", err)
	}

	var supersededByArg interface{}
	var reasonArg interface{}
	switch status {
	case StatusSuperseded:
		supersededByArg = target.ID
		if reason != "" {
			reasonArg = reason
		}
	case StatusObsolete:
		if reason != "" {
			reasonArg = reason
		}
	}

	now := nowISO()
	_, err = tx.ExecContext(ctx, `
		UPDATE observations
		SET status=?, superseded_by=?, status_reason=?, status_changed_at=?, updated_at=?
		WHERE id=?`,
		status, supersededByArg, reasonArg, now, now, id,
	)
	if err != nil {
		return fmt.Errorf("store.SetObservationStatus update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store.SetObservationStatus commit: %w", err)
	}
	return nil
}

// ListObservationsByStatus returns up to limit non-deleted observations in
// project with the given status, ordered by status_changed_at DESC (falling
// back to updated_at for rows whose status has never changed). Used by the
// curation skill and the TUI status filter. limit<=0 defaults to 50.
func (s *Store) ListObservationsByStatus(ctx context.Context, project, status string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+observationColumns+`
		FROM observations
		WHERE deleted_at IS NULL AND project=? AND status=?
		ORDER BY COALESCE(status_changed_at, updated_at) DESC
		LIMIT ?`,
		project, status, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store.ListObservationsByStatus: %w", err)
	}
	defer rows.Close()

	var out []Observation
	for rows.Next() {
		o, err := scanObservationRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store.ListObservationsByStatus scan: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ListObservationsByStatus rows: %w", err)
	}
	return out, nil
}
