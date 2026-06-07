package mcp

import (
	"testing"
)

func TestPromptBuffer_round_trip(t *testing.T) {
	s := &Server{
		promptsBySession: make(map[string]string),
	}
	s.recordPrompt("sess-1", "hello world")
	got := s.lastPromptForSession("sess-1")
	if got != "hello world" {
		t.Errorf("lastPromptForSession = %q, want %q", got, "hello world")
	}
}

func TestPromptBuffer_single_slot_overwrite(t *testing.T) {
	s := &Server{
		promptsBySession: make(map[string]string),
	}
	s.recordPrompt("sess-1", "first")
	s.recordPrompt("sess-1", "second")
	got := s.lastPromptForSession("sess-1")
	if got != "second" {
		t.Errorf("expected only 'second' after overwrite, got %q", got)
	}
}

func TestPromptBuffer_empty_when_no_prompt(t *testing.T) {
	s := &Server{
		promptsBySession: make(map[string]string),
	}
	got := s.lastPromptForSession("nonexistent-session")
	if got != "" {
		t.Errorf("expected empty string for missing session, got %q", got)
	}
}

func TestPromptBuffer_clears_after_read(t *testing.T) {
	s := &Server{
		promptsBySession: make(map[string]string),
	}
	s.recordPrompt("sess-1", "the prompt")
	_ = s.lastPromptForSession("sess-1")
	// Second read must return empty.
	got := s.lastPromptForSession("sess-1")
	if got != "" {
		t.Errorf("expected empty after read clears slot, got %q", got)
	}
}
