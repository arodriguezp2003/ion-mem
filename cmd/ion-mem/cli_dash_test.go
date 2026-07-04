package main

import (
	"strings"
	"testing"
)

// TestParseDashFlags_defaults verifies the dash subcommand accepts --data-dir.
func TestParseDashFlags_defaults(t *testing.T) {
	cfg, err := parseDashFlags(nil, fakeHome)
	if err != nil {
		t.Fatalf("parseDashFlags: %v", err)
	}
	if !strings.HasSuffix(cfg.dataDir, "/.ion-mem") {
		t.Errorf("dataDir = %q, want suffix /.ion-mem", cfg.dataDir)
	}
}

func TestParseDashFlags_explicitDataDir(t *testing.T) {
	cfg, err := parseDashFlags([]string{"--data-dir=/tmp/custom"}, fakeHome)
	if err != nil {
		t.Fatalf("parseDashFlags: %v", err)
	}
	if cfg.dataDir != "/tmp/custom" {
		t.Errorf("dataDir = %q, want %q", cfg.dataDir, "/tmp/custom")
	}
}

func TestParseDashFlags_unknownFlagErrors(t *testing.T) {
	_, err := parseDashFlags([]string{"--bogus=value"}, fakeHome)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

// TestRouteCommand_dashNonTTY verifies that `ion-mem dash` on a non-TTY writer
// exits with a clear error (not a hang) because the output is not a terminal.
func TestRouteCommand_dashNonTTY(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "dash", "--data-dir=" + dir}, &sb)
	// Must return an error on non-TTY — specifically "not a terminal" or similar.
	if err == nil {
		t.Fatal("expected error when stdout is not a TTY")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error = %q, want it to mention 'terminal'", err.Error())
	}
}

// TestRouteCommand_dashUsageInHelp verifies that usage output mentions the dash command.
func TestRouteCommand_dashUsageInHelp(t *testing.T) {
	var sb strings.Builder
	_ = routeCommand([]string{"ion-mem", "help"}, &sb)
	out := sb.String()
	if !strings.Contains(out, "dash") {
		t.Errorf("usage output does not mention 'dash': %s", out)
	}
}

// TestRouteCommand_bareNonTTY verifies that bare `ion-mem` on a non-TTY
// keeps its current behavior: shows usage and returns an error.
func TestRouteCommand_bareNonTTY(t *testing.T) {
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem"}, &sb)
	if err == nil {
		t.Fatal("bare ion-mem on non-TTY should return error")
	}
	if !strings.Contains(sb.String(), "Usage:") {
		t.Errorf("bare non-TTY should print Usage:, got: %s", sb.String())
	}
}
