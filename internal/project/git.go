package project

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// parseRemoteName extracts the repository name from a git remote URL.
//
// Supported formats: SSH (git@host:org/name.git), HTTPS (https://host/org/name.git),
// ssh:// scheme, with or without .git suffix. Returns "" on malformed input so
// the caller can fall through to the next detection case (R-PARSE-07).
func parseRemoteName(url string) string {
	// R-PARSE-06: trim whitespace.
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	// Strip query string (e.g. ?ref=main).
	if idx := strings.IndexByte(url, '?'); idx >= 0 {
		url = url[:idx]
	}

	// R-PARSE-05: strip trailing .git suffix.
	url = strings.TrimSuffix(url, ".git")

	// Split on both '/' and ':' to handle SSH (host:org/repo) and HTTPS uniformly.
	parts := strings.FieldsFunc(url, func(r rune) bool {
		return r == '/' || r == ':'
	})
	if len(parts) == 0 {
		return ""
	}

	// Take the last non-empty segment.
	for i := len(parts) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(parts[i]); s != "" {
			// R-PARSE-07: a single segment with no separator in the original
			// means the URL was malformed (no : or /). Return "" so the caller
			// falls through to git_root.
			if len(parts) == 1 {
				return ""
			}
			return s
		}
	}
	return ""
}

// gitRoot returns the absolute path of the git repository root for the given
// cwd. Returns (root, true, nil) when found, ("", false, nil) when cwd is not
// inside a git repository, or ("", false, err) on other failures.
//
// Uses a 2 s context timeout per R-CC-06.
func gitRoot(cwd string) (root string, found bool, err error) {
	if _, lookupErr := exec.LookPath("git"); lookupErr != nil {
		return "", false, fmt.Errorf("project: git binary not found in PATH: %w", lookupErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		// Exit 128 means "not a git repository" — that is the normal not-found case.
		return "", false, nil
	}

	root = strings.TrimSpace(string(out))
	if root == "" {
		return "", false, nil
	}
	return root, true, nil
}

// gitRemoteOrigin returns the URL of the "origin" remote for the repository at
// repoRoot. Returns ("", false, nil) when no origin remote is configured, or
// ("", false, err) on other failures.
//
// Uses a 2 s context timeout per R-CC-06.
func gitRemoteOrigin(repoRoot string) (url string, found bool, err error) {
	if _, lookupErr := exec.LookPath("git"); lookupErr != nil {
		return "", false, fmt.Errorf("project: git binary not found in PATH: %w", lookupErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "remote", "get-url", "origin")
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		// Any non-zero exit from "git remote get-url" means no such remote.
		return "", false, nil
	}

	url = strings.TrimSpace(string(out))
	if url == "" {
		return "", false, nil
	}
	return url, true, nil
}
