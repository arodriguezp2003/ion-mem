package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildDeleteTool constructs the ion_delete ServerTool.
func buildDeleteTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_delete",
		mcplib.WithDescription("Delete an observation by ID. Default is soft delete (hard=false), which hides it from search and retrieval. Set hard=true for permanent removal. Missing IDs produce an error in result, never a Go error."),
		mcplib.WithNumber("id", mcplib.Description("Observation ID (required)."), mcplib.Required()),
		mcplib.WithBoolean("hard", mcplib.Description("When true, permanently removes the row. Default: false (soft delete).")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleDelete(s)}
}

// handleDelete is the ToolHandlerFunc for ion_delete.
func handleDelete(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := int64(req.GetFloat("id", 0))
		hard := req.GetBool("hard", false)

		det, _ := s.resolveProject("", "")

		err := s.store.DeleteObservation(ctx, id, hard)
		if err != nil {
			msg := "observation not found"
			if !errors.Is(err, store.ErrObservationNotFound) {
				msg = "error deleting observation: " + err.Error()
			}
			raw := BuildError(det, errorCode(err), msg)
			return textResult(raw), nil
		}

		deleteType := "soft"
		if hard {
			deleteType = "hard"
		}
		raw := Build(det, fmt.Sprintf("observation %d deleted (%s)", id, deleteType), nil)
		return textResult(raw), nil
	}
}
