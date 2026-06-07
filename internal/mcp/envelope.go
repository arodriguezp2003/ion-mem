package mcp

import (
	"encoding/json"

	"github.com/ionix/ion-mem/internal/project"
)

// Build constructs the canonical JSON envelope for every MCP tool response
// except ion_current_project. It is the SOLE entry point for response JSON —
// handlers MUST NOT call json.Marshal directly.
//
// The returned JSON always contains the four standard fields:
//
//	project, project_source, project_path, result
//
// Extension fields from extras are merged at the top level (not nested under "data").
// Passing nil extras is valid and results in exactly four top-level fields.
func Build(det project.DetectionResult, result string, extras map[string]any) []byte {
	// Start with the four required fields.
	m := map[string]any{
		"project":        det.Project,
		"project_source": det.Source,
		"project_path":   det.Path,
		"result":         result,
	}
	// Merge extension fields at top level.
	for k, v := range extras {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}
