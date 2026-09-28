package store

// ValidObservationTypes is the closed vocabulary of accepted observation type
// values. Any non-empty type supplied to ion_save or ion_update that is not in
// this set is rejected with an invalid_argument error.
//
// "manual" is the default; empty type strings default to "manual" without error.
var ValidObservationTypes = map[string]struct{}{
	"decision":        {},
	"architecture":    {},
	"bugfix":          {},
	"discovery":       {},
	"config":          {},
	"preference":      {},
	"pattern":         {},
	"session_summary": {},
	"manual":          {},
}

// IsValidObservationType reports whether typ is in the closed vocabulary.
// Empty strings return false — callers that want to accept empty as "manual"
// must check len(typ) == 0 before calling this function.
func IsValidObservationType(typ string) bool {
	_, ok := ValidObservationTypes[typ]
	return ok
}

// ─── observation status / lifecycle ──────────────────────────────────────────

// Status values for observations.status. Memory hygiene means changing
// state, not deleting: a superseded decision is history worth keeping, so
// only "obsolete" ever leaves a row out of normal recall (and even then the
// row survives until an explicit soft/hard delete — status alone never
// deletes anything).
const (
	StatusActive     = "active"
	StatusSuperseded = "superseded"
	StatusObsolete   = "obsolete"
)

// ValidObservationStatuses is the closed vocabulary accepted by
// SetObservationStatus.
var ValidObservationStatuses = map[string]struct{}{
	StatusActive:     {},
	StatusSuperseded: {},
	StatusObsolete:   {},
}

// IsValidObservationStatus reports whether status is in the closed vocabulary.
func IsValidObservationStatus(status string) bool {
	_, ok := ValidObservationStatuses[status]
	return ok
}

// permanentObservationTypes are the types whose observations are the
// project's durable, permanent learning record. Bug fixes and root-cause
// discoveries must never be curated or pruned away — see Permanent,
// SetObservationStatus, and Store.PruneDeletedObs.
var permanentObservationTypes = map[string]struct{}{
	"bugfix":    {},
	"discovery": {},
}

// Permanent reports whether typ is a permanent observation type (bugfix or
// discovery). Permanent observations can never move to status "obsolete" and
// can only move to "superseded" when superseded_by also points at a
// permanent-type observation (a newer fix/discovery replacing an older one) —
// see SetObservationStatus. PruneDeletedObs also refuses to hard-delete
// permanent-type rows even after a human soft-deletes them.
func Permanent(typ string) bool {
	_, ok := permanentObservationTypes[typ]
	return ok
}
