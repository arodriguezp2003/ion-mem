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
