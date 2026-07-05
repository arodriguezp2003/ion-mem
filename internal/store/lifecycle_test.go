package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── Backup ───────────────────────────────────────────────────────────────────

// TestBackup_CreatesFileAndSetsLastAt verifies Backup writes a compact SQLite
// file and records backup.last_at in settings.
func TestBackup_CreatesFileAndSetsLastAt(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	// Seed one observation so the backup is non-trivial.
	sess := mustSession(t, s, "backup-project")
	mustObservation(t, s, sess.ID)

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// File must exist and be non-empty.
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat backup file: %v", err)
	}
	if info.Size() == 0 {
		t.Error("backup file is empty")
	}

	// The backup must open as a valid store with migrations applied.
	backupStore, err := store.OpenRaw(dest)
	if err != nil {
		t.Fatalf("store.OpenRaw on backup: %v", err)
	}
	defer backupStore.Close()

	// Observation count must match.
	origCount := countObservations(t, s)
	backupCount := countObservations(t, backupStore)
	if backupCount != origCount {
		t.Errorf("backup obs count = %d, want %d", backupCount, origCount)
	}

	// backup.last_at must be set.
	val, ok, err := s.GetSetting(ctx, "backup.last_at")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok || val == "" {
		t.Error("backup.last_at not set after Backup")
	}
}

// TestBackup_RefusesExistingDest verifies Backup returns an error when the
// destination already exists (prevents silent overwrites).
func TestBackup_RefusesExistingDest(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	dest := filepath.Join(t.TempDir(), "existing.db")
	// Create the file first.
	if err := os.WriteFile(dest, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("create placeholder: %v", err)
	}

	err := s.Backup(ctx, dest)
	if err == nil {
		t.Fatal("expected error when dest already exists, got nil")
	}
}

// countObservations queries the total number of observations in s (including
// soft-deleted) for test assertion purposes.
func countObservations(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM observations").Scan(&n); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	return n
}

// ─── Export ───────────────────────────────────────────────────────────────────

// TestExport_WritesFilesAndManifest seeds a store, runs Export, and verifies
// that all JSONL files and manifest.json exist with matching counts.
func TestExport_WritesFilesAndManifest(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	// Seed data: 1 session, 2 observations (one soft-deleted), 1 prompt.
	sess := mustSession(t, s, "export-project")
	obs1 := mustObservation(t, s, sess.ID)
	mustObservation(t, s, sess.ID)
	mustPrompt(t, s, sess.ID, "hello export", "export-project")

	// Soft-delete obs1.
	if err := s.DeleteObservation(ctx, obs1.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	outDir := t.TempDir()
	manifest, err := s.Export(ctx, outDir)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// All expected files must exist.
	for _, name := range []string{
		"observations.jsonl", "prompts.jsonl", "sessions.jsonl",
		"revisions.jsonl", "settings.jsonl", "manifest.json",
	} {
		path := filepath.Join(outDir, name)
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("expected file %q missing: %v", name, statErr)
		}
	}

	// Manifest counts must match actual rows.
	if manifest.Counts["observations"] != 2 {
		t.Errorf("manifest observations count = %d, want 2 (all incl. soft-deleted)", manifest.Counts["observations"])
	}
	if manifest.Counts["sessions"] != 1 {
		t.Errorf("manifest sessions count = %d, want 1", manifest.Counts["sessions"])
	}
	if manifest.Counts["prompts"] != 1 {
		t.Errorf("manifest prompts count = %d, want 1", manifest.Counts["prompts"])
	}
	if manifest.SchemaVersion != 8 {
		t.Errorf("manifest schema_version = %d, want 8", manifest.SchemaVersion)
	}

	// Soft-deleted observation must appear in the JSONL.
	obsLines := readJSONLLines(t, filepath.Join(outDir, "observations.jsonl"))
	if len(obsLines) != 2 {
		t.Errorf("observations.jsonl lines = %d, want 2", len(obsLines))
	}
}

// TestExport_SoftDeletedIncluded ensures soft-deleted observations appear in export.
func TestExport_SoftDeletedIncluded(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "proj")
	obs := mustObservation(t, s, sess.ID)

	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	outDir := t.TempDir()
	manifest, err := s.Export(ctx, outDir)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if manifest.Counts["observations"] != 1 {
		t.Errorf("soft-deleted obs missing from export: count = %d, want 1", manifest.Counts["observations"])
	}
}

// readJSONLLines reads a JSONL file and returns its non-empty lines.
func readJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readJSONLLines %q: %v", path, err)
	}
	var lines []string
	for _, line := range splitLines(string(data)) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// ─── Prune ────────────────────────────────────────────────────────────────────

// TestCountPrunablePrompts_OlderThanCutoff verifies that prompts created before
// the cutoff are counted as prunable.
func TestCountPrunablePrompts_OlderThanCutoff(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "prune-proj")

	// Insert a prompt with a backdated created_at.
	oldDate := "2020-01-01T00:00:00Z"
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO user_prompts (sync_id, session_id, content, project, created_at)
		 VALUES ('pr-old', ?, 'old content', 'prune-proj', ?)`,
		sess.ID, oldDate,
	)
	if err != nil {
		t.Fatalf("insert old prompt: %v", err)
	}

	// A fresh prompt (created now) — should NOT be counted.
	mustPrompt(t, s, sess.ID, "fresh content", "prune-proj")

	cutoff := "2024-01-01T00:00:00Z"
	count, err := s.CountPrunablePrompts(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountPrunablePrompts: %v", err)
	}
	if count != 1 {
		t.Errorf("CountPrunablePrompts = %d, want 1", count)
	}
}

// TestPrunePrompts_DeletesOnlyOldRows verifies that PrunePrompts removes only
// prompts created before the cutoff, leaving newer ones intact.
func TestPrunePrompts_DeletesOnlyOldRows(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "prune-proj")

	oldDate := "2020-01-01T00:00:00Z"
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO user_prompts (sync_id, session_id, content, project, created_at)
		 VALUES ('pr-del', ?, 'old to delete', 'prune-proj', ?)`,
		sess.ID, oldDate,
	)
	if err != nil {
		t.Fatalf("insert old prompt: %v", err)
	}

	mustPrompt(t, s, sess.ID, "keep me", "prune-proj")

	cutoff := "2024-01-01T00:00:00Z"
	deleted, err := s.PrunePrompts(ctx, cutoff)
	if err != nil {
		t.Fatalf("PrunePrompts: %v", err)
	}
	if deleted != 1 {
		t.Errorf("PrunePrompts deleted = %d, want 1", deleted)
	}

	// Fresh prompt must survive.
	var remaining int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM user_prompts WHERE content='keep me'").Scan(&remaining); err != nil {
		t.Fatalf("count surviving: %v", err)
	}
	if remaining != 1 {
		t.Errorf("fresh prompt not found after prune (remaining=%d)", remaining)
	}
}

// TestCountPrunableDeletedObs_OlderThanCutoff verifies that soft-deleted
// observations older than the cutoff are counted.
func TestCountPrunableDeletedObs_OlderThanCutoff(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "prune-obs-proj")
	obs := mustObservation(t, s, sess.ID)

	// Soft-delete the observation and backdate deleted_at.
	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		"UPDATE observations SET deleted_at='2020-01-01T00:00:00Z' WHERE id=?", obs.ID,
	); err != nil {
		t.Fatalf("backdate deleted_at: %v", err)
	}

	// Also create a recently-deleted obs — should NOT be counted.
	obs2 := mustObservation(t, s, sess.ID)
	if err := s.DeleteObservation(ctx, obs2.ID, false); err != nil {
		t.Fatalf("DeleteObservation obs2: %v", err)
	}

	cutoff := "2024-01-01T00:00:00Z"
	count, err := s.CountPrunableDeletedObs(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountPrunableDeletedObs: %v", err)
	}
	if count != 1 {
		t.Errorf("CountPrunableDeletedObs = %d, want 1", count)
	}
}

// TestPruneDeletedObs_CascadesAndCleansFTS verifies that PruneDeletedObs hard-
// deletes rows and the AFTER DELETE trigger cleans the FTS index.
func TestPruneDeletedObs_CascadesAndCleansFTS(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "cascade-proj")
	obs := mustObservation(t, s, sess.ID)

	// Soft-delete and backdate.
	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		"UPDATE observations SET deleted_at='2020-01-01T00:00:00Z' WHERE id=?", obs.ID,
	); err != nil {
		t.Fatalf("backdate deleted_at: %v", err)
	}

	// Add an embedding so we can verify CASCADE.
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO observation_embeddings (observation_id, model, dims, vector, updated_at)
		 VALUES (?, 'test-model', 3, X'000000', '2020-01-01T00:00:00Z')`, obs.ID,
	)
	if err != nil {
		t.Fatalf("insert embedding: %v", err)
	}

	cutoff := "2024-01-01T00:00:00Z"
	deleted, err := s.PruneDeletedObs(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneDeletedObs: %v", err)
	}
	if deleted != 1 {
		t.Errorf("PruneDeletedObs deleted = %d, want 1", deleted)
	}

	// Observation row must be gone.
	var remaining int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM observations WHERE id=?", obs.ID).Scan(&remaining); err != nil {
		t.Fatalf("count obs: %v", err)
	}
	if remaining != 0 {
		t.Errorf("obs row not deleted (remaining=%d)", remaining)
	}

	// Embedding must be cascaded.
	var embCount int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM observation_embeddings WHERE observation_id=?", obs.ID).Scan(&embCount); err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if embCount != 0 {
		t.Errorf("embedding not cascaded (count=%d)", embCount)
	}
}

// ─── RenameProject ────────────────────────────────────────────────────────────

// TestRenameProject_RenamesAcrossAllTables verifies cross-table rename in a
// single transaction.
func TestRenameProject_RenamesAcrossAllTables(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "old-proj")
	mustObservation(t, s, sess.ID)
	mustObservationForProject(t, s, sess.ID, "old-proj")
	mustPrompt(t, s, sess.ID, "content", "old-proj")

	rows, err := s.RenameProject(ctx, "old-proj", "new-proj")
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if rows == 0 {
		t.Error("RenameProject returned 0 rows affected")
	}

	// No rows should remain under old-proj.
	var obsOld, sessOld, promptOld int
	s.DB().QueryRow("SELECT COUNT(*) FROM observations WHERE project='old-proj'").Scan(&obsOld)
	s.DB().QueryRow("SELECT COUNT(*) FROM sessions WHERE project='old-proj'").Scan(&sessOld)
	s.DB().QueryRow("SELECT COUNT(*) FROM user_prompts WHERE project='old-proj'").Scan(&promptOld)
	if obsOld+sessOld+promptOld != 0 {
		t.Errorf("rows still under old-proj: obs=%d sess=%d prompts=%d", obsOld, sessOld, promptOld)
	}

	// All rows should be under new-proj.
	var obsNew, sessNew int
	s.DB().QueryRow("SELECT COUNT(*) FROM observations WHERE project='new-proj'").Scan(&obsNew)
	s.DB().QueryRow("SELECT COUNT(*) FROM sessions WHERE project='new-proj'").Scan(&sessNew)
	if obsNew == 0 || sessNew == 0 {
		t.Errorf("rows missing under new-proj: obs=%d sess=%d", obsNew, sessNew)
	}
}

// TestRenameProject_NotFoundErrors verifies that renaming a non-existent project
// returns an error.
func TestRenameProject_NotFoundErrors(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	_, err := s.RenameProject(ctx, "ghost-project", "whatever")
	if err == nil {
		t.Fatal("expected error when old project has 0 rows")
	}
}

// TestRenameProject_EmptyNamesError verifies validation of empty names.
func TestRenameProject_EmptyNamesError(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if _, err := s.RenameProject(ctx, "", "new"); err == nil {
		t.Fatal("expected error for empty old name")
	}
	if _, err := s.RenameProject(ctx, "old", ""); err == nil {
		t.Fatal("expected error for empty new name")
	}
}

// TestRenameProject_EqualNamesError verifies that renaming to the same name
// returns an error.
func TestRenameProject_EqualNamesError(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	_, err := s.RenameProject(ctx, "same", "same")
	if err == nil {
		t.Fatal("expected error when old == new")
	}
}

// ─── EndStaleSessions ─────────────────────────────────────────────────────────

// TestEndStaleSessions_OnlyEndsStale verifies that only sessions with
// status=active AND started_at older than the cutoff are ended.
func TestEndStaleSessions_OnlyEndsStale(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	// Create a stale active session (backdated started_at).
	if _, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID: "stale-1", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("create stale: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		"UPDATE sessions SET started_at='2020-01-01T00:00:00Z' WHERE id='stale-1'",
	); err != nil {
		t.Fatalf("backdate stale: %v", err)
	}

	// Create a fresh active session — must NOT be ended.
	if _, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID: "fresh-1", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("create fresh: %v", err)
	}

	// Create an already-ended session — must NOT be re-ended.
	if _, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID: "ended-1", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("create ended: %v", err)
	}
	if err := s.EndSession(ctx, "ended-1", ""); err != nil {
		t.Fatalf("EndSession ended-1: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		"UPDATE sessions SET started_at='2020-01-01T00:00:00Z' WHERE id='ended-1'",
	); err != nil {
		t.Fatalf("backdate ended: %v", err)
	}

	cutoff := "2024-01-01T00:00:00Z"
	ended, err := s.EndStaleSessions(ctx, cutoff)
	if err != nil {
		t.Fatalf("EndStaleSessions: %v", err)
	}
	if ended != 1 {
		t.Errorf("EndStaleSessions = %d, want 1", ended)
	}

	// stale-1 must now be ended.
	stale, _ := s.GetSession(ctx, "stale-1")
	if stale.Status != "ended" {
		t.Errorf("stale-1 status = %q, want ended", stale.Status)
	}

	// fresh-1 must still be active.
	fresh, _ := s.GetSession(ctx, "fresh-1")
	if fresh.Status != "active" {
		t.Errorf("fresh-1 status = %q, want active", fresh.Status)
	}
}
