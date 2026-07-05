package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ionix/ion-mem/internal/project"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildCurrentProjectTool constructs the ion_current_project ServerTool.
//
// ion_current_project is the SOLE exception to the envelope rule: it returns
// a DetectionResult directly (no project/project_source/project_path/result wrapper).
// Ambiguity surfaces as error:"project_ambiguous" + available_projects in the body.
// It NEVER returns a Go error.
func buildCurrentProjectTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_current_project",
		mcplib.WithDescription("Detect the active project from the current working directory or a supplied path. Returns project name, source, and path. Ambiguity surfaces in the response body — never as a Go error."),
		mcplib.WithString("cwd",
			mcplib.Description("Absolute path to use for detection instead of the server's cwd."),
		),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleCurrentProject(s)}
}

// handleCurrentProject is the ToolHandlerFunc for ion_current_project.
func handleCurrentProject(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		cwdArg := req.GetString("cwd", "")

		var det project.DetectionResult
		var err error
		if cwdArg != "" {
			det, err = s.detect(cwdArg)
		} else {
			det, err = s.resolveProject("", "")
		}

		if err != nil {
			if errors.Is(err, project.ErrAmbiguousProject) {
				// Return ambiguity as structured body, never as Go error.
				body := map[string]any{
					"project":            det.Project,
					"project_source":     det.Source,
					"project_path":       det.Path,
					"error":              "project_ambiguous",
					"available_projects": det.AvailableProjects,
				}
				raw, _ := json.Marshal(body)
				return textResult(raw), nil
			}
			// Other errors: return as structured warning.
			body := map[string]any{
				"project":        "",
				"project_source": "",
				"project_path":   "",
				"warning":        err.Error(),
			}
			raw, _ := json.Marshal(body)
			return textResult(raw), nil
		}

		// Happy path: return DetectionResult directly.
		body := map[string]any{
			"project":        det.Project,
			"project_source": det.Source,
			"project_path":   det.Path,
		}
		if det.Warning != "" {
			body["warning"] = det.Warning
		}
		if len(det.AvailableProjects) > 0 {
			body["available_projects"] = det.AvailableProjects
		}
		// Attach known_directories (capped at 5) when the project is resolvable
		// and the store has session records for it.
		if det.Project != "" {
			if dirs := knownDirectoriesForProject(ctx, s, det.Project); len(dirs) > 0 {
				body["known_directories"] = dirs
			}
		}
		raw, _ := json.Marshal(body)
		return textResult(raw), nil
	}
}

// knownDirectoriesForProject queries the store for the most recently used
// directories of the given project, capped at 5 entries. It silently ignores
// store errors so detection failures never surface as user-visible errors here.
func knownDirectoriesForProject(ctx context.Context, s *Server, project string) []string {
	const maxDirs = 5
	dirs, err := s.store.ProjectDirectories(ctx, project)
	if err != nil || len(dirs) == 0 {
		return nil
	}
	if len(dirs) > maxDirs {
		dirs = dirs[:maxDirs]
	}
	return dirs
}
