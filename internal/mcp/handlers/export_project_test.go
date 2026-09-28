package handlers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

func TestIonExportProject_WritesBundleAndReturnsManifest(t *testing.T) {
	st := mustStore(t)
	seedObservation(t, st, "ion-mem", "Use widgets", "We decided to use widgets.", "decision")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	outFile := filepath.Join(t.TempDir(), "ion-mem.ionmem.zip")
	res := callTool(t, ts, "ion_export_project", map[string]any{"out": outFile})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	if env["out"] != outFile {
		t.Errorf("out = %v, want %q", env["out"], outFile)
	}
	if _, statErr := os.Stat(outFile); statErr != nil {
		t.Errorf("bundle file not written: %v", statErr)
	}

	manifest, ok := env["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("manifest field missing or wrong type: %v", env["manifest"])
	}
	if manifest["project"] != "ion-mem" {
		t.Errorf("manifest.project = %v, want ion-mem", manifest["project"])
	}
	counts, ok := manifest["counts"].(map[string]any)
	if !ok {
		t.Fatalf("manifest.counts missing or wrong type")
	}
	if obsCount, _ := counts["observations"].(float64); obsCount != 1 {
		t.Errorf("counts.observations = %v, want 1", counts["observations"])
	}
}

func TestIonExportProject_ResolvesRelativeOutAgainstCwd(t *testing.T) {
	st := mustStore(t)
	seedObservation(t, st, "ion-mem", "Use widgets", "We decided to use widgets.", "decision")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	dir := t.TempDir()
	res := callTool(t, ts, "ion_export_project", map[string]any{"out": "relative.ionmem.zip", "cwd": dir})
	env := decodeText(t, res)
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok; env=%v", env["status"], env)
	}
	wantPath := filepath.Join(dir, "relative.ionmem.zip")
	if env["out"] != wantPath {
		t.Errorf("out = %v, want %q", env["out"], wantPath)
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Errorf("bundle file not written at resolved path: %v", statErr)
	}
}

func TestIonExportProject_RelativeOutWithoutCwdIsAnError(t *testing.T) {
	st := mustStore(t)
	seedObservation(t, st, "ion-mem", "Use widgets", "We decided to use widgets.", "decision")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_export_project", map[string]any{"out": "relative.ionmem.zip"})
	env := decodeText(t, res)
	if env["status"] != "error" {
		t.Fatalf("status = %v, want error for a relative out with no cwd; env=%v", env["status"], env)
	}
}

func TestIonExportProject_RefusesSecretsUnlessAllowed(t *testing.T) {
	st := mustStore(t)
	seedObservation(t, st, "ion-mem", "aws creds", "AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP", "config")
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	outFile := filepath.Join(t.TempDir(), "ion-mem.ionmem.zip")

	res := callTool(t, ts, "ion_export_project", map[string]any{"out": outFile})
	env := decodeText(t, res)
	if env["status"] != "error" {
		t.Fatalf("status = %v, want error (secrets present)", env["status"])
	}
	if env["error_code"] != "invalid_argument" {
		t.Errorf("error_code = %v, want invalid_argument", env["error_code"])
	}
	if _, statErr := os.Stat(outFile); statErr == nil {
		t.Error("bundle file must not be written when export is refused")
	}

	res2 := callTool(t, ts, "ion_export_project", map[string]any{"out": outFile, "allow_secrets": true})
	env2 := decodeText(t, res2)
	if env2["status"] != "ok" {
		t.Fatalf("status = %v, want ok with allow_secrets=true", env2["status"])
	}
	if _, statErr := os.Stat(outFile); statErr != nil {
		t.Errorf("bundle file must be written with allow_secrets=true: %v", statErr)
	}
	findings, _ := env2["findings"].([]any)
	if len(findings) == 0 {
		t.Error("findings must still be reported even when allowed")
	}
}

func TestIonExportProject_ExcludesPromptsByDefault(t *testing.T) {
	st := mustStore(t)
	sid := mustSeedSession(t, st, "ion-mem")
	if _, err := st.AddPromptIfMissing(contextBG(t), store.AddPromptParams{
		SessionID: sid, Content: "hello", Project: "ion-mem",
	}); err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	outFile := filepath.Join(t.TempDir(), "ion-mem.ionmem.zip")
	res := callTool(t, ts, "ion_export_project", map[string]any{"out": outFile})
	env := decodeText(t, res)
	manifest := env["manifest"].(map[string]any)
	if manifest["includes_prompts"] != false {
		t.Errorf("includes_prompts = %v, want false by default", manifest["includes_prompts"])
	}

	outFile2 := filepath.Join(t.TempDir(), "ion-mem-prompts.ionmem.zip")
	res2 := callTool(t, ts, "ion_export_project", map[string]any{"out": outFile2, "with_prompts": true})
	env2 := decodeText(t, res2)
	manifest2 := env2["manifest"].(map[string]any)
	if manifest2["includes_prompts"] != true {
		t.Errorf("includes_prompts = %v, want true with with_prompts=true", manifest2["includes_prompts"])
	}

	// Verify the on-disk bundle actually round-trips prompts.
	f, err := os.Open(outFile2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	info, _ := f.Stat()
	b, err := bundle.Read(f, info.Size())
	if err != nil {
		t.Fatalf("bundle.Read: %v", err)
	}
	if len(b.Prompts) != 1 {
		t.Errorf("prompts in bundle = %d, want 1", len(b.Prompts))
	}
}
