package mcp

import (
	"context"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSavePromptTool constructs the ion_save_prompt ServerTool.
func buildSavePromptTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_save_prompt",
		mcplib.WithDescription("Record a user prompt for durable context capture. Stores via store.AddPromptIfMissing (dedup on session+content). Empty content is rejected. Returns envelope + id, sync_id, session_id."),
		mcplib.WithString("content", mcplib.Description("Prompt content (required)."), mcplib.Required()),
		mcplib.WithString("session_id", mcplib.Description("Session ID. Auto-created if absent.")),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSavePrompt(s)}
}

// handleSavePrompt is the ToolHandlerFunc for ion_save_prompt.
func handleSavePrompt(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		content, _ := req.RequireString("content")
		sessionIDArg := req.GetString("session_id", "")
		projectArg := req.GetString("project", "")
		cwdArg := req.GetString("cwd", "")

		// Per spec R-S2-SP-01 / silent-fall-through contract:
		// Empty content MUST NOT overwrite buffer; return error in result.
		if content == "" {
			det, _ := s.resolveProject(projectArg, cwdArg)
			raw := BuildError(det, CodeInvalidArgument, "empty content: prompt not saved")
			return textResult(raw), nil
		}

		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		// Ensure session.
		sessionID, err := s.ensureSession(ctx, det.Project, sessionIDArg)
		if err != nil {
			raw := BuildError(det, CodeDBError, "error ensuring session: "+err.Error())
			return textResult(raw), nil
		}

		// Store the prompt durably. No in-memory buffer is written —
		// ion_save consumes via store.ConsumeLatestPrompt (spec R-S2-SP-02).
		prompt, err := s.store.AddPromptIfMissing(ctx, store.AddPromptParams{
			SessionID: sessionID,
			Content:   content,
			Project:   det.Project,
		})
		if err != nil {
			raw := BuildError(det, CodeDBError, "error saving prompt: "+err.Error())
			return textResult(raw), nil
		}

		raw := Build(det, "prompt saved", map[string]any{
			"id":         prompt.ID,
			"sync_id":    prompt.SyncID,
			"session_id": sessionID,
		})
		return textResult(raw), nil
	}
}
