package project_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo initializes a bare-minimum git repository at dir. It creates a
// commit so that git operations that require at least one commit work correctly.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("initRepo: %v: %s", err, out)
		}
	}
	run("git", "init", dir)
	run("git", "-C", dir, "config", "user.email", "test@test.com")
	run("git", "-C", dir, "config", "user.name", "Test")
	run("git", "-C", dir, "commit", "--allow-empty", "-m", "init")
}

// addRemote sets the origin remote URL for the git repository at repoDir.
func addRemote(t *testing.T, repoDir, url string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoDir, "remote", "add", "origin", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("addRemote: %v: %s", err, out)
	}
}

// writeConfig writes a .ion-mem/config.json file at dir with the given project name.
func writeConfig(t *testing.T, dir, project string) {
	t.Helper()
	configDir := filepath.Join(dir, ".ion-mem")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("writeConfig: mkdir: %v", err)
	}
	data, err := json.Marshal(map[string]string{"project": project})
	if err != nil {
		t.Fatalf("writeConfig: marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644); err != nil {
		t.Fatalf("writeConfig: write: %v", err)
	}
}

// chdir changes the working directory to dir for the duration of the test.
// It uses t.Chdir (Go 1.24+) which restores the original cwd on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

// mustAbs resolves an absolute, symlink-free path. On macOS /tmp is a symlink
// to /private/tmp, so naive filepath.Abs comparisons fail; this helper
// eliminates that class of test failure.
func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("mustAbs: Abs(%q): %v", p, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("mustAbs: EvalSymlinks(%q): %v", abs, err)
	}
	return resolved
}
