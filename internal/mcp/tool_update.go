package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/arodriguezp2003/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// contentChanging returns true when params includes a Title or Content update
// (i.e. the text that gets embedded has changed).
func contentChanging(params store.UpdateObservationParams) bool {
	return params.Title != nil || params.Content != nil
}

// buildUpdateTool constructs the ion_update ServerTool.
func buildUpdateTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_update",
		mcplib.WithDescription("Partially update an observation by ID. Only supplied fields are changed; omitted fields remain unchanged. Returns envelope + updated observation object. Missing or deleted IDs produce an error in result, never a Go error."),
		mcplib.WithNumber("id", mcplib.Description("Observation ID (required)."), mcplib.Required()),
		mcplib.WithString("title", mcplib.Description("New title (optional).")),
		mcplib.WithString("content", mcplib.Description("New content (optional).")),
		mcplib.WithString("type", mcplib.Description("New type (optional).")),
		mcplib.WithString("topic_key", mcplib.Description("New topic key (optional).")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleUpdate(s)}
}

// handleUpdate is the ToolHandlerFunc for ion_update.
func handleUpdate(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := int64(req.GetFloat("id", 0))
		det, _ := s.resolveProject("", "")

		params := store.UpdateObservationParams{}

		if v := req.GetString("title", ""); v != "" {
			params.Title = &v
		}
		if v := req.GetString("content", ""); v != "" {
			params.Content = &v
		}
		if v := req.GetString("type", ""); v != "" {
			if !store.IsValidObservationType(v) {
				raw := BuildError(det, CodeInvalidArgument,
					"invalid type: "+v+"; valid types: decision, architecture, bugfix, discovery, config, preference, pattern, session_summary, manual")
				return textResult(raw), nil
			}
			params.Type = &v
		}
		if v := req.GetString("topic_key", ""); v != "" {
			params.TopicKey = &v
		}

		obs, err := s.store.UpdateObservation(ctx, id, params)
		if err != nil {
			msg := "observation not found"
			if !errors.Is(err, store.ErrObservationNotFound) {
				msg = "error updating observation: " + err.Error()
			}
			raw := BuildError(det, errorCode(err), msg)
			return textResult(raw), nil
		}

		// Best-effort re-embed when title or content changed.
		// On embed failure: delete the stale vector (a stale vector must not lie).
		var embedded bool
		if contentChanging(params) {
			title := obs.Title
			content := obs.Content
			embedded = tryEmbed(ctx, s.store, obs.ID, title, content)
			if !embedded {
				// Best-effort delete of any stale embedding.
				_ = s.store.DeleteEmbedding(ctx, obs.ID)
			}
		}

		obsMap := observationToMap(ctx, s.store, obs)
		raw := Build(det, "observation updated", map[string]any{
			"observation": obsMap,
			"embedded":    embedded,
		})
		return textResult(raw), nil
	}
}

// observationToMap converts a store.Observation to a map suitable for envelope extensions.
// Every response that surfaces a full observation (ion_update, ion_set_status,
// ion_get_observation) includes status/superseded_by/status_reason so a caller
// never has to guess whether a row is history rather than current guidance; a
// superseded row also gets a "note" pointing at its replacement. Building the
// note requires a lookup (see supersededNote), hence ctx/st.
func observationToMap(ctx context.Context, st *store.Store, obs store.Observation) map[string]any {
	m := map[string]any{
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
		"status":          obs.Status,
	}
	if obs.ToolName != nil {
		m["tool_name"] = *obs.ToolName
	}
	if obs.TopicKey != nil {
		m["topic_key"] = *obs.TopicKey
	}
	if obs.SupersededBy != nil {
		m["superseded_by"] = *obs.SupersededBy
		m["note"] = supersededNote(ctx, st, *obs.SupersededBy)
	}
	if obs.StatusReason != nil {
		m["status_reason"] = *obs.StatusReason
	}
	return m
}

// supersededNote formats the lineage note attached to a superseded
// observation's response so an agent reading it sees where to look next. It
// looks the target up so a dangling pointer (the replacement was itself
// later hard- or soft-deleted) is labeled rather than silently implying the
// replacement still exists.
func supersededNote(ctx context.Context, st *store.Store, supersededBy int64) string {
	base := fmt.Sprintf("superseded by #%d", supersededBy)
	if _, err := st.GetObservation(ctx, supersededBy); err != nil {
		return base + " (target no longer available)"
	}
	return base
}
