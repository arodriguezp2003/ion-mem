package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── backup ───────────────────────────────────────────────────────────────────

func TestParseBackupFlags_defaults(t *testing.T) {
	cfg, err := parseBackupFlags([]string{}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackupFlags: %v", err)
	}
	if cfg.dataDir == "" {
		t.Error("dataDir must not be empty")
	}
	if cfg.out != "" {
		t.Error("out must be empty by default (means auto-generate)")
	}
}

func TestParseBackupFlags_customOut(t *testing.T) {
	cfg, err := parseBackupFlags([]string{"--out=/tmp/my.db"}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackupFlags: %v", err)
	}
	if cfg.out != "/tmp/my.db" {
		t.Errorf("out = %q, want /tmp/my.db", cfg.out)
	}
}

func TestRunBackup_WritesFile(t *testing.T) {
	dir := t.TempDir()
	destDir := t.TempDir()
	dest := filepath.Join(destDir, "manual.db")

	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "backup",
		"--data-dir=" + dir,
		"--out=" + dest,
	}, &sb)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	if _, statErr := os.Stat(dest); statErr != nil {
		t.Errorf("backup file not created: %v", statErr)
	}
	if !strings.Contains(sb.String(), dest) {
		t.Errorf("output did not mention dest path; got: %q", sb.String())
	}
}

func TestRunBackup_AutoDest_CreatesBackupsDir(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "backup", "--data-dir=" + dir}, &sb)
	if err != nil {
		t.Fatalf("backup auto dest: %v", err)
	}

	backupsDir := filepath.Join(dir, "backups")
	entries, readErr := os.ReadDir(backupsDir)
	if readErr != nil {
		t.Fatalf("backups dir not created: %v", readErr)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 backup file, got %d", len(entries))
	}
}

func TestRunBackup_RefusesExistingDest(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "existing.db")
	if err := os.WriteFile(dest, []byte("x"), 0o644); err != nil {
		t.Fatalf("create existing: %v", err)
	}

	err := routeCommand([]string{"ion-mem", "backup",
		"--data-dir=" + dir,
		"--out=" + dest,
	}, nil)
	if err == nil {
		t.Fatal("expected error when dest exists")
	}
}

func TestRunBackup_SetsLastAt(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(t.TempDir(), "b.db")

	if err := routeCommand([]string{"ion-mem", "backup",
		"--data-dir=" + dir,
		"--out=" + dest,
	}, nil); err != nil {
		t.Fatalf("backup: %v", err)
	}

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	val, ok, err := st.GetSetting(context.Background(), "backup.last_at")
	if err != nil || !ok || val == "" {
		t.Errorf("backup.last_at not set (ok=%v val=%q err=%v)", ok, val, err)
	}
}

// ─── export ───────────────────────────────────────────────────────────────────

func TestParseExportFlags_defaults(t *testing.T) {
	cfg, err := parseExportFlags([]string{}, fakeHome)
	if err != nil {
		t.Fatalf("parseExportFlags: %v", err)
	}
	if cfg.dataDir == "" {
		t.Error("dataDir must not be empty")
	}
}

func TestRunExport_CreatesFiles(t *testing.T) {
	dir := t.TempDir()
	outDir := t.TempDir()

	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "export",
		"--data-dir=" + dir,
		"--out=" + outDir,
	}, &sb)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	for _, name := range []string{
		"observations.jsonl", "prompts.jsonl", "sessions.jsonl",
		"revisions.jsonl", "settings.jsonl", "manifest.json",
	} {
		if _, statErr := os.Stat(filepath.Join(outDir, name)); statErr != nil {
			t.Errorf("expected file %q: %v", name, statErr)
		}
	}

	// manifest must be valid JSON.
	data, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("manifest.json not valid JSON: %v", err)
	}
}

func TestRunExport_AutoOutDir(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "export", "--data-dir=" + dir}, &sb)
	if err != nil {
		t.Fatalf("export auto out: %v", err)
	}
	// Output must mention the directory.
	out := sb.String()
	if !strings.Contains(out, "export-") {
		t.Errorf("output does not mention export dir; got: %q", out)
	}
}

// ─── prune ────────────────────────────────────────────────────────────────────

func TestParsePruneFlags_defaults(t *testing.T) {
	cfg, err := parsePruneFlags([]string{}, fakeHome)
	if err != nil {
		t.Fatalf("parsePruneFlags: %v", err)
	}
	if cfg.promptDays != 90 {
		t.Errorf("promptDays = %d, want 90", cfg.promptDays)
	}
	if cfg.deletedDays != 30 {
		t.Errorf("deletedDays = %d, want 30", cfg.deletedDays)
	}
	if cfg.apply {
		t.Error("apply must default to false (dry-run)")
	}
}

func TestRunPrune_DryRunPrintsCountsNothingDeleted(t *testing.T) {
	dir := t.TempDir()
	seedStoreWithPrunables(t, dir)

	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "prune",
		"--data-dir=" + dir,
		"--prompt-days=1",
		"--deleted-days=1",
	}, &sb)
	if err != nil {
		t.Fatalf("prune dry-run: %v", err)
	}

	out := sb.String()
	if !strings.Contains(out, "dry-run") && !strings.Contains(out, "DRY") && !strings.Contains(out, "would") {
		t.Errorf("dry-run output must indicate no changes; got: %q", out)
	}

	// Verify nothing was actually deleted.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	var pcount int
	st.DB().QueryRow("SELECT COUNT(*) FROM user_prompts").Scan(&pcount)
	if pcount == 0 {
		t.Error("prune dry-run must not delete prompts")
	}
}

func TestRunPrune_ApplyWithoutBackupRefused(t *testing.T) {
	dir := t.TempDir()
	err := routeCommand([]string{"ion-mem", "prune",
		"--data-dir=" + dir,
		"--apply",
	}, nil)
	if err == nil {
		t.Fatal("expected error when no backup exists and --apply is set")
	}
	if !strings.Contains(err.Error(), "backup") {
		t.Errorf("error must mention backup; got: %v", err)
	}
}

func TestRunPrune_ApplyAfterBackupDeletes(t *testing.T) {
	dir := t.TempDir()
	seedStoreWithPrunables(t, dir)

	// Run backup first.
	dest := filepath.Join(t.TempDir(), "b.db")
	if err := routeCommand([]string{"ion-mem", "backup",
		"--data-dir=" + dir,
		"--out=" + dest,
	}, nil); err != nil {
		t.Fatalf("backup: %v", err)
	}

	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "prune",
		"--data-dir=" + dir,
		"--prompt-days=1",
		"--deleted-days=1",
		"--apply",
	}, &sb)
	if err != nil {
		t.Fatalf("prune --apply: %v", err)
	}

	// The old prompt must be gone.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	var oldCount int
	st.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_prompts WHERE content='old-prompt'",
	).Scan(&oldCount)
	if oldCount != 0 {
		t.Error("old prompt not deleted after --apply")
	}

	// Fresh prompt must survive.
	var freshCount int
	st.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_prompts WHERE content='fresh-prompt'",
	).Scan(&freshCount)
	if freshCount != 1 {
		t.Error("fresh prompt must survive prune")
	}
}

// seedStoreWithPrunables creates a store with an old prompt and a recently
// soft-deleted observation for prune tests.
func seedStoreWithPrunables(t *testing.T, dataDir string) {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("seedStore open: %v", err)
	}
	defer st.Close()

	sess, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID: "seed-sess", Project: "seed-proj", Directory: "/tmp/seed",
	})
	if err != nil {
		t.Fatalf("seedStore create session: %v", err)
	}

	// Old prompt (backdated).
	if _, err := st.DB().ExecContext(ctx,
		`INSERT INTO user_prompts (sync_id, session_id, content, project, created_at)
		 VALUES ('pr-old', ?, 'old-prompt', 'seed-proj', '2020-01-01T00:00:00Z')`,
		sess.ID,
	); err != nil {
		t.Fatalf("seedStore insert old prompt: %v", err)
	}

	// Fresh prompt.
	if _, err := st.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "fresh-prompt", Project: "seed-proj",
	}); err != nil {
		t.Fatalf("seedStore insert fresh prompt: %v", err)
	}
}

// ─── config ───────────────────────────────────────────────────────────────────

func TestParseConfigFlags_requiresSubcmd(t *testing.T) {
	_, err := parseConfigFlags([]string{}, fakeHome)
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunConfig_GetSetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder

	// set
	err := routeCommand([]string{"ion-mem", "config", "set",
		"embeddings.enabled", "true",
		"--data-dir=" + dir,
	}, &sb)
	if err != nil {
		t.Fatalf("config set: %v", err)
	}

	// get
	sb.Reset()
	err = routeCommand([]string{"ion-mem", "config", "get",
		"embeddings.enabled",
		"--data-dir=" + dir,
	}, &sb)
	if err != nil {
		t.Fatalf("config get: %v", err)
	}
	if !strings.Contains(sb.String(), "true") {
		t.Errorf("config get output = %q, want 'true'", sb.String())
	}
}

func TestRunConfig_List(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "config", "list", "--data-dir=" + dir}, &sb)
	if err != nil {
		t.Fatalf("config list: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "embeddings.enabled") {
		t.Errorf("config list must show known keys; got: %q", out)
	}
}

func TestRunConfig_UnknownKeyRejected(t *testing.T) {
	dir := t.TempDir()
	err := routeCommand([]string{"ion-mem", "config", "set",
		"bogus.key", "value",
		"--data-dir=" + dir,
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "valid") {
		t.Errorf("error must list valid keys; got: %v", err)
	}
}

func TestRunConfig_MissingArgsError(t *testing.T) {
	dir := t.TempDir()
	// get without key
	err := routeCommand([]string{"ion-mem", "config", "get", "--data-dir=" + dir}, nil)
	if err == nil {
		t.Fatal("expected error: config get requires <key>")
	}
	// set without value
	err = routeCommand([]string{"ion-mem", "config", "set", "embeddings.enabled", "--data-dir=" + dir}, nil)
	if err == nil {
		t.Fatal("expected error: config set requires <key> <value>")
	}
}

// ─── project rename ───────────────────────────────────────────────────────────

func TestRunProject_RenameDryRun(t *testing.T) {
	dir := t.TempDir()
	seedProjectData(t, dir, "alpha")

	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "project", "rename",
		"alpha", "beta",
		"--data-dir=" + dir,
	}, &sb)
	if err != nil {
		t.Fatalf("project rename dry-run: %v", err)
	}

	// Dry-run: nothing renamed.
	st, openErr := store.Open(dir)
	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}
	defer st.Close()
	var c int
	st.DB().QueryRow("SELECT COUNT(*) FROM sessions WHERE project='alpha'").Scan(&c)
	if c == 0 {
		t.Error("dry-run must not rename; project 'alpha' must still exist")
	}
	out := sb.String()
	if !strings.Contains(out, "dry-run") && !strings.Contains(out, "DRY") && !strings.Contains(out, "would") {
		t.Errorf("output must indicate dry-run; got: %q", out)
	}
}

func TestRunProject_RenameApply(t *testing.T) {
	dir := t.TempDir()
	seedProjectData(t, dir, "alpha")

	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "project", "rename",
		"alpha", "beta",
		"--data-dir=" + dir,
		"--apply",
	}, &sb)
	if err != nil {
		t.Fatalf("project rename --apply: %v", err)
	}

	st, openErr := store.Open(dir)
	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}
	defer st.Close()

	var alphaCount int
	st.DB().QueryRow("SELECT COUNT(*) FROM sessions WHERE project='alpha'").Scan(&alphaCount)
	if alphaCount != 0 {
		t.Errorf("'alpha' must be gone after rename, still has %d rows", alphaCount)
	}
}

func TestRunProject_RenameNotFound(t *testing.T) {
	dir := t.TempDir()
	err := routeCommand([]string{"ion-mem", "project", "rename",
		"ghost", "whatever",
		"--data-dir=" + dir,
		"--apply",
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-existent project")
	}
}

func TestRunProject_RenameEmptyNamesError(t *testing.T) {
	dir := t.TempDir()
	// Missing args entirely
	err := routeCommand([]string{"ion-mem", "project", "rename", "--data-dir=" + dir}, nil)
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func seedProjectData(t *testing.T, dataDir, project string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("seedProjectData open: %v", err)
	}
	defer st.Close()

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID: "sess-" + project, Project: project, Directory: "/tmp/" + project,
	}); err != nil {
		t.Fatalf("seedProjectData create session: %v", err)
	}
}

// ─── session-end --all-stale ──────────────────────────────────────────────────

func TestRunSessionEnd_AllStale_EndsOnlyStale(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	// Stale session (backdated).
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID: "stale", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("create stale: %v", err)
	}
	st.DB().ExecContext(ctx, "UPDATE sessions SET started_at='2020-01-01T00:00:00Z' WHERE id='stale'")

	// Fresh active session — must survive.
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID: "fresh", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	st.Close()

	var sb strings.Builder
	err = routeCommand([]string{"ion-mem", "session-end",
		"--all-stale",
		"--data-dir=" + dir,
	}, &sb)
	if err != nil {
		t.Fatalf("session-end --all-stale: %v", err)
	}

	st2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer st2.Close()

	stale, _ := st2.GetSession(context.Background(), "stale")
	if stale.Status != "ended" {
		t.Errorf("stale status = %q, want ended", stale.Status)
	}

	fresh, _ := st2.GetSession(context.Background(), "fresh")
	if fresh.Status != "active" {
		t.Errorf("fresh status = %q, want active", fresh.Status)
	}

	out := sb.String()
	if !strings.Contains(out, "1") {
		t.Errorf("output must mention count; got: %q", out)
	}
}

func TestParseSessionEndFlags_allStaleFlag(t *testing.T) {
	cfg, err := parseSessionEndFlags([]string{"--all-stale"}, fakeHome)
	if err != nil {
		t.Fatalf("parseSessionEndFlags: %v", err)
	}
	if !cfg.allStale {
		t.Error("allStale must be true")
	}
}

func TestParseSessionEndFlags_olderThanCustom(t *testing.T) {
	cfg, err := parseSessionEndFlags([]string{"--all-stale", "--older-than=48h"}, fakeHome)
	if err != nil {
		t.Fatalf("parseSessionEndFlags: %v", err)
	}
	if cfg.olderThan.Hours() != 48 {
		t.Errorf("olderThan = %v, want 48h", cfg.olderThan)
	}
}
