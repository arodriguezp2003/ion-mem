package project

import (
	"errors"
	"fmt"
)

// ErrAmbiguousProject is returned when the working directory is a parent of
// multiple git repositories and the package cannot auto-select one. Use
// errors.Is to check for this sentinel.
var ErrAmbiguousProject = errors.New("project: ambiguous — multiple git repositories found")

// wrap returns a wrapped error in the standard "project: <op>: <err>" form.
func wrap(op string, err error) error {
	return fmt.Errorf("project: %s: %w", op, err)
}
