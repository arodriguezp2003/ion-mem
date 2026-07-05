package mcp

import (
	"context"
	"time"

	"github.com/ionix/ion-mem/internal/embed"
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

		// Validate type vocabulary. Empty type is allowed and defaults to "manual"
		// (handled below when AddObservation is called). Non-empty unknown types
		// are rejected immediately with invalid_argument.
		if obsType != "manual" && obsType != "" && !store.IsValidObservationType(obsType) {
			det, _ := s.resolveProject(projectArg, cwdArg)
			raw := BuildError(det, CodeInvalidArgument,
				"invalid type: "+obsType+"; valid types: decision, architecture, bugfix, discovery, config, preference, pattern, session_summary, manual")
			return textResult(raw), nil
		}

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

		// Attach prompt if requested: consume the latest unconsumed prompt row
		// from the durable store. Survives process restarts between save_prompt
		// and save calls (spec R-TOOL-SAVE-04, R-S2-SESSION-01).
		promptAttached := false
		if capturePrompt {
			_, attached, err := s.store.ConsumeLatestPrompt(ctx, sessionID)
			if err != nil {
				raw := BuildError(det, CodeDBError, "error consuming prompt: "+err.Error())
				return textResult(raw), nil
			}
			promptAttached = attached
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

		// Best-effort embedding: read settings, embed title+content, upsert row.
		// On any failure: save still succeeds; embedded=false in the response.
		embedded := tryEmbed(ctx, s.store, obs.ID, title, content)

		extras := map[string]any{
			"id":              obs.ID,
			"sync_id":         obs.SyncID,
			"revision_count":  obs.RevisionCount,
			"duplicate_count": obs.DuplicateCount,
			"prompt_attached": promptAttached,
			"embedded":        embedded,
		}
		raw := Build(det, "observation saved", extras)
		return textResult(raw), nil
	}
}

// tryEmbed attempts to embed title+"\n"+content using the current settings.
// Returns true when the embedding was stored successfully, false on any error.
// This is always best-effort: callers must not fail the main operation on false.
func tryEmbed(ctx context.Context, st *store.Store, obsID int64, title, content string) bool {
	enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false")
	if enabled != "true" {
		return false
	}

	url := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
	model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, "nomic-embed-text")

	client := embed.DefaultClient(url)
	embedder := embed.NewOllamaEmbedder(client, model)

	embedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	text := title + "\n" + content
	vec, err := embedder.Embed(embedCtx, text)
	if err != nil {
		return false
	}

	if err := st.UpsertEmbedding(ctx, obsID, model, vec); err != nil {
		return false
	}
	return true
}
