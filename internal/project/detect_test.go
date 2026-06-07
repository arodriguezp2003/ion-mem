package project_test

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ionix/ion-mem/internal/project"
)

// ---------------------------------------------------------------------------
// listGitChildren tests (T-13) — via white-box package shim below
// ---------------------------------------------------------------------------
// Note: listGitChildren is unexported. We test it via TestListGitChildren
// which is placed in git_test.go (white-box package project) to access
// the unexported function. Here we test higher-level DetectFull behavior.

// ---------------------------------------------------------------------------
// DetectFull — case 5: dir_basename (T-16)
// ---------------------------------------------------------------------------

func TestDetectFull_DirBasename(t *testing.T) {
	t.Run("DIR-BASENAME-01-plain-dir", func(t *testing.T) {
		dir := t.TempDir()
		// Use mustAbs to resolve any symlinks (macOS /tmp → /private/tmp).
		dir = mustAbs(t, dir)
		// Create a unique subdir so we know the basename.
		named := filepath.Join(dir, "standalone")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		result, err := project.DetectFull(named)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "dir_basename" {
			t.Errorf("Source = %q; want %q", result.Source, "dir_basename")
		}
		if result.Project != "standalone" {
			t.Errorf("Project = %q; want %q", result.Project, "standalone")
		}
		if result.Path != named {
			t.Errorf("Path = %q; want %q", result.Path, named)
		}
	})

	t.Run("DIR-BASENAME-02-root-dir", func(t *testing.T) {
		result, err := project.DetectFull("/")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "dir_basename" {
			t.Errorf("Source = %q; want %q", result.Source, "dir_basename")
		}
		// filepath.Base("/") returns "/" on Unix; our normalize should handle it.
		// We just check it doesn't panic and source is dir_basename.
		_ = result.Project
	})

	t.Run("ERR-01-relative-path", func(t *testing.T) {
		_, err := project.DetectFull("./foo")
		if err == nil {
			t.Fatal("expected error for relative path")
		}
		if !containsStr(err.Error(), "absolute") {
			t.Errorf("error %q should contain 'absolute'", err.Error())
		}
	})

	t.Run("ERR-02-nonexistent-path", func(t *testing.T) {
		_, err := project.DetectFull("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Fatal("expected error for non-existent path")
		}
	})

	t.Run("ERR-03-file-not-dir", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := project.DetectFull(f)
		if err == nil {
			t.Fatal("expected error for file path")
		}
	})

	t.Run("ERR-04-symlink-resolved", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Skip("symlinks not supported:", err)
		}
		result, err := project.DetectFull(link)
		if err != nil {
			t.Fatalf("unexpected error for symlink: %v", err)
		}
		if result.Source != "dir_basename" {
			t.Errorf("Source = %q; want %q", result.Source, "dir_basename")
		}
	})

	t.Run("DetectionResult-fields-present", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		sub := filepath.Join(dir, "mydir")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		result, _ := project.DetectFull(sub)
		// Just assert all fields exist (compile-time check via field access).
		_ = result.Project
		_ = result.Source
		_ = result.Path
		_ = result.Warning
		_ = result.AvailableProjects
	})
}

// ---------------------------------------------------------------------------
// DetectFull — case 3: git_root (T-18)
// ---------------------------------------------------------------------------

func TestDetectFull_GitRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("GIT-ROOT-01-cwd-is-repo-root", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		named := filepath.Join(dir, "myproj")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, named)
		result, err := project.DetectFull(named)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_root" {
			t.Errorf("Source = %q; want %q", result.Source, "git_root")
		}
		if result.Project != "myproj" {
			t.Errorf("Project = %q; want %q", result.Project, "myproj")
		}
		if mustAbs(t, result.Path) != mustAbs(t, named) {
			t.Errorf("Path = %q; want %q", result.Path, named)
		}
	})

	t.Run("GIT-ROOT-02-cwd-is-nested-subdir", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		named := filepath.Join(dir, "myproj")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, named)
		nested := filepath.Join(named, "deep", "nested", "dir")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		result, err := project.DetectFull(nested)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_root" {
			t.Errorf("Source = %q; want %q", result.Source, "git_root")
		}
		if result.Project != "myproj" {
			t.Errorf("Project = %q; want %q", result.Project, "myproj")
		}
		if mustAbs(t, result.Path) != mustAbs(t, named) {
			t.Errorf("Path = %q; want %q", result.Path, named)
		}
	})
}

// ---------------------------------------------------------------------------
// DetectFull — case 2: git_remote (T-20)
// ---------------------------------------------------------------------------

func TestDetectFull_GitRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("GIT-REMOTE-01-SSH-URL", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		addRemote(t, dir, "git@github.com:ionix/ion-mem.git")
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_remote" {
			t.Errorf("Source = %q; want %q", result.Source, "git_remote")
		}
		if result.Project != "ion-mem" {
			t.Errorf("Project = %q; want %q", result.Project, "ion-mem")
		}
	})

	t.Run("GIT-REMOTE-02-HTTPS-with-git", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		addRemote(t, dir, "https://github.com/ionix/ion-mem.git")
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_remote" {
			t.Errorf("Source = %q; want %q", result.Source, "git_remote")
		}
		if result.Project != "ion-mem" {
			t.Errorf("Project = %q; want %q", result.Project, "ion-mem")
		}
	})

	t.Run("GIT-REMOTE-03-HTTPS-no-git", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		addRemote(t, dir, "https://github.com/ionix/ion-mem")
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_remote" {
			t.Errorf("Source = %q; want %q", result.Source, "git_remote")
		}
		if result.Project != "ion-mem" {
			t.Errorf("Project = %q; want %q", result.Project, "ion-mem")
		}
	})

	t.Run("GIT-REMOTE-04-malformed-falls-through-to-git-root", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		named := filepath.Join(dir, "myrepo")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, named)
		addRemote(t, named, "not-a-url")
		result, err := project.DetectFull(named)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_root" {
			t.Errorf("Source = %q; want %q (malformed URL should fall to git_root)", result.Source, "git_root")
		}
	})

	t.Run("GIT-REMOTE-05-ssh-scheme", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		addRemote(t, dir, "ssh://git@github.com/ionix/ion-mem.git")
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_remote" {
			t.Errorf("Source = %q; want %q", result.Source, "git_remote")
		}
		if result.Project != "ion-mem" {
			t.Errorf("Project = %q; want %q", result.Project, "ion-mem")
		}
	})
}

// ---------------------------------------------------------------------------
// DetectFull — case 1: config (T-22)
// ---------------------------------------------------------------------------

func TestDetectFull_Config(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("CONFIG-01-config-wins-over-remote", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		addRemote(t, dir, "https://github.com/org/other-name.git")
		writeConfig(t, dir, "my-proj")
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "config" {
			t.Errorf("Source = %q; want %q", result.Source, "config")
		}
		if result.Project != "my-proj" {
			t.Errorf("Project = %q; want %q", result.Project, "my-proj")
		}
	})

	t.Run("CONFIG-02-malformed-falls-through", func(t *testing.T) {
		// R-ALGO-03 + design §7: malformed JSON falls through silently at the
		// DetectFull level. readConfig surfaces the error internally, but
		// DetectFull catches it and continues to the next case (here git_root,
		// since the repo has no remote configured).
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		configDir := filepath.Join(dir, ".ion-mem")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{not json`), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("expected silent fallthrough on malformed config, got error: %v", err)
		}
		if result.Source != "git_root" {
			t.Errorf("Source = %q; want %q (malformed config should fall through to git_root)", result.Source, "git_root")
		}
		if result.Project == "" {
			t.Error("Project should be non-empty after fall-through to git_root (repo basename normalized)")
		}
	})

	t.Run("CONFIG-03-empty-field-falls-through", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		named := filepath.Join(dir, "myrepo")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, named)
		writeConfig(t, named, "")
		result, err := project.DetectFull(named)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Empty config falls through — should end up at git_root.
		if result.Source != "git_root" {
			t.Errorf("Source = %q; want %q (empty config should fall through)", result.Source, "git_root")
		}
	})

	t.Run("CONFIG-04-outside-repo-boundary-ignored", func(t *testing.T) {
		base := t.TempDir()
		base = mustAbs(t, base)
		// Config at base (parent), repo at base/repo.
		repoDir := filepath.Join(base, "repo")
		if err := os.Mkdir(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, repoDir)
		// Write config at parent — should NOT be honored.
		writeConfig(t, base, "parent-project")
		result, err := project.DetectFull(repoDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should fall through to git_root since config is outside repo boundary.
		if result.Source != "git_root" {
			t.Errorf("Source = %q; want %q (parent config should be ignored)", result.Source, "git_root")
		}
		if result.Project == "parent-project" {
			t.Error("parent-project leaked across repo boundary")
		}
	})

	t.Run("CONFIG-05-config-wins-over-both-remote-and-root", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		initRepo(t, dir)
		addRemote(t, dir, "https://github.com/org/bar.git")
		writeConfig(t, dir, "foo")
		result, err := project.DetectFull(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Project != "foo" {
			t.Errorf("Project = %q; want %q", result.Project, "foo")
		}
		if result.Source != "config" {
			t.Errorf("Source = %q; want %q", result.Source, "config")
		}
	})

	t.Run("CONFIG-06-config-in-subdir-walk-up-nearest-wins", func(t *testing.T) {
		// Repo at root; config at root/pkg (subdir); cwd is root/pkg/sub.
		root := t.TempDir()
		root = mustAbs(t, root)
		initRepo(t, root)
		pkgDir := filepath.Join(root, "pkg")
		subDir := filepath.Join(pkgDir, "sub")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, pkgDir, "sub-proj")
		result, err := project.DetectFull(subDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "config" {
			t.Errorf("Source = %q; want %q", result.Source, "config")
		}
		if result.Project != "sub-proj" {
			t.Errorf("Project = %q; want %q", result.Project, "sub-proj")
		}
		// Path should be the dir that held the config (pkgDir).
		if mustAbs(t, result.Path) != mustAbs(t, pkgDir) {
			t.Errorf("Path = %q; want %q", result.Path, pkgDir)
		}
	})
}

// ---------------------------------------------------------------------------
// DetectFull — case 4: git_child (T-24)
// ---------------------------------------------------------------------------

func TestDetectFull_GitChild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("GIT-CHILD-01-single-child-auto-promoted", func(t *testing.T) {
		parent := t.TempDir()
		parent = mustAbs(t, parent)
		childDir := filepath.Join(parent, "myproj")
		if err := os.Mkdir(childDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, childDir)
		result, err := project.DetectFull(parent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_child" {
			t.Errorf("Source = %q; want %q", result.Source, "git_child")
		}
		if result.Project != "myproj" {
			t.Errorf("Project = %q; want %q", result.Project, "myproj")
		}
		if result.Warning == "" {
			t.Error("Warning should be non-empty for git_child auto-promotion")
		}
		if !containsStr(result.Warning, "auto-promoted") {
			t.Errorf("Warning %q should start with 'auto-promoted'", result.Warning)
		}
	})

	t.Run("GIT-CHILD-02-two-children-ambiguous", func(t *testing.T) {
		parent := t.TempDir()
		parent = mustAbs(t, parent)
		for _, name := range []string{"a", "b"} {
			d := filepath.Join(parent, name)
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
			initRepo(t, d)
		}
		result, err := project.DetectFull(parent)
		if err == nil {
			t.Fatal("expected ErrAmbiguousProject")
		}
		if !errors.Is(err, project.ErrAmbiguousProject) {
			t.Errorf("err = %v; want ErrAmbiguousProject", err)
		}
		if result.Project != "" {
			t.Errorf("Project = %q; want empty", result.Project)
		}
		want := []string{"a", "b"}
		sort.Strings(want)
		if len(result.AvailableProjects) != 2 {
			t.Errorf("AvailableProjects = %v; want %v", result.AvailableProjects, want)
		}
	})

	t.Run("GIT-CHILD-03-noise-dir-filtered-out", func(t *testing.T) {
		parent := t.TempDir()
		parent = mustAbs(t, parent)
		// node_modules is noise — has .git but should be excluded.
		noiseDir := filepath.Join(parent, "node_modules")
		if err := os.Mkdir(noiseDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, noiseDir)
		// myproj is a valid git child.
		validDir := filepath.Join(parent, "myproj")
		if err := os.Mkdir(validDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, validDir)
		result, err := project.DetectFull(parent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_child" {
			t.Errorf("Source = %q; want %q", result.Source, "git_child")
		}
		if result.Project != "myproj" {
			t.Errorf("Project = %q; want %q", result.Project, "myproj")
		}
	})

	t.Run("GIT-CHILD-04-all-noise-falls-to-dir-basename", func(t *testing.T) {
		parent := t.TempDir()
		parent = mustAbs(t, parent)
		noiseDir := filepath.Join(parent, "node_modules")
		if err := os.Mkdir(noiseDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, noiseDir)
		// parent dir name after mustAbs — let's give it a known name via subdir.
		namedParent := filepath.Join(t.TempDir(), "workspace")
		if err := os.MkdirAll(namedParent, 0o755); err != nil {
			t.Fatal(err)
		}
		noiseOnly := filepath.Join(namedParent, "node_modules")
		if err := os.Mkdir(noiseOnly, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, noiseOnly)
		namedParent = mustAbs(t, namedParent)
		result, err := project.DetectFull(namedParent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "dir_basename" {
			t.Errorf("Source = %q; want %q (all noise → dir_basename)", result.Source, "dir_basename")
		}
	})

	t.Run("GIT-CHILD-05-hidden-dir-filtered", func(t *testing.T) {
		parent := t.TempDir()
		parent = mustAbs(t, parent)
		// .hidden-repo has .git but is hidden — should be excluded.
		hiddenDir := filepath.Join(parent, ".hidden-repo")
		if err := os.Mkdir(hiddenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, hiddenDir)
		// myproj is a valid git child.
		validDir := filepath.Join(parent, "myproj")
		if err := os.Mkdir(validDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, validDir)
		result, err := project.DetectFull(parent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Source != "git_child" {
			t.Errorf("Source = %q; want %q", result.Source, "git_child")
		}
		if result.Project != "myproj" {
			t.Errorf("Project = %q; want %q", result.Project, "myproj")
		}
	})
}

// ---------------------------------------------------------------------------
// Detect() wrapper (T-26)
// ---------------------------------------------------------------------------

func TestDetect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("DETECT-01-happy-path", func(t *testing.T) {
		dir := t.TempDir()
		dir = mustAbs(t, dir)
		named := filepath.Join(dir, "myproject")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepo(t, named)
		got, err := project.Detect(named)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "myproject" {
			t.Errorf("Detect = %q; want %q", got, "myproject")
		}
	})

	t.Run("DETECT-02-ambiguous-returns-sentinel", func(t *testing.T) {
		parent := t.TempDir()
		parent = mustAbs(t, parent)
		for _, name := range []string{"a", "b"} {
			d := filepath.Join(parent, name)
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
			initRepo(t, d)
		}
		got, err := project.Detect(parent)
		if !errors.Is(err, project.ErrAmbiguousProject) {
			t.Errorf("err = %v; want ErrAmbiguousProject", err)
		}
		if got != "" {
			t.Errorf("Detect = %q; want empty string on ambiguous", got)
		}
	})

	t.Run("DETECT-helper-error-fails-open-to-basename", func(t *testing.T) {
		// A non-existent path passed to Detect should fail open to dir_basename.
		// Detect wraps errors → returns (basename, nil).
		dir := t.TempDir()
		named := filepath.Join(dir, "myfallback")
		if err := os.Mkdir(named, 0o755); err != nil {
			t.Fatal(err)
		}
		named = mustAbs(t, named)
		// Remove the dir so EvalSymlinks would fail... actually Detect should
		// fail open. Let's use a known non-existent path constructed from
		// the parent (so the dir name is "myfallback").
		missing := filepath.Join(dir, "missing-dir-xyz")
		got, err := project.Detect(missing)
		if err != nil {
			t.Fatalf("Detect should fail open, got error: %v", err)
		}
		// Should return the basename of the path.
		if got != "missing-dir-xyz" {
			t.Errorf("Detect = %q; want %q (fail-open basename)", got, "missing-dir-xyz")
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
