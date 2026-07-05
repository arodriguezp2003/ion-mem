package mcp

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ionix/ion-mem/internal/project"
)

// projectCacheTTL bounds how long a process-cwd detection result is trusted.
// Detection inputs can change on disk after the server starts (a git remote
// is added, a .ion-mem/config.json is created), so the cache is re-validated
// instead of pinned for the process lifetime.
const projectCacheTTL = 5 * time.Minute

// isAmbiguousProjectError reports whether err wraps project.ErrAmbiguousProject.
// Used by handlers to choose project_ambiguous vs internal as the error_code.
func isAmbiguousProjectError(err error) bool {
	return errors.Is(err, project.ErrAmbiguousProject)
}

// configuredDefaultProject reads ION_MEM_PROJECT from the environment.
// This is the SINGLE callsite that may call os.Getenv — R-CC-04, R-S1-PROJ-03.
// The result is passed to New via WithDefaultProject; resolveProject never calls
// os.Getenv directly.
func configuredDefaultProject() string {
	return os.Getenv("ION_MEM_PROJECT")
}

// resolveProject determines the active project following the precedence rules
// from R-S1-PROJ-01:
//
//  1. per-call projectArg (non-empty string)
//  2. s.defaultProj (set from ION_MEM_PROJECT or --project at startup)
//  3. per-call cwdArg (detected via s.detect)
//  4. s.detect(os.Getwd())
//
// Results from path (2)/(4) are cached for process lifetime (R-S1-PROJ-02).
// os.Getenv is NOT called here — env is only read in configuredDefaultProject.
func (s *Server) resolveProject(projectArg, cwdArg string) (project.DetectionResult, error) {
	// (1) Per-call project arg wins immediately — no caching.
	if projectArg != "" {
		return project.DetectionResult{
			Project: projectArg,
			Source:  "env_override",
		}, nil
	}

	// (2) Static default project (from env/flag, set at startup).
	// No TTL here: the value is startup config, not filesystem detection.
	if s.defaultProj != "" {
		s.cacheMu.Lock()
		if s.cachedProject != nil {
			det := *s.cachedProject
			s.cacheMu.Unlock()
			return det, nil
		}
		det := project.DetectionResult{
			Project: s.defaultProj,
			Source:  "env_override",
		}
		s.cachedProject = &det
		s.cachedAt = time.Now()
		s.cacheMu.Unlock()
		return det, nil
	}

	// (3) Per-call cwd arg (NOT cached — each cwd arg may differ).
	if cwdArg != "" {
		det, err := s.detect(cwdArg)
		if err != nil {
			return project.DetectionResult{}, err
		}
		return s.applyPathMapping(cwdArg, det), nil
	}

	// (4) Default: detect from process cwd, cached with TTL re-validation.
	s.cacheMu.Lock()
	if s.cachedProject != nil && time.Since(s.cachedAt) < projectCacheTTL {
		det := *s.cachedProject
		s.cacheMu.Unlock()
		return det, nil
	}
	s.cacheMu.Unlock()

	// Detect now (outside lock to avoid holding lock during I/O).
	cwd, err := os.Getwd()
	if err != nil {
		return project.DetectionResult{}, err
	}
	det, err := s.detect(cwd)
	if err != nil {
		return project.DetectionResult{}, err
	}
	det = s.applyPathMapping(cwd, det)

	s.cacheMu.Lock()
	// Double-check after acquiring lock (another goroutine may have refreshed
	// it). A fresh entry wins; an expired one is overwritten with our result.
	if s.cachedProject == nil || time.Since(s.cachedAt) >= projectCacheTTL {
		s.cachedProject = &det
		s.cachedAt = time.Now()
	} else {
		det = *s.cachedProject
	}
	s.cacheMu.Unlock()
	return det, nil
}

// applyPathMapping upgrades a dir_basename detection result to "path_mapping"
// when the store has a known session for the given directory. Strong detection
// sources (config, git_remote, git_root, git_child) are never overridden.
//
// The layering rationale: project detection (internal/project) is intentionally
// FS-pure — it knows nothing about stored sessions. The MCP layer owns the
// store and is the correct place to enrich detection results with historical
// context. This keeps internal/project free of store dependencies.
func (s *Server) applyPathMapping(dir string, det project.DetectionResult) project.DetectionResult {
	if det.Source != "dir_basename" {
		// Strong sources are never overridden.
		return det
	}
	if s.store == nil {
		return det
	}
	proj, ok, err := s.store.ProjectForDirectory(context.Background(), dir)
	if err != nil || !ok {
		return det
	}
	return project.DetectionResult{
		Project: proj,
		Source:  "path_mapping",
		Path:    dir,
	}
}
