package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSessionStartTool constructs the ion_session_start ServerTool.
func buildSessionStartTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_session_start",
		mcplib.WithDescription("Start or reuse a named session. Duplicate session_id is idempotent (created:false, no error). Returns envelope + session_id and created:bool."),
		mcplib.WithString("session_id", mcplib.Description("Session ID (required)."), mcplib.Required()),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithString("directory", mcplib.Description("Working directory to associate with the session.")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSessionStart(s)}
}

// handleSessionStart is the ToolHandlerFunc for ion_session_start.
func handleSessionStart(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		sessionID, _ := req.RequireString("session_id")
		projectArg := req.GetString("project", "")
		directory := req.GetString("directory", "")
		cwdArg := req.GetString("cwd", "")

		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		_, err = s.store.CreateSession(ctx, store.CreateSessionParams{
			ID:        sessionID,
			Project:   det.Project,
			Directory: directory,
		})

		created := true
		if err != nil {
			if isAlreadyExistsError(err) {
				created = false
			} else {
				raw := BuildError(det, CodeDBError, "error creating session: "+err.Error())
				return textResult(raw), nil
			}
		}

		raw := Build(det, "session ready", map[string]any{
			"session_id": sessionID,
			"created":    created,
		})
		return textResult(raw), nil
	}
}

// buildSessionEndTool constructs the ion_session_end ServerTool.
func buildSessionEndTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_session_end",
		mcplib.WithDescription("End an active session. Unknown session_id returns an error in result (no Go error). Returns envelope + session_id and ended_at."),
		mcplib.WithString("session_id", mcplib.Description("Session ID to end (required)."), mcplib.Required()),
		mcplib.WithString("summary", mcplib.Description("Optional session summary.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSessionEnd(s)}
}

// handleSessionEnd is the ToolHandlerFunc for ion_session_end.
func handleSessionEnd(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		sessionID, _ := req.RequireString("session_id")
		summary := req.GetString("summary", "")

		det, _ := s.resolveProject("", "")

		err := s.store.EndSession(ctx, sessionID, summary)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				raw := BuildError(det, CodeNotFound, fmt.Sprintf("session %q not found", sessionID))
				return textResult(raw), nil
			}
			raw := BuildError(det, CodeDBError, "error ending session: "+err.Error())
			return textResult(raw), nil
		}

		// Fetch ended_at from the store.
		sess, err := s.store.GetSession(ctx, sessionID)
		endedAt := ""
		if err == nil && sess.EndedAt != nil {
			endedAt = *sess.EndedAt
		}

		raw := Build(det, "session ended", map[string]any{
			"session_id": sessionID,
			"ended_at":   endedAt,
		})
		return textResult(raw), nil
	}
}

// buildSessionSummaryTool constructs the ion_session_summary ServerTool.
func buildSessionSummaryTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_session_summary",
		mcplib.WithDescription("Save a session summary as an observation (type=session_summary). When session_id is provided, also ends the session. Auto-creates session if absent. Returns envelope + session_id, observation_id, sync_id."),
		mcplib.WithString("summary", mcplib.Description("Session summary markdown (required)."), mcplib.Required()),
		mcplib.WithString("session_id", mcplib.Description("Session ID. Auto-created if absent.")),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithString("topic_key", mcplib.Description("Optional topic key for upsert.")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSessionSummary(s)}
}

// handleSessionSummary is the ToolHandlerFunc for ion_session_summary.
func handleSessionSummary(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		summary, _ := req.RequireString("summary")
		sessionIDArg := req.GetString("session_id", "")
		projectArg := req.GetString("project", "")
		topicKey := req.GetString("topic_key", "")
		cwdArg := req.GetString("cwd", "")

		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		// Ensure session (auto-create if session_id absent).
		sessionID, err := s.ensureSession(ctx, det.Project, sessionIDArg)
		if err != nil {
			raw := BuildError(det, CodeDBError, "error ensuring session: "+err.Error())
			return textResult(raw), nil
		}

		// Save summary as an observation.
		obs, err := s.store.AddObservation(ctx, store.AddObservationParams{
			SessionID: sessionID,
			Type:      "session_summary",
			Title:     fmt.Sprintf("Session summary: %s", det.Project),
			Content:   summary,
			Project:   det.Project,
			Scope:     "project",
			TopicKey:  topicKey,
		})
		if err != nil {
			raw := BuildError(det, CodeDBError, "error saving summary: "+err.Error())
			return textResult(raw), nil
		}

		// Critical side-effect: when a session_id was supplied, also end the session.
		// The observation is already saved; a failure here must not discard it.
		// session_ended reports the outcome so callers can detect silent losses.
		sessionEnded := false
		resultMsg := "session summary saved"
		if sessionIDArg != "" {
			if endErr := s.endSessionFn(ctx, sessionID, summary); endErr != nil {
				resultMsg = "summary saved; session end failed: " + endErr.Error()
			} else {
				sessionEnded = true
			}
		}

		raw := Build(det, resultMsg, map[string]any{
			"session_id":     sessionID,
			"observation_id": obs.ID,
			"sync_id":        obs.SyncID,
			"session_ended":  sessionEnded,
		})
		return textResult(raw), nil
	}
}
