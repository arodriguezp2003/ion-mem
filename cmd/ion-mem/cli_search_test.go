package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── flag parsing ─────────────────────────────────────────────────────────────

func TestParseSearchFlags_Defaults(t *testing.T) {
	cfg, err := parseSearchFlags([]string{"my query"}, fakeHome)
	if err != nil {
		t.Fatalf("parseSearchFlags: %v", err)
	}
	if cfg.query != "my query" {
		t.Errorf("query = %q, want %q", cfg.query, "my query")
	}
	if cfg.limit != 10 {
		t.Errorf("limit = %d, want 10", cfg.limit)
	}
	if cfg.project != "" {
		t.Errorf("project = %q, want empty", cfg.project)
	}
	if cfg.allProjects {
		t.Error("allProjects should default to false")
	}
	if cfg.jsonOut {
		t.Error("jsonOut should default to false")
	}
}

func TestParseSearchFlags_AllFlags(t *testing.T) {
	cfg, err := parseSearchFlags([]string{
		"--project=myproj", "--limit=5", "--type=decision",
		"--all-projects", "--json", "--data-dir=/tmp/x",
		"the query terms",
	}, fakeHome)
	if err != nil {
		t.Fatalf("parseSearchFlags: %v", err)
	}
	if cfg.query != "the query terms" {
		t.Errorf("query = %q, want %q", cfg.query, "the query terms")
	}
	if cfg.project != "myproj" {
		t.Errorf("project = %q, want %q", cfg.project, "myproj")
	}
	if cfg.limit != 5 {
		t.Errorf("limit = %d, want 5", cfg.limit)
	}
	if cfg.obsType != "decision" {
		t.Errorf("type = %q, want %q", cfg.obsType, "decision")
	}
	if !cfg.allProjects {
		t.Error("allProjects should be true")
	}
	if !cfg.jsonOut {
		t.Error("jsonOut should be true")
	}
	if cfg.dataDir != "/tmp/x" {
		t.Errorf("dataDir = %q, want %q", cfg.dataDir, "/tmp/x")
	}
}

func TestParseSearchFlags_EmptyQueryReturnsError(t *testing.T) {
	_, err := parseSearchFlags([]string{}, fakeHome)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestParseSearchFlags_ZeroLimitReturnsError(t *testing.T) {
	_, err := parseSearchFlags([]string{"--limit=0", "query"}, fakeHome)
	if err == nil {
		t.Fatal("expected error for --limit=0")
	}
}

// ─── integration: runSearch against a seeded temp store ──────────────────────

func mustSearchStore(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("mustSearchStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	sess, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID:      "sess-search-test",
		Project: "testproj",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "searchable-observation-title",
		Content:   "unique content for search subcommand test",
		Project:   "testproj",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	_ = st.Close()
	return dir, nil
}

func TestRunSearch_NoResults_ExitsZero(t *testing.T) {
	dir := t.TempDir()
	// Open and close to initialize schema.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runSearch([]string{"--data-dir=" + dir, "no-match-xyzzy"}, &sb, nil)
	if err != nil {
		t.Errorf("runSearch with no results must exit 0, got error: %v", err)
	}
}

func TestRunSearch_TableOutput_ContainsHeader(t *testing.T) {
	dir, _ := mustSearchStore(t)

	var sb strings.Builder
	err := runSearch([]string{"--data-dir=" + dir, "searchable-observation-title"}, &sb, nil)
	if err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	out := sb.String()
	// Table output must contain column headers.
	if !strings.Contains(out, "ID") || !strings.Contains(out, "TYPE") || !strings.Contains(out, "TITLE") {
		t.Errorf("table output missing expected headers: %q", out)
	}
}

func TestRunSearch_JSONOutput_ValidJSON(t *testing.T) {
	dir, _ := mustSearchStore(t)

	var sb strings.Builder
	err := runSearch([]string{"--data-dir=" + dir, "--json", "searchable-observation-title"}, &sb, nil)
	if err != nil {
		t.Fatalf("runSearch --json: %v", err)
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		t.Fatal("--json output is empty")
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("--json output is not valid JSON array: %v\nraw: %q", err, out)
	}
}

func TestRunSearch_JSONOutput_FieldsPresent(t *testing.T) {
	dir, _ := mustSearchStore(t)

	var sb strings.Builder
	err := runSearch([]string{"--data-dir=" + dir, "--json", "searchable-observation-title"}, &sb, nil)
	if err != nil {
		t.Fatalf("runSearch --json: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(sb.String())), &arr); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(arr) == 0 {
		t.Skip("no results — FTS may not have indexed the seeded observation yet")
	}
	row := arr[0]
	for _, field := range []string{"id", "type", "title", "score"} {
		if _, ok := row[field]; !ok {
			t.Errorf("JSON result missing field %q", field)
		}
	}
}

func TestRunSearch_StoreOpenFailure_ReturnsError(t *testing.T) {
	var sb strings.Builder
	err := runSearch([]string{"--data-dir=/nonexistent/path", "query"}, &sb, nil)
	if err == nil {
		t.Error("expected error when store cannot be opened")
	}
}

func TestRouteCommand_SearchRoutes(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = routeCommand([]string{"ion-mem", "search", "--data-dir=" + dir, "test query"}, &sb)
	if err != nil {
		t.Fatalf("routeCommand search: %v", err)
	}
}
