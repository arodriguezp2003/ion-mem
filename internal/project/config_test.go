package project

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfigRaw writes an arbitrary byte slice to <dir>/.ion-mem/config.json.
// Used for testing malformed JSON cases.
func writeConfigRaw(t *testing.T, dir string, content []byte) {
	t.Helper()
	configDir := filepath.Join(dir, ".ion-mem")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("writeConfigRaw: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), content, 0o644); err != nil {
		t.Fatalf("writeConfigRaw: write: %v", err)
	}
}

// writeConfigFile writes a valid JSON config with the given project value.
func writeConfigFile(t *testing.T, dir, project string) {
	t.Helper()
	content := `{"project":"` + project + `"}`
	writeConfigRaw(t, dir, []byte(content))
}

func TestReadConfig(t *testing.T) {
	t.Run("config-at-cwd-nearest-wins", func(t *testing.T) {
		// cwd == repoRoot — finds config in that dir.
		dir := t.TempDir()
		writeConfigFile(t, dir, "myproject")
		cfg, configDir, found, err := readConfig(dir, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if cfg.Project != "myproject" {
			t.Errorf("cfg.Project = %q; want %q", cfg.Project, "myproject")
		}
		if configDir != dir {
			t.Errorf("configDir = %q; want %q", configDir, dir)
		}
	})

	t.Run("config-at-repoRoot-found-by-walk", func(t *testing.T) {
		// Config at repoRoot; cwd is a subdir. Walk-up should find it.
		root := t.TempDir()
		subdir := filepath.Join(root, "pkg", "sub")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeConfigFile(t, root, "root-project")
		cfg, configDir, found, err := readConfig(subdir, root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if cfg.Project != "root-project" {
			t.Errorf("cfg.Project = %q; want %q", cfg.Project, "root-project")
		}
		if configDir != root {
			t.Errorf("configDir = %q; want %q", configDir, root)
		}
	})

	t.Run("nearest-config-wins-over-parent", func(t *testing.T) {
		// Config at both cwd and repoRoot; nearest (cwd) wins.
		root := t.TempDir()
		subdir := filepath.Join(root, "pkg")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeConfigFile(t, root, "root-project")
		writeConfigFile(t, subdir, "sub-project")
		cfg, configDir, found, err := readConfig(subdir, root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if cfg.Project != "sub-project" {
			t.Errorf("cfg.Project = %q; want %q (nearest should win)", cfg.Project, "sub-project")
		}
		if configDir != subdir {
			t.Errorf("configDir = %q; want %q (nearest dir should be reported)", configDir, subdir)
		}
	})

	t.Run("config-above-repo-boundary-not-found", func(t *testing.T) {
		// Parent dir has config but repoRoot doesn't — walk should stop at repoRoot.
		parent := t.TempDir()
		repoRoot := filepath.Join(parent, "repo")
		if err := os.MkdirAll(repoRoot, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeConfigFile(t, parent, "parent-project")
		_, _, found, err := readConfig(repoRoot, repoRoot)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false — should not cross repo boundary")
		}
	})

	t.Run("malformed-json-returns-error", func(t *testing.T) {
		// Malformed JSON at nearest config location returns error per design §7.
		// DetectFull treats this as fall-through (spec R-ALGO-03).
		dir := t.TempDir()
		writeConfigRaw(t, dir, []byte(`{not valid json`))
		_, _, _, err := readConfig(dir, dir)
		if err == nil {
			t.Fatal("expected error for malformed JSON; got nil")
		}
	})

	t.Run("empty-project-field-falls-through", func(t *testing.T) {
		// Empty project field: found=false so caller falls through.
		dir := t.TempDir()
		writeConfigFile(t, dir, "")
		_, _, found, err := readConfig(dir, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false for empty project field")
		}
	})

	t.Run("whitespace-only-project-falls-through", func(t *testing.T) {
		// Whitespace-only project field is treated as empty.
		dir := t.TempDir()
		writeConfigFile(t, dir, "   ")
		_, _, found, err := readConfig(dir, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false for whitespace-only project field")
		}
	})

	t.Run("no-config-anywhere", func(t *testing.T) {
		dir := t.TempDir()
		_, _, found, err := readConfig(dir, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false when no config exists")
		}
	})

	t.Run("cwd-equals-repoRoot", func(t *testing.T) {
		// When cwd == repoRoot, only that dir is checked.
		dir := t.TempDir()
		writeConfigFile(t, dir, "same-dir")
		cfg, configDir, found, err := readConfig(dir, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if cfg.Project != "same-dir" {
			t.Errorf("cfg.Project = %q; want %q", cfg.Project, "same-dir")
		}
		if configDir != dir {
			t.Errorf("configDir = %q; want %q", configDir, dir)
		}
	})
}
