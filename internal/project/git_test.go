package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// parseRemoteName
// ---------------------------------------------------------------------------

func TestParseRemoteName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
		want string
	}{
		// R-PARSE-01: SSH form git@host:org/name.git
		{name: "ssh-git@", url: "git@github.com:org/repo.git", want: "repo"},
		// R-PARSE-02: HTTPS with .git suffix
		{name: "https-with-git", url: "https://github.com/org/repo.git", want: "repo"},
		// R-PARSE-03: ssh:// scheme
		{name: "ssh-scheme", url: "ssh://git@github.com/org/repo.git", want: "repo"},
		// R-PARSE-04/R-PARSE-05: HTTPS without .git suffix
		{name: "https-no-git", url: "https://github.com/org/repo", want: "repo"},
		// R-PARSE-06 adjacent: strip query string
		{name: "query-string", url: "https://github.com/org/repo?ref=main", want: "repo"},
		// R-PARSE-06: leading/trailing whitespace trimmed
		{name: "whitespace", url: "  https://github.com/org/repo.git  ", want: "repo"},
		// R-PARSE-07: malformed (no : or /)
		{name: "malformed-no-separator", url: "not-a-url", want: ""},
		// Empty string
		{name: "empty", url: "", want: ""},
		// SSH with only host:name (no slash)
		{name: "ssh-no-slash", url: "git@host:repo.git", want: "repo"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRemoteName(tt.url)
			if got != tt.want {
				t.Errorf("parseRemoteName(%q) = %q; want %q", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalize (placed in git_test.go for Phase 3; white-box package)
// ---------------------------------------------------------------------------

func TestNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trim-and-lower", input: "  MyProject  ", want: "myproject"},
		{name: "all-caps", input: "IONIX", want: "ionix"},
		{name: "empty", input: "", want: ""},
		{name: "whitespace-only", input: "  ", want: ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := normalize(tt.input)
			if got != tt.want {
				t.Errorf("normalize(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// gitRoot
// ---------------------------------------------------------------------------

func TestGitRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("cwd-is-repo-root", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWhitebox(t, dir)
		root, found, err := gitRoot(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		wantRoot := mustAbsWhitebox(t, dir)
		gotRoot := mustAbsWhitebox(t, root)
		if gotRoot != wantRoot {
			t.Errorf("gitRoot = %q; want %q", gotRoot, wantRoot)
		}
	})

	t.Run("cwd-is-subdir", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWhitebox(t, dir)
		subdir := filepath.Join(dir, "pkg", "sub")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}
		root, found, err := gitRoot(subdir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		wantRoot := mustAbsWhitebox(t, dir)
		gotRoot := mustAbsWhitebox(t, root)
		if gotRoot != wantRoot {
			t.Errorf("gitRoot = %q; want %q", gotRoot, wantRoot)
		}
	})

	t.Run("not-a-repo", func(t *testing.T) {
		dir := t.TempDir()
		_, found, err := gitRoot(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false for non-repo dir")
		}
	})

	t.Run("symlink-cwd", func(t *testing.T) {
		base := t.TempDir()
		repoDir := filepath.Join(base, "repo")
		if err := os.Mkdir(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWhitebox(t, repoDir)

		linkDir := filepath.Join(base, "link")
		if err := os.Symlink(repoDir, linkDir); err != nil {
			t.Skip("symlinks not supported on this platform:", err)
		}
		root, found, err := gitRoot(linkDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true for symlinked repo dir")
		}
		// Root path may differ via symlink vs real path; both are valid.
		_ = root
	})
}

// ---------------------------------------------------------------------------
// gitRemoteOrigin
// ---------------------------------------------------------------------------

func TestGitRemoteOrigin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}

	t.Run("with-remote", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWhitebox(t, dir)
		addRemoteWhitebox(t, dir, "https://github.com/org/repo.git")
		url, found, err := gitRemoteOrigin(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if url != "https://github.com/org/repo.git" {
			t.Errorf("gitRemoteOrigin = %q; want %q", url, "https://github.com/org/repo.git")
		}
	})

	t.Run("no-remote", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWhitebox(t, dir)
		_, found, err := gitRemoteOrigin(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false when no remote set")
		}
	})
}

// ---------------------------------------------------------------------------
// listGitChildren (T-13) — white-box test in package project
// ---------------------------------------------------------------------------

func TestListGitChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: shells out to git")
	}
	noise := map[string]struct{}{"node_modules": {}, "vendor": {}}

	t.Run("single-git-child", func(t *testing.T) {
		parent := t.TempDir()
		childDir := filepath.Join(parent, "child")
		if err := os.Mkdir(childDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWhitebox(t, childDir)
		children, err := listGitChildren(parent, noise)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(children) != 1 {
			t.Fatalf("expected 1 child; got %d: %v", len(children), children)
		}
		if children[0].Name != "child" {
			t.Errorf("Name = %q; want %q", children[0].Name, "child")
		}
	})

	t.Run("two-git-children-sorted", func(t *testing.T) {
		parent := t.TempDir()
		for _, name := range []string{"beta", "alpha"} {
			d := filepath.Join(parent, name)
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
			initRepoWhitebox(t, d)
		}
		children, err := listGitChildren(parent, noise)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(children) != 2 {
			t.Fatalf("expected 2 children; got %d", len(children))
		}
		if children[0].Name != "alpha" || children[1].Name != "beta" {
			t.Errorf("children not sorted: %v", children)
		}
	})

	t.Run("noise-dir-excluded", func(t *testing.T) {
		parent := t.TempDir()
		noiseDir := filepath.Join(parent, "node_modules")
		if err := os.Mkdir(noiseDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWhitebox(t, noiseDir)
		validDir := filepath.Join(parent, "good")
		if err := os.Mkdir(validDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWhitebox(t, validDir)
		children, err := listGitChildren(parent, noise)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(children) != 1 || children[0].Name != "good" {
			t.Errorf("expected [good]; got %v", children)
		}
	})

	t.Run("hidden-dir-excluded", func(t *testing.T) {
		parent := t.TempDir()
		hiddenDir := filepath.Join(parent, ".hidden")
		if err := os.Mkdir(hiddenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWhitebox(t, hiddenDir)
		validDir := filepath.Join(parent, "good")
		if err := os.Mkdir(validDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoWhitebox(t, validDir)
		children, err := listGitChildren(parent, noise)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(children) != 1 || children[0].Name != "good" {
			t.Errorf("expected [good]; got %v", children)
		}
	})

	t.Run("no-git-children", func(t *testing.T) {
		parent := t.TempDir()
		// Create a subdir without .git.
		if err := os.Mkdir(filepath.Join(parent, "notrepo"), 0o755); err != nil {
			t.Fatal(err)
		}
		children, err := listGitChildren(parent, noise)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(children) != 0 {
			t.Errorf("expected 0 children; got %d", len(children))
		}
	})
}

// ---------------------------------------------------------------------------
// White-box fixture helpers (package project — not project_test)
// These parallel the helpers_test.go fixtures which live in project_test.
// ---------------------------------------------------------------------------

func initRepoWhitebox(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("initRepoWhitebox: %v: %s", err, out)
		}
	}
	run("git", "init", dir)
	run("git", "-C", dir, "config", "user.email", "test@test.com")
	run("git", "-C", dir, "config", "user.name", "Test")
	run("git", "-C", dir, "commit", "--allow-empty", "-m", "init")
}

func addRemoteWhitebox(t *testing.T, repoDir, url string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoDir, "remote", "add", "origin", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("addRemoteWhitebox: %v: %s", err, out)
	}
}

func mustAbsWhitebox(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("mustAbsWhitebox: Abs(%q): %v", p, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("mustAbsWhitebox: EvalSymlinks(%q): %v", abs, err)
	}
	return resolved
}
