package mcp

import (
	"errors"
	"os"

	"github.com/ionix/ion-mem/internal/project"
)

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
		s.cacheMu.Unlock()
		return det, nil
	}

	// (3) Per-call cwd arg (NOT cached — each cwd arg may differ).
	if cwdArg != "" {
		return s.detect(cwdArg)
	}

	// (4) Default: detect from process cwd, cached for lifetime.
	s.cacheMu.Lock()
	if s.cachedProject != nil {
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

	s.cacheMu.Lock()
	// Double-check after acquiring lock (another goroutine may have set it).
	if s.cachedProject == nil {
		s.cachedProject = &det
	} else {
		det = *s.cachedProject
	}
	s.cacheMu.Unlock()
	return det, nil
}
