package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildContextTool constructs the ion_context ServerTool.
func buildContextTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_context",
		mcplib.WithDescription("Returns a markdown summary of recent sessions and observations for the current project. Empty store returns valid empty markdown (never a Go error)."),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithNumber("limit", mcplib.Description("Max items to include (default: 10).")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleContext(s)}
}

// handleContext is the ToolHandlerFunc for ion_context.
func handleContext(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		projectArg := req.GetString("project", "")
		limit := req.GetInt("limit", 10)
		cwdArg := req.GetString("cwd", "")

		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		if limit <= 0 {
			limit = 10
		}

		// Gather recent sessions.
		sessions, err := s.store.RecentSessions(ctx, det.Project, limit)
		if err != nil {
			raw := BuildError(det, CodeDBError, "error fetching sessions: "+err.Error())
			return textResult(raw), nil
		}

		// Gather recent observations.
		obs, err := s.store.RecentObservations(ctx, store.RecentObservationsParams{
			Project: det.Project,
			Limit:   limit,
		})
		if err != nil {
			raw := BuildError(det, CodeDBError, "error fetching observations: "+err.Error())
			return textResult(raw), nil
		}

		md := buildContextMarkdown(det.Project, sessions, obs)
		raw := Build(det, md, nil)
		return textResult(raw), nil
	}
}

// buildContextMarkdown assembles the context markdown string from sessions and observations.
// Returns a minimal but valid markdown string even when both slices are empty.
func buildContextMarkdown(project string, sessions []store.Session, obs []store.Observation) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Context: %s\n\n", project))

	// Sessions section.
	sb.WriteString("## Recent Sessions\n\n")
	if len(sessions) == 0 {
		sb.WriteString("_No sessions found._\n\n")
	} else {
		for _, sess := range sessions {
			sb.WriteString(fmt.Sprintf("- **%s** (status: %s, started: %s)\n", sess.ID, sess.Status, sess.StartedAt.Format("2006-01-02 15:04:05")))
			if sess.Summary != nil && *sess.Summary != "" {
				sb.WriteString(fmt.Sprintf("  - Summary: %s\n", *sess.Summary))
			}
		}
		sb.WriteString("\n")
	}

	// Observations section.
	sb.WriteString("## Recent Observations\n\n")
	if len(obs) == 0 {
		sb.WriteString("_No observations found._\n\n")
	} else {
		for _, o := range obs {
			sb.WriteString(fmt.Sprintf("- [%d] **%s** (%s)\n", o.ID, o.Title, o.Type))
			if o.Content != "" {
				preview := o.Content
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("  > %s\n", preview))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
