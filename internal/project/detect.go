package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DetectionResult carries the full output of DetectFull.
type DetectionResult struct {
	// Project is the resolved project name. Empty when ErrAmbiguousProject is returned.
	Project string
	// Source describes how the project name was derived. One of: "config",
	// "git_remote", "git_root", "git_child", "dir_basename", or "ambiguous".
	Source string
	// Path is the canonical directory associated with the project (repo root for
	// git cases, the config-bearing dir for config case, cwd for dir_basename).
	Path string
	// Warning is a non-empty advisory message when Source is "git_child"
	// (auto-promotion warning).
	Warning string
	// AvailableProjects is populated only when ErrAmbiguousProject is returned.
	AvailableProjects []string
}

// childRepo holds a git-repository entry found during git_child scanning.
type childRepo struct {
	Name string // directory basename
	Path string // absolute path
}

// DetectFull resolves a project name from the given absolute directory path
// using a deterministic 5-case algorithm:
//
//  1. config     — nearest .ion-mem/config.json inside the enclosing git repo
//  2. git_remote — git remote origin URL → parse repo name
//  3. git_root   — git repository root basename
//  4. git_child  — single git-repo immediate subdir → auto-promote
//  5. dir_basename — fallback: directory basename (always succeeds)
//
// All resolved project names are normalized to lowercase with whitespace trimmed.
// cwd must be an absolute path; relative paths are rejected.
func DetectFull(cwd string) (DetectionResult, error) {
	// Precondition 1: reject relative paths (R-API-05).
	if !filepath.IsAbs(cwd) {
		return DetectionResult{}, fmt.Errorf("project: cwd must be an absolute path, got %q", cwd)
	}

	// Precondition 2: resolve symlinks (R-API-06).
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return DetectionResult{}, wrap("evalsymlinks", err)
	}
	cwd = resolved

	// Precondition 3: must exist and be a directory (R-API-07).
	info, err := os.Stat(cwd)
	if err != nil {
		return DetectionResult{}, wrap("stat", err)
	}
	if !info.IsDir() {
		return DetectionResult{}, fmt.Errorf("project: stat: %q is not a directory", cwd)
	}

	// Git probe.
	repoRoot, isRepo, err := gitRoot(cwd)
	if err != nil {
		return DetectionResult{}, wrap("gitroot", err)
	}

	if isRepo {
		// Case 1: config — config wins over git_root (locked decision #6).
		// R-ALGO-03 + design §7: malformed JSON is a SILENT fall-through to the
		// next case. readConfig returns the error so internal callers can inspect
		// it if needed, but DetectFull honors the architectural promise that
		// detection always returns a usable result. Empty/malformed config →
		// behave as if no config exists.
		cfg, configDir, found, cfgErr := readConfig(cwd, repoRoot)
		if cfgErr == nil && found {
			name := normalize(cfg.Project)
			if name != "" {
				return DetectionResult{
					Project: name,
					Source:  "config",
					Path:    configDir,
				}, nil
			}
		}

		// Case 2: git_remote.
		remoteURL, hasRemote, remoteErr := gitRemoteOrigin(repoRoot)
		if remoteErr != nil {
			return DetectionResult{}, wrap("gitremote", remoteErr)
		}
		if hasRemote {
			name := normalize(parseRemoteName(remoteURL))
			if name != "" {
				return DetectionResult{
					Project: name,
					Source:  "git_remote",
					Path:    repoRoot,
				}, nil
			}
			// Malformed URL → fall through to case 3 (git_root).
		}

		// Case 3: git_root.
		return DetectionResult{
			Project: normalize(filepath.Base(repoRoot)),
			Source:  "git_root",
			Path:    repoRoot,
		}, nil
	}

	// Not inside a git repo — try git_child.
	children, listErr := listGitChildren(cwd, noiseDirs)
	if listErr != nil {
		return DetectionResult{}, wrap("listchildren", listErr)
	}

	switch len(children) {
	case 1:
		// Case 4: exactly one git child — auto-promote (R-ALGO-07).
		child := children[0]
		// Recurse into the child to get its full detection result.
		childResult, childErr := DetectFull(child.Path)
		if childErr != nil && !errors.Is(childErr, ErrAmbiguousProject) {
			// Propagate unexpected errors but fall through on ambiguity.
			return DetectionResult{}, wrap("git_child", childErr)
		}
		// Override Source to "git_child" (spec confirms engram behavior: Source is
		// "git_child" even when child's own detection produced a different source).
		childResult.Source = "git_child"
		childResult.Warning = "auto-promoted child repository: " + child.Name
		return childResult, nil

	default:
		if len(children) >= 2 {
			// Case 4: ambiguous (R-ALGO-08).
			names := make([]string, len(children))
			for i, c := range children {
				names[i] = c.Name
			}
			sort.Strings(names)
			return DetectionResult{
				Project:           "",
				Source:            "ambiguous",
				Path:              cwd,
				AvailableProjects: names,
			}, ErrAmbiguousProject
		}
	}

	// Case 5: dir_basename — unconditional fallback (R-ALGO-09).
	base := filepath.Base(cwd)
	return DetectionResult{
		Project: normalize(base),
		Source:  "dir_basename",
		Path:    cwd,
	}, nil
}

// Detect is a convenience wrapper around DetectFull that returns only the
// project name. On success it returns (project, nil). On ErrAmbiguousProject
// it returns ("", ErrAmbiguousProject). On any other error it fails open and
// returns (filepath.Base(cwd), nil) so callers never receive an empty string
// unexpectedly (design §7).
func Detect(cwd string) (string, error) {
	result, err := DetectFull(cwd)
	if err != nil {
		if errors.Is(err, ErrAmbiguousProject) {
			return "", ErrAmbiguousProject
		}
		// Fail open: use basename of cwd as the project name.
		base := filepath.Base(cwd)
		if base == "" || base == "." {
			base = "unknown"
		}
		return normalize(base), nil
	}
	return result.Project, nil
}

// listGitChildren scans cwd one level deep for immediate subdirectories that
// contain a .git entry. It skips entries in the noise set and hidden directories
// (any name starting with "."). A 200 ms wall-clock deadline and a 20-entry scan
// cap enforce R-ALGO-10. The result is sorted by Name for deterministic ordering.
func listGitChildren(cwd string, noise map[string]struct{}) ([]childRepo, error) {
	deadline := time.Now().Add(200 * time.Millisecond)

	entries, err := os.ReadDir(cwd)
	if err != nil {
		return nil, err
	}

	var children []childRepo
	scanned := 0

	for _, entry := range entries {
		if time.Now().After(deadline) {
			// Timeout — fall through to dir_basename (R-ALGO-10).
			break
		}
		if scanned >= 20 {
			break
		}
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// R-ALGO-11: skip hidden directories.
		if strings.HasPrefix(name, ".") {
			continue
		}
		// R-NOISE-01: skip noise set entries.
		if _, isNoise := noise[name]; isNoise {
			continue
		}
		scanned++

		childPath := filepath.Join(cwd, name)
		gitPath := filepath.Join(childPath, ".git")
		if _, statErr := os.Stat(gitPath); statErr == nil {
			children = append(children, childRepo{Name: name, Path: childPath})
		}
		// Per-entry Stat errors are silently ignored (matches engram behavior).
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].Name < children[j].Name
	})
	return children, nil
}

// normalize applies canonical project name rules: lowercase + trim whitespace.
// Empty result after normalization signals the name should fall through to the
// next detection case (locked decision #8).
func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
