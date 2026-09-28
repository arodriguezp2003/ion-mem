package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseStatusFlags_defaults(t *testing.T) {
	cfg, err := parseStatusFlags(nil, fakeHome)
	if err != nil {
		t.Fatalf("parseStatusFlags: %v", err)
	}
	if cfg.limit != 5 {
		t.Errorf("limit = %d, want 5", cfg.limit)
	}
	if !strings.HasSuffix(cfg.dataDir, "/.ion-mem") {
		t.Errorf("dataDir = %q, want suffix /.ion-mem", cfg.dataDir)
	}
}

func TestParseStatusFlags_all(t *testing.T) {
	cfg, err := parseStatusFlags([]string{"--data-dir=/tmp/x", "--limit=12"}, fakeHome)
	if err != nil {
		t.Fatalf("parseStatusFlags: %v", err)
	}
	if cfg.dataDir != "/tmp/x" {
		t.Errorf("dataDir = %q, want /tmp/x", cfg.dataDir)
	}
	if cfg.limit != 12 {
		t.Errorf("limit = %d, want 12", cfg.limit)
	}
}

func TestParseStatusFlags_zeroLimitErrors(t *testing.T) {
	_, err := parseStatusFlags([]string{"--limit=0"}, fakeHome)
	if err == nil {
		t.Fatal("expected error for --limit=0")
	}
}

func TestParseStatusFlags_negativeLimitErrors(t *testing.T) {
	_, err := parseStatusFlags([]string{"--limit=-3"}, fakeHome)
	if err == nil {
		t.Fatal("expected error for negative --limit")
	}
}

func TestRouteCommand_status_unknown_data_dir_errors(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "status", "--data-dir=relative-path"}, nil)
	if err == nil {
		t.Fatal("expected error for relative data-dir")
	}
}

func TestRouteCommand_status_routes_correctly(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "status", "--data-dir=" + dir}, &sb)
	if err != nil {
		t.Fatalf("status routing: %v", err)
	}
	out := sb.String()
	// Fresh empty store: should emit headers + the "no observations" alert.
	for _, want := range []string{
		"ion-mem status — health snapshot",
		"Stats",
		"Recent observations",
		"Active sessions",
		"Last captured prompt",
		"Alerts",
		"no observations saved yet",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing section %q in status output:\n%s", want, out)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 6, 7, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		iso, want string
	}{
		{now.Format(time.RFC3339Nano), "just now"},
		{now.Add(-30 * time.Second).Format(time.RFC3339Nano), "just now"},
		{now.Add(-5 * time.Minute).Format(time.RFC3339Nano), "5m ago"},
		{now.Add(-2 * time.Hour).Format(time.RFC3339Nano), "2h ago"},
		{now.Add(-49 * time.Hour).Format(time.RFC3339Nano), "2d ago"},
		// SQLite default datetime('now') format — space, no T, no timezone.
		{now.Add(-3 * time.Hour).UTC().Format("2006-01-02 15:04:05"), "3h ago"},
		{"", "?"},
		{"not-a-date", "not-a-date"},
	}
	for _, c := range cases {
		got := relativeTime(now, c.iso)
		if got != c.want {
			t.Errorf("relativeTime(%q) = %q, want %q", c.iso, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell…"},
		{"a", 1, "a"},
		{"abc", 0, ""},
	}
	for _, c := range cases {
		got := truncate(c.in, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1024 * 1024, "1.0MB"},
		{int64(1024) * 1024 * 1024, "1.0GB"},
	}
	for _, c := range cases {
		got := humanBytes(c.n)
		if got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
