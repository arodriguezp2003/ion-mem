package mcp

import (
	"context"
	"errors"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// contentPreviewMaxLen is the maximum number of runes included in the
// content_preview field of each revision entry.
const contentPreviewMaxLen = 300

// buildHistoryTool constructs the ion_history ServerTool.
func buildHistoryTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_history",
		mcplib.WithDescription("Return the revision history for an observation. Shows what the observation looked like before each destructive overwrite. Missing or deleted IDs return a not_found error in result, never a Go error."),
		mcplib.WithNumber("id", mcplib.Description("Observation ID (required)."), mcplib.Required()),
		mcplib.WithNumber("limit", mcplib.Description("Maximum number of revisions to return (default 10).")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleHistory(s)}
}

// handleHistory is the ToolHandlerFunc for ion_history.
func handleHistory(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := int64(req.GetFloat("id", 0))
		limit := int(req.GetFloat("limit", 10))
		if limit <= 0 {
			limit = 10
		}

		det, _ := s.resolveProject("", "")

		if id == 0 {
			raw := BuildError(det, CodeInvalidArgument, "id is required")
			return textResult(raw), nil
		}

		obs, err := s.store.GetObservation(ctx, id)
		if err != nil {
			msg := "observation not found"
			if !errors.Is(err, store.ErrObservationNotFound) {
				msg = "error fetching observation: " + err.Error()
			}
			raw := BuildError(det, errorCode(err), msg)
			return textResult(raw), nil
		}

		revs, err := s.store.ListRevisions(ctx, id)
		if err != nil {
			msg := "error fetching revisions: " + err.Error()
			raw := BuildError(det, CodeDBError, msg)
			return textResult(raw), nil
		}

		// Apply limit.
		if len(revs) > limit {
			revs = revs[:limit]
		}

		// Serialize revisions; always an array, never null.
		serialized := make([]map[string]any, 0, len(revs))
		for _, r := range revs {
			entry := map[string]any{
				"revision":        r.Revision,
				"type":            r.Type,
				"title":           r.Title,
				"content_preview": truncateRunes(r.Content, contentPreviewMaxLen),
				"archived_at":     r.ArchivedAt,
			}
			if r.ToolName != nil {
				entry["tool_name"] = *r.ToolName
			}
			serialized = append(serialized, entry)
		}

		obsMap := map[string]any{
			"id":             obs.ID,
			"title":          obs.Title,
			"type":           obs.Type,
			"revision_count": obs.RevisionCount,
			"updated_at":     obs.UpdatedAt,
		}

		raw := Build(det, "history fetched", map[string]any{
			"observation": obsMap,
			"revisions":   serialized,
			"count":       len(serialized),
		})
		return textResult(raw), nil
	}
}

// truncateRunes returns s truncated to at most maxLen runes.
func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
