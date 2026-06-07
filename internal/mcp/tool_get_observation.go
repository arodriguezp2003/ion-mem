package mcp

import (
	"context"
	"errors"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildGetObservationTool constructs the ion_get_observation ServerTool.
func buildGetObservationTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_get_observation",
		mcplib.WithDescription("Fetch a single observation by ID. Returns envelope + full observation object. Missing or deleted IDs return an error in result, never a Go error."),
		mcplib.WithNumber("id", mcplib.Description("Observation ID (required)."), mcplib.Required()),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleGetObservation(s)}
}

// handleGetObservation is the ToolHandlerFunc for ion_get_observation.
func handleGetObservation(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := int64(req.GetFloat("id", 0))

		// Use a zero-value DetectionResult for envelope when no project resolution is needed.
		det, _ := s.resolveProject("", "")

		obs, err := s.store.GetObservation(ctx, id)
		if err != nil {
			msg := "observation not found"
			if !errors.Is(err, store.ErrObservationNotFound) {
				msg = "error fetching observation: " + err.Error()
			}
			raw := Build(det, msg, nil)
			return textResult(raw), nil
		}

		obsMap := map[string]any{
			"id":              obs.ID,
			"sync_id":         obs.SyncID,
			"session_id":      obs.SessionID,
			"type":            obs.Type,
			"title":           obs.Title,
			"content":         obs.Content,
			"project":         obs.Project,
			"scope":           obs.Scope,
			"revision_count":  obs.RevisionCount,
			"duplicate_count": obs.DuplicateCount,
			"last_seen_at":    obs.LastSeenAt,
			"created_at":      obs.CreatedAt,
			"updated_at":      obs.UpdatedAt,
		}
		if obs.ToolName != nil {
			obsMap["tool_name"] = *obs.ToolName
		}
		if obs.TopicKey != nil {
			obsMap["topic_key"] = *obs.TopicKey
		}

		raw := Build(det, "observation fetched", map[string]any{
			"observation": obsMap,
		})
		return textResult(raw), nil
	}
}
