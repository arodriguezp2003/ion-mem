package mcp

import (
	"context"
	"errors"

	"github.com/arodriguezp2003/ion-mem/internal/store"
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
			raw := BuildError(det, errorCode(err), msg)
			return textResult(raw), nil
		}

		obsMap := observationToMap(ctx, s.store, obs)

		raw := Build(det, "observation fetched", map[string]any{
			"observation": obsMap,
		})
		return textResult(raw), nil
	}
}
