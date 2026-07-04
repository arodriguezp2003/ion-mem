package mcp

import (
	"context"
	"errors"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildTimelineTool constructs the ion_timeline ServerTool.
func buildTimelineTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_timeline",
		mcplib.WithDescription("Return a chronological window of observations and prompts from the same session as the given observation. Returns envelope + entries array. Missing observation_id produces an error in result, never a Go error."),
		mcplib.WithNumber("observation_id", mcplib.Description("Anchor observation ID (required)."), mcplib.Required()),
		mcplib.WithNumber("before", mcplib.Description("Number of entries to include before the anchor (default 5).")),
		mcplib.WithNumber("after", mcplib.Description("Number of entries to include after the anchor (default 5).")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleTimeline(s)}
}

// handleTimeline is the ToolHandlerFunc for ion_timeline.
func handleTimeline(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		observationID := int64(req.GetFloat("observation_id", 0))
		before := int(req.GetFloat("before", 5))
		after := int(req.GetFloat("after", 5))

		det, _ := s.resolveProject("", "")

		entries, err := s.store.Timeline(ctx, observationID, before, after)
		if err != nil {
			msg := "observation not found"
			if !errors.Is(err, store.ErrObservationNotFound) {
				msg = "error fetching timeline: " + err.Error()
			}
			raw := BuildError(det, errorCode(err), msg)
			return textResult(raw), nil
		}

		// Serialize entries; ensure the slice is never null in JSON output.
		serialized := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			entry := map[string]any{
				"kind": e.Kind,
			}
			if e.Observation != nil {
				entry["observation"] = observationToMap(*e.Observation)
			}
			if e.Prompt != nil {
				entry["prompt"] = map[string]any{
					"id":         e.Prompt.ID,
					"sync_id":    e.Prompt.SyncID,
					"session_id": e.Prompt.SessionID,
					"content":    e.Prompt.Content,
					"project":    e.Prompt.Project,
					"created_at": e.Prompt.CreatedAt,
				}
			}
			serialized = append(serialized, entry)
		}

		raw := Build(det, "timeline fetched", map[string]any{
			"entries": serialized,
			"count":   len(serialized),
		})
		return textResult(raw), nil
	}
}
