package store

import "errors"

// Sentinel errors returned by store operations.
// All are compatible with errors.Is.
var (
	// ErrNotFound is returned when a session lookup yields no row.
	ErrNotFound = errors.New("store: not found")

	// ErrSessionHasObservations is returned by DeleteSession when at least one
	// child observation or prompt FK-references the session.
	ErrSessionHasObservations = errors.New("store: session has observations")

	// ErrObservationNotFound is returned when an observation lookup yields no row.
	ErrObservationNotFound = errors.New("store: observation not found")

	// ErrPromptNotFound is returned when a prompt lookup yields no row.
	ErrPromptNotFound = errors.New("store: prompt not found")

	// ErrMigrationFailed is returned when the migration runner encounters an
	// unrecoverable schema error.
	ErrMigrationFailed = errors.New("store: migration failed")
)
