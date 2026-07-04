package mcp

import (
	"encoding/json"
	"errors"

	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// Closed vocabulary for the error_code field in error envelopes.
const (
	CodeNotFound        = "not_found"
	CodeDBError         = "db_error"
	CodeInvalidArgument = "invalid_argument"
	CodeProjectAmbiguous = "project_ambiguous"
	CodeInternal        = "internal"
)

// Build constructs the canonical JSON envelope for every MCP tool response
// except ion_current_project. It is the SOLE entry point for SUCCESS response JSON —
// handlers MUST NOT call json.Marshal directly. For error responses use BuildError.
//
// The returned JSON always contains the five standard fields:
//
//	project, project_source, project_path, result, status ("ok")
//
// Extension fields from extras are merged at the top level (not nested under "data").
// Passing nil extras is valid and results in exactly five top-level fields.
func Build(det project.DetectionResult, result string, extras map[string]any) []byte {
	// Start with the five required fields; status is always "ok" for success.
	m := map[string]any{
		"project":        det.Project,
		"project_source": det.Source,
		"project_path":   det.Path,
		"result":         result,
		"status":         "ok",
	}
	// Merge extension fields at top level.
	for k, v := range extras {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// BuildError constructs a canonical JSON error envelope. It sets status:"error",
// error_code to a closed-vocabulary value, and result to the human-readable msg.
// The standard project fields are always included.
func BuildError(det project.DetectionResult, code, msg string) []byte {
	m := map[string]any{
		"project":        det.Project,
		"project_source": det.Source,
		"project_path":   det.Path,
		"result":         msg,
		"status":         "error",
		"error_code":     code,
	}
	b, _ := json.Marshal(m)
	return b
}

// errorCode maps a store-layer error to a closed-vocabulary error_code string.
// It uses errors.Is to match sentinel values so wrapping is handled transparently.
func errorCode(err error) string {
	if errors.Is(err, store.ErrObservationNotFound) ||
		errors.Is(err, store.ErrNotFound) ||
		errors.Is(err, store.ErrPromptNotFound) {
		return CodeNotFound
	}
	return CodeDBError
}
