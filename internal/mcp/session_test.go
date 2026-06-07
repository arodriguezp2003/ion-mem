package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

func mustTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("mustTestStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestEnsureSession_auto_creates_when_no_arg(t *testing.T) {
	st := mustTestStore(t)
	s := New(st)

	id, err := s.ensureSession(context.Background(), "myproj", "")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if id == "" {
		t.Error("ensureSession returned empty session ID")
	}
}

func TestEnsureSession_caches_for_same_project(t *testing.T) {
	st := mustTestStore(t)
	s := New(st)

	id1, err := s.ensureSession(context.Background(), "myproj", "")
	if err != nil {
		t.Fatalf("first ensureSession: %v", err)
	}
	id2, err := s.ensureSession(context.Background(), "myproj", "")
	if err != nil {
		t.Fatalf("second ensureSession: %v", err)
	}
	if id1 != id2 {
		t.Errorf("ensureSession returned different IDs for same project: %q vs %q", id1, id2)
	}
}

func TestEnsureSession_respects_caller_supplied_id(t *testing.T) {
	st := mustTestStore(t)
	s := New(st)

	id, err := s.ensureSession(context.Background(), "myproj", "my-specific-session")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if id != "my-specific-session" {
		t.Errorf("ensureSession = %q, want %q", id, "my-specific-session")
	}
}

func TestEnsureSession_idempotent_for_duplicate_session_id(t *testing.T) {
	st := mustTestStore(t)
	s := New(st)

	// Create the session first.
	_, err := s.ensureSession(context.Background(), "myproj", "duplicate-session")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call with same ID must succeed (idempotent).
	id, err := s.ensureSession(context.Background(), "myproj", "duplicate-session")
	if err != nil {
		t.Fatalf("second call with duplicate id: %v", err)
	}
	if id != "duplicate-session" {
		t.Errorf("expected %q, got %q", "duplicate-session", id)
	}
}

func TestIsAlreadyExistsError_true_for_unique_violation(t *testing.T) {
	cases := []string{
		"UNIQUE constraint failed: sessions.id",
		"PRIMARY KEY constraint failed",
		"something already exists",
	}
	for _, msg := range cases {
		if !isAlreadyExistsError(errMsg(msg)) {
			t.Errorf("isAlreadyExistsError(%q) = false, want true", msg)
		}
	}
}

func TestIsAlreadyExistsError_false_for_other_errors(t *testing.T) {
	cases := []struct {
		msg string
		err error
	}{
		{"disk full", errMsg("disk full")},
		{"connection refused", errMsg("connection refused")},
		{"nil", nil},
	}
	for _, tc := range cases {
		if isAlreadyExistsError(tc.err) {
			t.Errorf("isAlreadyExistsError(%q) = true, want false", tc.msg)
		}
	}
}

func TestWithDefaultProject_sets_default_proj(t *testing.T) {
	st := mustTestStore(t)
	s := New(st, WithDefaultProject("my-default"))
	if s.defaultProj != "my-default" {
		t.Errorf("defaultProj = %q, want %q", s.defaultProj, "my-default")
	}
}

// errMsg is a simple error type for tests.
type errMsg string

func (e errMsg) Error() string { return string(e) }
