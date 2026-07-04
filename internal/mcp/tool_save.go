package mcp

import (
	"context"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSaveTool constructs the ion_save ServerTool.
func buildSaveTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_save",
		mcplib.WithDescription("Save a memory observation. Handles topic_key upsert, dedup, and prompt capture. Returns envelope with id, sync_id, revision_count, duplicate_count, prompt_attached."),
		mcplib.WithString("title", mcplib.Description("Observation title (required)."), mcplib.Required()),
		mcplib.WithString("content", mcplib.Description("Observation content.")),
		mcplib.WithString("type", mcplib.Description("Observation type (default: manual).")),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithString("scope", mcplib.Description("Scope: project (default) or personal.")),
		mcplib.WithString("topic_key", mcplib.Description("Stable key for upsert.")),
		mcplib.WithString("session_id", mcplib.Description("Session ID. Auto-created if unknown.")),
		mcplib.WithBoolean("capture_prompt", mcplib.Description("Attach last buffered prompt (default: true).")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSave(s)}
}

// handleSave is the ToolHandlerFunc for ion_save.
func handleSave(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title, _ := req.RequireString("title")
		content := req.GetString("content", "")
		obsType := req.GetString("type", "manual")
		projectArg := req.GetString("project", "")
		scope := req.GetString("scope", "project")
		topicKey := req.GetString("topic_key", "")
		sessionIDArg := req.GetString("session_id", "")
		capturePrompt := req.GetBool("capture_prompt", true)
		cwdArg := req.GetString("cwd", "")

		// Resolve project.
		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		// Ensure session exists (auto-create if unknown).
		sessionID, err := s.ensureSession(ctx, det.Project, sessionIDArg)
		if err != nil {
			raw := BuildError(det, CodeDBError, "error ensuring session: "+err.Error())
			return textResult(raw), nil
		}

		// Attach prompt if requested and buffer is non-empty.
		promptAttached := false
		if capturePrompt {
			lastPrompt := s.lastPromptForSession(sessionID)
			if lastPrompt != "" {
				_, _ = s.store.AddPromptIfMissing(ctx, store.AddPromptParams{
					SessionID: sessionID,
					Content:   lastPrompt,
					Project:   det.Project,
				})
				promptAttached = true
			}
		}

		// Save observation.
		obs, err := s.store.AddObservation(ctx, store.AddObservationParams{
			SessionID: sessionID,
			Type:      obsType,
			Title:     title,
			Content:   content,
			Project:   det.Project,
			Scope:     scope,
			TopicKey:  topicKey,
		})
		if err != nil {
			raw := BuildError(det, CodeDBError, "error saving observation: "+err.Error())
			return textResult(raw), nil
		}

		extras := map[string]any{
			"id":              obs.ID,
			"sync_id":         obs.SyncID,
			"revision_count":  obs.RevisionCount,
			"duplicate_count": obs.DuplicateCount,
			"prompt_attached": promptAttached,
		}
		raw := Build(det, "observation saved", extras)
		return textResult(raw), nil
	}
}
