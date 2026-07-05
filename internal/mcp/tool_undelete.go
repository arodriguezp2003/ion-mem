package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildUndeleteTool constructs the ion_undelete ServerTool.
func buildUndeleteTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_undelete",
		mcplib.WithDescription("Recover a soft-deleted observation by ID. Clears the deleted_at timestamp and makes the observation visible to search and retrieval again. Returns not_found when the observation does not exist, was never soft-deleted, or was permanently hard-deleted."),
		mcplib.WithNumber("id", mcplib.Description("Observation ID (required)."), mcplib.Required()),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleUndelete(s)}
}

// handleUndelete is the ToolHandlerFunc for ion_undelete.
func handleUndelete(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := int64(req.GetFloat("id", 0))

		det, _ := s.resolveProject("", "")

		err := s.store.UndeleteObservation(ctx, id)
		if err != nil {
			msg := "observation not found or not soft-deleted"
			if !errors.Is(err, store.ErrObservationNotFound) {
				msg = "error restoring observation: " + err.Error()
			}
			raw := BuildError(det, errorCode(err), msg)
			return textResult(raw), nil
		}

		raw := Build(det, fmt.Sprintf("observation %d restored", id), nil)
		return textResult(raw), nil
	}
}
