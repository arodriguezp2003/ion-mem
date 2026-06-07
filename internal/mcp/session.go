package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// ensureSession returns a valid session ID for the given project.
// Precedence:
//  1. sessionIDArg (caller-supplied, non-empty) — verify it exists in store, create if missing.
//  2. Cached session for this project (from a previous ensureSession call).
//  3. Auto-generate: "mcp-<project>-<unixnano>" and call store.CreateSession.
func (s *Server) ensureSession(ctx context.Context, proj, sessionIDArg string) (string, error) {
	if sessionIDArg != "" {
		// Per spec R-TOOL-SAVE-07: unknown session_id MUST call ensureSession (auto-create).
		// Attempt to create; if PK conflict (already exists) → success (idempotent).
		err := s.createSessionIfMissing(ctx, sessionIDArg, proj)
		if err != nil {
			return "", err
		}
		return sessionIDArg, nil
	}

	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if id, ok := s.sessionsByProj[proj]; ok {
		return id, nil
	}

	// Generate and create a new session.
	id := fmt.Sprintf("mcp-%s-%d", proj, time.Now().UnixNano()+autoSessionCounter.Add(1))
	_, err := s.store.CreateSession(ctx, store.CreateSessionParams{
		ID:      id,
		Project: proj,
	})
	if err != nil {
		return "", fmt.Errorf("mcp: ensureSession create: %w", err)
	}

	s.sessionsByProj[proj] = id
	return id, nil
}

// createSessionIfMissing attempts to create the session. A PK conflict
// (session already exists) is treated as success (idempotent).
func (s *Server) createSessionIfMissing(ctx context.Context, sessionID, proj string) error {
	_, err := s.store.CreateSession(ctx, store.CreateSessionParams{
		ID:      sessionID,
		Project: proj,
	})
	if err != nil {
		// If error message indicates PK conflict / already exists, treat as success.
		if isAlreadyExistsError(err) {
			return nil
		}
		return fmt.Errorf("mcp: ensureSession(id=%s): %w", sessionID, err)
	}
	return nil
}

// isAlreadyExistsError returns true if the error is a SQLite UNIQUE/PK constraint
// violation (session already exists).
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "UNIQUE constraint failed", "PRIMARY KEY constraint failed", "already exists")
}

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// autoSessionCounter provides monotonically increasing values for unique session IDs.
var autoSessionCounter atomic.Int64

// recordPrompt stores the latest prompt content for the given session (single-slot, overwrites).
func (s *Server) recordPrompt(sessionID, content string) {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	s.promptsBySession[sessionID] = content
}

// lastPromptForSession returns the last buffered prompt for the session, clearing the slot.
// Returns empty string when no prompt is buffered.
func (s *Server) lastPromptForSession(sessionID string) string {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	p := s.promptsBySession[sessionID]
	delete(s.promptsBySession, sessionID)
	return p
}
