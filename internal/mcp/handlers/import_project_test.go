package handlers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/mcp"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// mustExportedBundle exports a fresh project bundle from st and returns the
// file path. proj must already have at least one observation seeded.
func mustExportedBundle(t *testing.T, st *store.Store, proj string) string {
	t.Helper()
	_, ts := mustTestServer(t, st, fakeProject(proj))
	outFile := filepath.Join(t.TempDir(), proj+".ionmem.zip")
	res := callTool(t, ts, "ion_export_project", map[string]any{"out": outFile})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("export failed: %v", env)
	}
	return outFile
}

func TestIonImportProject_DryRunDoesNotWrite(t *testing.T) {
	src := mustStore(t)
	seedObservation(t, src, "shared-proj", "Use widgets", "We decided to use widgets.", "decision")
	bundleFile := mustExportedBundle(t, src, "shared-proj")

	dst := mustStore(t)
	_, dstTS := mustTestServer(t, dst, fakeProject("shared-proj"))

	res := callTool(t, dstTS, "ion_import_project", map[string]any{"file": bundleFile})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	if env["applied"] != false {
		t.Errorf("applied = %v, want false (dry-run)", env["applied"])
	}
	if inserted, _ := env["inserted"].(float64); inserted != 1 {
		t.Errorf("inserted = %v, want 1", env["inserted"])
	}

	obs, err := dst.RecentObservations(contextBG(t), store.RecentObservationsParams{Project: "shared-proj"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 0 {
		t.Errorf("dry-run must not write: found %d observations", len(obs))
	}
}

func TestIonImportProject_ApplyWritesAndBacksUp(t *testing.T) {
	src := mustStore(t)
	seedObservation(t, src, "shared-proj", "Use widgets", "We decided to use widgets.", "decision")
	bundleFile := mustExportedBundle(t, src, "shared-proj")

	dst := mustStore(t)
	dataDir := t.TempDir()
	_, dstTS := mustTestServer(t, dst, fakeProject("shared-proj"), mcp.WithDataDir(dataDir))

	res := callTool(t, dstTS, "ion_import_project", map[string]any{"file": bundleFile, "apply": true})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	if env["applied"] != true {
		t.Errorf("applied = %v, want true", env["applied"])
	}
	if env["backup_path"] == nil || env["backup_path"] == "" {
		t.Error("backup_path must be set on apply")
	}

	obs, err := dst.RecentObservations(contextBG(t), store.RecentObservationsParams{Project: "shared-proj"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("observations = %d, want 1", len(obs))
	}
}

func TestIonImportProject_ProjectMismatchIsReported(t *testing.T) {
	src := mustStore(t)
	seedObservation(t, src, "proj-a", "note", "content", "decision")
	bundleFile := mustExportedBundle(t, src, "proj-a")

	dst := mustStore(t)
	_, dstTS := mustTestServer(t, dst, fakeProject("proj-b"))

	res := callTool(t, dstTS, "ion_import_project", map[string]any{"file": bundleFile})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	if env["project_mismatch"] != true {
		t.Errorf("project_mismatch = %v, want true (bundle=proj-a, target=proj-b)", env["project_mismatch"])
	}
	if env["bundle_project"] != "proj-a" {
		t.Errorf("bundle_project = %v, want proj-a", env["bundle_project"])
	}
}

func TestIonImportProject_ResolvesRelativeFileAgainstCwd(t *testing.T) {
	src := mustStore(t)
	seedObservation(t, src, "shared-proj", "Use widgets", "We decided to use widgets.", "decision")
	bundleFile := mustExportedBundle(t, src, "shared-proj")

	dir := filepath.Dir(bundleFile)
	relName := filepath.Base(bundleFile)

	dst := mustStore(t)
	_, dstTS := mustTestServer(t, dst, fakeProject("shared-proj"))

	res := callTool(t, dstTS, "ion_import_project", map[string]any{"file": relName, "cwd": dir})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	if inserted, _ := env["inserted"].(float64); inserted != 1 {
		t.Errorf("inserted = %v, want 1", env["inserted"])
	}
}

func TestIonImportProject_RelativeFileWithoutCwdIsAnError(t *testing.T) {
	dst := mustStore(t)
	_, ts := mustTestServer(t, dst, fakeProject("shared-proj"))

	res := callTool(t, ts, "ion_import_project", map[string]any{"file": "relative-bundle.ionmem.zip"})
	env := decodeText(t, res)
	if env["status"] != "error" {
		t.Fatalf("status = %v, want error for a relative file with no cwd; env=%v", env["status"], env)
	}
	result, _ := env["result"].(string)
	if !strings.Contains(result, "cwd") && !strings.Contains(result, "relative") {
		t.Errorf("result = %q, want it to clearly explain the relative-path/cwd problem (not an incidental open() failure)", result)
	}
}

func TestIonImportProject_RequiresFile(t *testing.T) {
	dst := mustStore(t)
	_, ts := mustTestServer(t, dst, fakeProject("shared-proj"))

	res := callTool(t, ts, "ion_import_project", map[string]any{})
	env := decodeText(t, res)
	if env["status"] != "error" {
		t.Fatalf("status = %v, want error when file is missing", env["status"])
	}
}

// mustWriteBundle writes b to a .ionmem.zip in a temp dir and returns the path.
// Building the bundle by hand (rather than exporting one from a live store) is
// the only way to produce a bundled status the store will REJECT: a live store
// would never let a permanent type be marked obsolete in the first place.
func mustWriteBundle(t *testing.T, b bundle.Bundle) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "downgrade.ionmem.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("mustWriteBundle: create: %v", err)
	}
	defer f.Close()
	if err := bundle.Write(f, b); err != nil {
		t.Fatalf("mustWriteBundle: write: %v", err)
	}
	return path
}

// TestIonImportProject_StatusDowngradesAreReported pins status_downgrades to
// the envelope. A downgraded row lands with its CONTENT but not the status the
// bundle asked for, so dropping it from the response would hide a real
// difference between the bundle and what now sits in the local store.
func TestIonImportProject_StatusDowngradesAreReported(t *testing.T) {
	// A permanent type (bugfix) marked obsolete: the same invariant
	// SetObservationStatus enforces rejects it, so it imports as active.
	bundleFile := mustWriteBundle(t, bundle.Bundle{
		Manifest: bundle.Manifest{FormatVersion: bundle.FormatVersion, Project: "shared-proj"},
		Observations: []bundle.Observation{{
			SyncID:         "obs-bugfix0000001",
			Type:           "bugfix",
			Title:          "fixed the thing",
			Content:        "root cause was X",
			Project:        "shared-proj",
			Scope:          "project",
			NormalizedHash: "hash-bugfix",
			RevisionCount:  1,
			LastSeenAt:     "2026-01-01T00:00:00Z",
			CreatedAt:      "2026-01-01T00:00:00Z",
			UpdatedAt:      "2026-01-01T00:00:00Z",
			Status:         store.StatusObsolete,
		}},
	})

	dst := mustStore(t)
	_, ts := mustTestServer(t, dst, fakeProject("shared-proj"), mcp.WithDataDir(t.TempDir()))

	res := callTool(t, ts, "ion_import_project", map[string]any{"file": bundleFile, "apply": true})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	if inserted, _ := env["inserted"].(float64); inserted != 1 {
		t.Fatalf("inserted = %v, want 1 (content must land even when its status is rejected)", env["inserted"])
	}

	raw, ok := env["status_downgrades"].([]any)
	if !ok {
		t.Fatalf("status_downgrades = %#v, want an array", env["status_downgrades"])
	}
	if len(raw) != 1 {
		t.Fatalf("status_downgrades = %d entries, want 1", len(raw))
	}
	dg, ok := raw[0].(map[string]any)
	if !ok {
		t.Fatalf("status_downgrades[0] = %#v, want an object", raw[0])
	}
	if dg["sync_id"] != "obs-bugfix0000001" {
		t.Errorf("sync_id = %v, want obs-bugfix0000001", dg["sync_id"])
	}
	if dg["wanted"] != store.StatusObsolete {
		t.Errorf("wanted = %v, want %q (the status the BUNDLE asked for)", dg["wanted"], store.StatusObsolete)
	}
	if reason, _ := dg["reason"].(string); reason == "" {
		t.Error("reason must explain why the status was rejected, not be empty")
	}

	// The row itself is active in the store, not obsolete.
	obs, err := dst.RecentObservations(contextBG(t), store.RecentObservationsParams{Project: "shared-proj"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("observations = %d, want 1", len(obs))
	}
	if obs[0].Status != store.StatusActive {
		t.Errorf("status = %q, want %q (rejected status leaves the row active)", obs[0].Status, store.StatusActive)
	}
}

// TestIonImportProject_StatusDowngradesIsAlwaysAnArray guards the empty case:
// an agent reading the envelope must never have to distinguish "no downgrades"
// from a missing key.
func TestIonImportProject_StatusDowngradesIsAlwaysAnArray(t *testing.T) {
	src := mustStore(t)
	seedObservation(t, src, "shared-proj", "Use widgets", "We decided to use widgets.", "decision")
	bundleFile := mustExportedBundle(t, src, "shared-proj")

	dst := mustStore(t)
	_, ts := mustTestServer(t, dst, fakeProject("shared-proj"))

	env := decodeText(t, callTool(t, ts, "ion_import_project", map[string]any{"file": bundleFile}))
	raw, ok := env["status_downgrades"].([]any)
	if !ok {
		t.Fatalf("status_downgrades = %#v, want an empty array (never null, never absent)", env["status_downgrades"])
	}
	if len(raw) != 0 {
		t.Errorf("status_downgrades = %d entries, want 0", len(raw))
	}
}
