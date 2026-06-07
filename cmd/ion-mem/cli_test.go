package main

import (
	"strings"
	"testing"
)

func TestParseMCPFlags_defaults(t *testing.T) {
	getenv := func(string) string { return "" }
	cfg, err := parseMCPFlags(nil, getenv, fakeHome)
	if err != nil {
		t.Fatalf("parseMCPFlags: %v", err)
	}
	if cfg.profile != "agent" {
		t.Errorf("profile = %q, want %q", cfg.profile, "agent")
	}
	if !strings.HasSuffix(cfg.dataDir, "/.ion-mem") {
		t.Errorf("dataDir = %q, want suffix /.ion-mem", cfg.dataDir)
	}
	if cfg.project != "" {
		t.Errorf("project = %q, want empty (no env, no flag)", cfg.project)
	}
}

func TestParseMCPFlags_envProjectOverridesEmpty(t *testing.T) {
	getenv := func(k string) string {
		if k == "ION_MEM_PROJECT" {
			return "from-env"
		}
		return ""
	}
	cfg, err := parseMCPFlags(nil, getenv, fakeHome)
	if err != nil {
		t.Fatalf("parseMCPFlags: %v", err)
	}
	if cfg.project != "from-env" {
		t.Errorf("project = %q, want %q", cfg.project, "from-env")
	}
}

func TestParseMCPFlags_explicitFlagsBeatEnv(t *testing.T) {
	getenv := func(k string) string {
		if k == "ION_MEM_PROJECT" {
			return "from-env"
		}
		return ""
	}
	args := []string{"--profile=all", "--data-dir=/tmp/abc", "--project=from-flag"}
	cfg, err := parseMCPFlags(args, getenv, fakeHome)
	if err != nil {
		t.Fatalf("parseMCPFlags: %v", err)
	}
	if cfg.profile != "all" {
		t.Errorf("profile = %q, want %q", cfg.profile, "all")
	}
	if cfg.dataDir != "/tmp/abc" {
		t.Errorf("dataDir = %q, want %q", cfg.dataDir, "/tmp/abc")
	}
	if cfg.project != "from-flag" {
		t.Errorf("project = %q, want %q (flag beats env)", cfg.project, "from-flag")
	}
}

func TestParseMCPFlags_unknownFlagReturnsError(t *testing.T) {
	getenv := func(string) string { return "" }
	_, err := parseMCPFlags([]string{"--bogus=value"}, getenv, fakeHome)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestDefaultDataDir_usesHomeIonMem(t *testing.T) {
	got := defaultDataDir(func() (string, error) { return "/home/user", nil })
	if got != "/home/user/.ion-mem" {
		t.Errorf("defaultDataDir = %q, want %q", got, "/home/user/.ion-mem")
	}
}

func TestDefaultDataDir_homeErrorFallsBackToCwd(t *testing.T) {
	got := defaultDataDir(func() (string, error) { return "", errFakeHomeUnavailable })
	if got != ".ion-mem" {
		t.Errorf("defaultDataDir on home error = %q, want %q", got, ".ion-mem")
	}
}

func TestVersionString_hasName(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "ion-mem ") {
		t.Errorf("versionString = %q, want prefix %q", got, "ion-mem ")
	}
}

func TestRouteCommand_unknown(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "bogus"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %q, want substring %q", err.Error(), "unknown command")
	}
}

func TestRouteCommand_noArgsShowsUsage(t *testing.T) {
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem"}, &sb)
	if err == nil {
		t.Fatal("expected error (no command given)")
	}
	out := sb.String()
	if !strings.Contains(out, "Usage:") {
		t.Errorf("usage output missing %q: %s", "Usage:", out)
	}
}

func TestRouteCommand_versionWritesToOut(t *testing.T) {
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "version"}, &sb)
	if err != nil {
		t.Fatalf("routeCommand(version): %v", err)
	}
	if !strings.Contains(sb.String(), "ion-mem ") {
		t.Errorf("version output = %q", sb.String())
	}
}

func TestRouteCommand_helpAliases(t *testing.T) {
	for _, alias := range []string{"help", "--help", "-h"} {
		var sb strings.Builder
		err := routeCommand([]string{"ion-mem", alias}, &sb)
		if err != nil {
			t.Errorf("routeCommand(%q): %v", alias, err)
		}
		if !strings.Contains(sb.String(), "Usage:") {
			t.Errorf("alias %q did not produce usage; got %q", alias, sb.String())
		}
	}
}

// ─── session-start flag tests ─────────────────────────────────────────────────

func TestParseSessionStartFlags_defaults(t *testing.T) {
	// Missing required --id flag must return an error.
	_, err := parseSessionStartFlags([]string{"--project=foo", "--cwd=/tmp"}, fakeHome)
	if err == nil {
		t.Fatal("expected error when --id is missing")
	}
}

func TestParseSessionStartFlags_all(t *testing.T) {
	cfg, err := parseSessionStartFlags(
		[]string{"--id=sess-1", "--project=myproject", "--cwd=/workspace", "--data-dir=/tmp/x"},
		fakeHome,
	)
	if err != nil {
		t.Fatalf("parseSessionStartFlags: %v", err)
	}
	if cfg.id != "sess-1" {
		t.Errorf("id = %q, want %q", cfg.id, "sess-1")
	}
	if cfg.project != "myproject" {
		t.Errorf("project = %q, want %q", cfg.project, "myproject")
	}
	if cfg.cwd != "/workspace" {
		t.Errorf("cwd = %q, want %q", cfg.cwd, "/workspace")
	}
	if cfg.dataDir != "/tmp/x" {
		t.Errorf("dataDir = %q, want %q", cfg.dataDir, "/tmp/x")
	}
}

// ─── session-end flag tests ───────────────────────────────────────────────────

func TestParseSessionEndFlags_defaults(t *testing.T) {
	// Missing required --id flag must return an error.
	_, err := parseSessionEndFlags([]string{}, fakeHome)
	if err == nil {
		t.Fatal("expected error when --id is missing")
	}
}

func TestParseSessionEndFlags_all(t *testing.T) {
	cfg, err := parseSessionEndFlags(
		[]string{"--id=sess-1", "--summary=done", "--data-dir=/tmp/y"},
		fakeHome,
	)
	if err != nil {
		t.Fatalf("parseSessionEndFlags: %v", err)
	}
	if cfg.id != "sess-1" {
		t.Errorf("id = %q, want %q", cfg.id, "sess-1")
	}
	if cfg.summary != "done" {
		t.Errorf("summary = %q, want %q", cfg.summary, "done")
	}
	if cfg.dataDir != "/tmp/y" {
		t.Errorf("dataDir = %q, want %q", cfg.dataDir, "/tmp/y")
	}
}

// ─── context flag tests ───────────────────────────────────────────────────────

func TestParseContextFlags_defaults(t *testing.T) {
	// Missing required --project flag must return an error.
	_, err := parseContextFlags([]string{}, fakeHome)
	if err == nil {
		t.Fatal("expected error when --project is missing")
	}
}

func TestParseContextFlags_all(t *testing.T) {
	cfg, err := parseContextFlags(
		[]string{"--project=myproject", "--scope=personal", "--data-dir=/tmp/z"},
		fakeHome,
	)
	if err != nil {
		t.Fatalf("parseContextFlags: %v", err)
	}
	if cfg.project != "myproject" {
		t.Errorf("project = %q, want %q", cfg.project, "myproject")
	}
	if cfg.scope != "personal" {
		t.Errorf("scope = %q, want %q", cfg.scope, "personal")
	}
	if cfg.dataDir != "/tmp/z" {
		t.Errorf("dataDir = %q, want %q", cfg.dataDir, "/tmp/z")
	}
}

// ─── save-prompt flag tests ───────────────────────────────────────────────────

func TestParseSavePromptFlags_defaults(t *testing.T) {
	// Missing required --session-id flag must return an error.
	_, err := parseSavePromptFlags([]string{"--content=hello"}, fakeHome)
	if err == nil {
		t.Fatal("expected error when --session-id is missing")
	}
}

func TestParseSavePromptFlags_missingContent(t *testing.T) {
	// Missing required --content flag must return an error.
	_, err := parseSavePromptFlags([]string{"--session-id=sess-1"}, fakeHome)
	if err == nil {
		t.Fatal("expected error when --content is missing")
	}
}

func TestParseSavePromptFlags_all(t *testing.T) {
	cfg, err := parseSavePromptFlags(
		[]string{"--session-id=sess-1", "--content=hello world", "--project=foo", "--data-dir=/tmp/a"},
		fakeHome,
	)
	if err != nil {
		t.Fatalf("parseSavePromptFlags: %v", err)
	}
	if cfg.sessionID != "sess-1" {
		t.Errorf("sessionID = %q, want %q", cfg.sessionID, "sess-1")
	}
	if cfg.content != "hello world" {
		t.Errorf("content = %q, want %q", cfg.content, "hello world")
	}
	if cfg.project != "foo" {
		t.Errorf("project = %q, want %q", cfg.project, "foo")
	}
}

// ─── routeCommand routing tests ───────────────────────────────────────────────

func TestRouteCommand_sessionStart_unknown_data_dir_errors(t *testing.T) {
	// A non-absolute data-dir causes store.Open to fail — confirm the subcommand
	// surfaces the error rather than panicking.
	err := routeCommand([]string{"ion-mem", "session-start",
		"--id=x", "--project=y", "--cwd=/tmp",
		"--data-dir=relative-path"}, nil)
	if err == nil {
		t.Fatal("expected error for relative data-dir")
	}
}

func TestRouteCommand_sessionEnd_unknown_data_dir_errors(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "session-end",
		"--id=x",
		"--data-dir=relative-path"}, nil)
	if err == nil {
		t.Fatal("expected error for relative data-dir")
	}
}

func TestRouteCommand_context_unknown_data_dir_errors(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "context",
		"--project=foo",
		"--data-dir=relative-path"}, nil)
	if err == nil {
		t.Fatal("expected error for relative data-dir")
	}
}

func TestRouteCommand_savePrompt_unknown_data_dir_errors(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "save-prompt",
		"--session-id=x", "--content=y",
		"--data-dir=relative-path"}, nil)
	if err == nil {
		t.Fatal("expected error for relative data-dir")
	}
}

func TestRouteCommand_sessionStart_routes_correctly(t *testing.T) {
	dir := t.TempDir()
	err := routeCommand([]string{"ion-mem", "session-start",
		"--id=test-route-1", "--project=testproject", "--cwd=/tmp",
		"--data-dir=" + dir}, nil)
	if err != nil {
		t.Fatalf("session-start routing: %v", err)
	}
}

func TestRouteCommand_sessionEnd_routes_correctly(t *testing.T) {
	dir := t.TempDir()
	// First create a session so end doesn't error.
	if err := routeCommand([]string{"ion-mem", "session-start",
		"--id=test-route-2", "--project=testproject", "--cwd=/tmp",
		"--data-dir=" + dir}, nil); err != nil {
		t.Fatalf("session-start for setup: %v", err)
	}
	err := routeCommand([]string{"ion-mem", "session-end",
		"--id=test-route-2",
		"--data-dir=" + dir}, nil)
	if err != nil {
		t.Fatalf("session-end routing: %v", err)
	}
}

func TestRouteCommand_context_routes_correctly(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "context",
		"--project=testproject",
		"--data-dir=" + dir}, &sb)
	if err != nil {
		t.Fatalf("context routing: %v", err)
	}
}

func TestRouteCommand_savePrompt_routes_correctly(t *testing.T) {
	dir := t.TempDir()
	// Create a session first so the FK constraint is satisfied.
	if err := routeCommand([]string{"ion-mem", "session-start",
		"--id=test-route-sp", "--project=testproject", "--cwd=/tmp",
		"--data-dir=" + dir}, nil); err != nil {
		t.Fatalf("session-start for setup: %v", err)
	}
	err := routeCommand([]string{"ion-mem", "save-prompt",
		"--session-id=test-route-sp", "--content=test prompt",
		"--data-dir=" + dir}, nil)
	if err != nil {
		t.Fatalf("save-prompt routing: %v", err)
	}
}

// ─── idempotency tests ────────────────────────────────────────────────────────

func TestRouteCommand_sessionStart_idempotent(t *testing.T) {
	dir := t.TempDir()
	args := []string{"ion-mem", "session-start",
		"--id=idem-1", "--project=foo", "--cwd=/tmp",
		"--data-dir=" + dir}
	if err := routeCommand(args, nil); err != nil {
		t.Fatalf("first session-start: %v", err)
	}
	// Second call with same ID must succeed (idempotent, not error).
	if err := routeCommand(args, nil); err != nil {
		t.Fatalf("second session-start (idempotent): %v", err)
	}
}

func TestRouteCommand_sessionEnd_notFound_silent(t *testing.T) {
	dir := t.TempDir()
	// Ending a non-existent session must return nil (silent).
	err := routeCommand([]string{"ion-mem", "session-end",
		"--id=no-such-session",
		"--data-dir=" + dir}, nil)
	if err != nil {
		t.Fatalf("session-end for missing session must be silent, got: %v", err)
	}
}

// --- test helpers ---

var errFakeHomeUnavailable = stringErr("home unavailable")

type stringErr string

func (e stringErr) Error() string { return string(e) }

func fakeHome() (string, error) { return "/tmp/fake-home", nil }
