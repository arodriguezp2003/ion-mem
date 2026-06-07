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

// --- test helpers ---

var errFakeHomeUnavailable = stringErr("home unavailable")

type stringErr string

func (e stringErr) Error() string { return string(e) }

func fakeHome() (string, error) { return "/tmp/fake-home", nil }
