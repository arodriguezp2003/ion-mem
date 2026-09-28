package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── flag parsing ─────────────────────────────────────────────────────────────

func TestParseExportProjectFlags_defaults(t *testing.T) {
	cfg, err := parseExportProjectFlags([]string{}, fakeHome)
	if err != nil {
		t.Fatalf("parseExportProjectFlags: %v", err)
	}
	if cfg.project != "" {
		t.Errorf("project = %q, want empty (auto-detect)", cfg.project)
	}
	if cfg.out != "" {
		t.Errorf("out = %q, want empty (auto-generate)", cfg.out)
	}
	if cfg.withPrompts || cfg.includeDeleted || cfg.allowSecrets {
		t.Error("all boolean flags must default to false")
	}
	if cfg.dataDir == "" {
		t.Error("dataDir must not be empty")
	}
}

func TestParseExportProjectFlags_explicit(t *testing.T) {
	cfg, err := parseExportProjectFlags([]string{
		"--project=demo", "--out=/tmp/demo.ionmem.zip",
		"--with-prompts", "--include-deleted", "--allow-secrets",
	}, fakeHome)
	if err != nil {
		t.Fatalf("parseExportProjectFlags: %v", err)
	}
	if cfg.project != "demo" || cfg.out != "/tmp/demo.ionmem.zip" {
		t.Errorf("cfg = %+v", cfg)
	}
	if !cfg.withPrompts || !cfg.includeDeleted || !cfg.allowSecrets {
		t.Errorf("cfg = %+v, want all booleans true", cfg)
	}
}

func TestParseImportProjectFlags_requiresFile(t *testing.T) {
	_, err := parseImportProjectFlags([]string{"--apply"}, fakeHome)
	if err == nil {
		t.Fatal("expected error when <file> is missing")
	}
}

func TestParseImportProjectFlags_parsesPositionalAndFlags(t *testing.T) {
	cfg, err := parseImportProjectFlags([]string{
		"bundle.ionmem.zip", "--project=demo", "--apply", "--prefer-bundle", "--with-prompts",
	}, fakeHome)
	if err != nil {
		t.Fatalf("parseImportProjectFlags: %v", err)
	}
	if cfg.file != "bundle.ionmem.zip" {
		t.Errorf("file = %q, want bundle.ionmem.zip", cfg.file)
	}
	if cfg.project != "demo" || !cfg.apply || !cfg.preferBundle || !cfg.withPrompts {
		t.Errorf("cfg = %+v", cfg)
	}
}

// ─── export-project / import-project integration ─────────────────────────────

// seedProject creates a store at dataDir with one observation under project.
func seedProject(t *testing.T, dataDir, project string) {
	t.Helper()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	sess, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-" + project, Project: project, Directory: "/tmp/" + project})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "Use widgets",
		Content:   "We decided to use widgets for the thing.",
		Project:   project,
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
}

func TestExportProjectCLI_WritesBundleAndSummary(t *testing.T) {
	dataDir := t.TempDir()
	seedProject(t, dataDir, "demo")

	outFile := filepath.Join(t.TempDir(), "demo.ionmem.zip")
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "export-project",
		"--project=demo", "--out=" + outFile, "--data-dir=" + dataDir,
	}, &sb)
	if err != nil {
		t.Fatalf("export-project: %v", err)
	}

	if _, statErr := os.Stat(outFile); statErr != nil {
		t.Fatalf("bundle file not created: %v", statErr)
	}
	if !strings.Contains(sb.String(), "observations: 1") {
		t.Errorf("summary missing observation count; got: %s", sb.String())
	}
}

func TestExportProjectCLI_RefusesSecretsWithoutFlag(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	ctx := context.Background()
	sess, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-demo", Project: "demo", Directory: "/tmp/demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "config",
		Title:     "aws creds",
		Content:   "export AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP",
		Project:   "demo",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	st.Close()

	outFile := filepath.Join(t.TempDir(), "demo.ionmem.zip")
	var sb strings.Builder
	err = routeCommand([]string{"ion-mem", "export-project",
		"--project=demo", "--out=" + outFile, "--data-dir=" + dataDir,
	}, &sb)
	if err == nil {
		t.Fatal("expected error when secrets are found without --allow-secrets")
	}
	if _, statErr := os.Stat(outFile); statErr == nil {
		t.Error("bundle file must not be written when export is refused")
	}

	// --allow-secrets must let it through.
	var sb2 strings.Builder
	err = routeCommand([]string{"ion-mem", "export-project",
		"--project=demo", "--out=" + outFile, "--data-dir=" + dataDir, "--allow-secrets",
	}, &sb2)
	if err != nil {
		t.Fatalf("export-project with --allow-secrets: %v", err)
	}
	if _, statErr := os.Stat(outFile); statErr != nil {
		t.Errorf("bundle file must be written with --allow-secrets: %v", statErr)
	}
}

func TestImportProjectCLI_DryRunWritesNothing(t *testing.T) {
	srcDir := t.TempDir()
	seedProject(t, srcDir, "demo")

	bundleFile := filepath.Join(t.TempDir(), "demo.ionmem.zip")
	if err := routeCommand([]string{"ion-mem", "export-project",
		"--project=demo", "--out=" + bundleFile, "--data-dir=" + srcDir,
	}, nil); err != nil {
		t.Fatalf("export-project: %v", err)
	}

	dstDir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "import-project", bundleFile,
		"--project=demo", "--data-dir=" + dstDir,
	}, &sb)
	if err != nil {
		t.Fatalf("import-project dry-run: %v", err)
	}
	if !strings.Contains(sb.String(), "DRY-RUN") {
		t.Errorf("output must mention DRY-RUN; got: %s", sb.String())
	}
	if !strings.Contains(sb.String(), "Inserted: 1") {
		t.Errorf("output must report Inserted: 1; got: %s", sb.String())
	}

	dst, err := store.Open(dstDir)
	if err != nil {
		t.Fatalf("store.Open dst: %v", err)
	}
	defer dst.Close()
	obs, err := dst.RecentObservations(context.Background(), store.RecentObservationsParams{Project: "demo"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 0 {
		t.Errorf("dry-run must not write: found %d observations", len(obs))
	}
}

func TestImportProjectCLI_ApplyRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	seedProject(t, srcDir, "demo")

	bundleFile := filepath.Join(t.TempDir(), "demo.ionmem.zip")
	if err := routeCommand([]string{"ion-mem", "export-project",
		"--project=demo", "--out=" + bundleFile, "--data-dir=" + srcDir,
	}, nil); err != nil {
		t.Fatalf("export-project: %v", err)
	}

	dstDir := t.TempDir()
	var sb strings.Builder
	err := routeCommand([]string{"ion-mem", "import-project", bundleFile,
		"--project=demo", "--apply", "--data-dir=" + dstDir,
	}, &sb)
	if err != nil {
		t.Fatalf("import-project --apply: %v", err)
	}
	if !strings.Contains(sb.String(), "Backup written to:") {
		t.Errorf("output must mention backup; got: %s", sb.String())
	}
	if !strings.Contains(sb.String(), "backfill-embeddings") {
		t.Errorf("output must hint backfill-embeddings; got: %s", sb.String())
	}

	dst, err := store.Open(dstDir)
	if err != nil {
		t.Fatalf("store.Open dst: %v", err)
	}
	defer dst.Close()
	obs, err := dst.RecentObservations(context.Background(), store.RecentObservationsParams{Project: "demo"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("dst observations = %d, want 1", len(obs))
	}

	backupsDir := filepath.Join(dstDir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil || len(entries) != 1 {
		t.Errorf("expected 1 pre-import backup file, err=%v entries=%d", err, len(entries))
	}
}

func TestImportProjectCLI_ConflictReportedByDefault(t *testing.T) {
	srcDir := t.TempDir()
	seedProject(t, srcDir, "demo")

	bundleFile := filepath.Join(t.TempDir(), "demo.ionmem.zip")
	if err := routeCommand([]string{"ion-mem", "export-project",
		"--project=demo", "--out=" + bundleFile, "--data-dir=" + srcDir,
	}, nil); err != nil {
		t.Fatalf("export-project: %v", err)
	}

	dstDir := t.TempDir()
	// First apply: inserts the row.
	if err := routeCommand([]string{"ion-mem", "import-project", bundleFile,
		"--project=demo", "--apply", "--data-dir=" + dstDir,
	}, nil); err != nil {
		t.Fatalf("first import-project --apply: %v", err)
	}

	// Locally diverge the row so the next import sees a conflict.
	dst, err := store.Open(dstDir)
	if err != nil {
		t.Fatalf("store.Open dst: %v", err)
	}
	ctx := context.Background()
	obs, err := dst.RecentObservations(ctx, store.RecentObservationsParams{Project: "demo"})
	if err != nil || len(obs) != 1 {
		t.Fatalf("RecentObservations: %v (len=%d)", err, len(obs))
	}
	newTitle := "locally edited title"
	if _, err := dst.UpdateObservation(ctx, obs[0].ID, store.UpdateObservationParams{Title: &newTitle}); err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}
	dst.Close()

	// Re-importing the same (unchanged) bundle now conflicts with the diverged local row.
	var sb strings.Builder
	err = routeCommand([]string{"ion-mem", "import-project", bundleFile,
		"--project=demo", "--data-dir=" + dstDir,
	}, &sb)
	if err != nil {
		t.Fatalf("second import-project dry-run: %v", err)
	}
	if !strings.Contains(sb.String(), "Conflicts (1)") {
		t.Errorf("expected 1 conflict reported; got: %s", sb.String())
	}
}

// TestImportProjectCLI_StatusDowngradesReported pins the status-downgrade block
// in the shared dry-run/apply report. A downgraded row is imported with its
// content but WITHOUT the status the bundle asked for, so the local copy now
// differs from the sender's — printing it is the only way the operator learns
// that. The bundle is built by hand because a live store would never let a
// permanent type be marked obsolete in the first place.
func TestImportProjectCLI_StatusDowngradesReported(t *testing.T) {
	bundleFile := filepath.Join(t.TempDir(), "downgrade.ionmem.zip")
	f, err := os.Create(bundleFile)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	err = bundle.Write(f, bundle.Bundle{
		Manifest: bundle.Manifest{FormatVersion: bundle.FormatVersion, Project: "demo"},
		Observations: []bundle.Observation{{
			SyncID:         "obs-bugfix0000001",
			Type:           "bugfix",
			Title:          "fixed the thing",
			Content:        "root cause was X",
			Project:        "demo",
			Scope:          "project",
			NormalizedHash: "hash-bugfix",
			RevisionCount:  1,
			LastSeenAt:     "2026-01-01T00:00:00Z",
			CreatedAt:      "2026-01-01T00:00:00Z",
			UpdatedAt:      "2026-01-01T00:00:00Z",
			Status:         store.StatusObsolete,
		}},
	})
	f.Close()
	if err != nil {
		t.Fatalf("bundle.Write: %v", err)
	}

	var sb strings.Builder
	if err := routeCommand([]string{"ion-mem", "import-project", bundleFile,
		"--project=demo", "--data-dir=" + t.TempDir(),
	}, &sb); err != nil {
		t.Fatalf("import-project dry-run: %v", err)
	}

	out := sb.String()
	if !strings.Contains(out, "Status downgrades (1)") {
		t.Errorf("expected the status-downgrade block in the report; got: %s", out)
	}
	if !strings.Contains(out, "obs-bugfix0000001") {
		t.Errorf("expected the downgraded row's sync_id in the report; got: %s", out)
	}
	if !strings.Contains(out, "wanted "+store.StatusObsolete) {
		t.Errorf("expected the report to name the status the bundle asked for; got: %s", out)
	}
}
