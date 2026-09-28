package mcp

import (
	"context"
	"errors"

	"github.com/arodriguezp2003/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSetStatusTool constructs the ion_set_status ServerTool.
func buildSetStatusTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_set_status",
		mcplib.WithDescription("Change an observation's lifecycle status: active, superseded, or obsolete. Memory hygiene means changing state, not deleting — a superseded decision stays searchable (ranked lower, labeled) as history of why the team moved on. status=superseded requires superseded_by (the id of the observation that replaces it), which must point at an observation in the SAME project that is currently status=active — a superseded_by chain must always end at the newest active row, so pointing at an already-superseded or obsolete observation is refused. bugfix and discovery observations are permanent: status=obsolete is refused outright for them, and status=superseded is only accepted when superseded_by also points at a bugfix or discovery observation (a newer fix/discovery replacing an older one, never a demotion). Returns envelope + updated observation object. Missing IDs or invalid transitions return an error in result, never a Go error."),
		mcplib.WithNumber("id", mcplib.Description("Observation ID to update (required)."), mcplib.Required()),
		mcplib.WithString("status", mcplib.Description("New status: active, superseded, or obsolete (required)."), mcplib.Required()),
		mcplib.WithNumber("superseded_by", mcplib.Description("Observation ID that replaces this one. Required when status=superseded.")),
		mcplib.WithString("reason", mcplib.Description("Short human-readable note explaining the status change (optional).")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSetStatus(s)}
}

// handleSetStatus is the ToolHandlerFunc for ion_set_status.
func handleSetStatus(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := int64(req.GetFloat("id", 0))
		status := req.GetString("status", "")
		reason := req.GetString("reason", "")

		det, _ := s.resolveProject("", "")

		if !store.IsValidObservationStatus(status) {
			raw := BuildError(det, CodeInvalidArgument,
				"invalid status: "+status+"; valid statuses: active, superseded, obsolete")
			return textResult(raw), nil
		}

		var supersededBy *int64
		if req.GetFloat("superseded_by", 0) != 0 {
			v := int64(req.GetFloat("superseded_by", 0))
			supersededBy = &v
		}

		if err := s.store.SetObservationStatus(ctx, id, status, supersededBy, reason); err != nil {
			if errors.Is(err, store.ErrObservationNotFound) {
				raw := BuildError(det, CodeNotFound, "observation not found")
				return textResult(raw), nil
			}
			raw := BuildError(det, CodeInvalidArgument, err.Error())
			return textResult(raw), nil
		}

		obs, err := s.store.GetObservation(ctx, id)
		if err != nil {
			raw := BuildError(det, errorCode(err), "status updated but re-fetch failed: "+err.Error())
			return textResult(raw), nil
		}

		raw := Build(det, "observation status updated", map[string]any{
			"observation": observationToMap(ctx, s.store, obs),
		})
		return textResult(raw), nil
	}
}
