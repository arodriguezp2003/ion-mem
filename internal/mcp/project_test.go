package mcp

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/project"
)

// makeDetectFunc returns a detect function that records call count and
// always returns the given result/error. The counter is safe for concurrent use.
func makeDetectFunc(result project.DetectionResult, err error) (func(string) (project.DetectionResult, error), *atomic.Int32) {
	var count atomic.Int32
	fn := func(_ string) (project.DetectionResult, error) {
		count.Add(1)
		return result, err
	}
	return fn, &count
}

func TestResolveProject_env_override_wins(t *testing.T) {
	t.Setenv("ION_MEM_PROJECT", "env-project")

	detect, _ := makeDetectFunc(project.DetectionResult{Project: "from-detect", Source: "git_remote", Path: "/x"}, nil)
	s := &Server{
		detect:      detect,
		defaultProj: "env-project", // simulating what config loader sets from ION_MEM_PROJECT
	}

	det, err := s.resolveProject("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if det.Project != "env-project" {
		t.Errorf("project = %q, want %q", det.Project, "env-project")
	}
	if det.Source != "env_override" {
		t.Errorf("source = %q, want %q", det.Source, "env_override")
	}
}

func TestResolveProject_per_call_project_arg_wins_over_default(t *testing.T) {
	detect, _ := makeDetectFunc(project.DetectionResult{Project: "from-detect", Source: "git_remote", Path: "/x"}, nil)
	s := &Server{
		detect:      detect,
		defaultProj: "default-proj",
	}

	det, err := s.resolveProject("call-level-proj", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if det.Project != "call-level-proj" {
		t.Errorf("project = %q, want %q", det.Project, "call-level-proj")
	}
	if det.Source != "env_override" {
		t.Errorf("source = %q, want %q", det.Source, "env_override")
	}
}

func TestResolveProject_default_proj_cached_for_process_lifetime(t *testing.T) {
	det := project.DetectionResult{Project: "cached-proj", Source: "git_root", Path: "/repo"}
	detect, count := makeDetectFunc(det, nil)
	s := &Server{detect: detect}

	// Call resolveProject multiple times with no overrides; DetectFull(cwd) path used.
	for i := 0; i < 5; i++ {
		if _, err := s.resolveProject("", ""); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	// detect should have been called only once — result is cached.
	if count.Load() != 1 {
		t.Errorf("detect called %d times, want exactly 1 (caching)", count.Load())
	}
}

func TestResolveProject_cwd_arg_override(t *testing.T) {
	// No default proj, no env var; cwd arg triggers detect with supplied path.
	calls := 0
	det := project.DetectionResult{Project: "cwd-proj", Source: "git_root", Path: "/supplied"}
	detect := func(cwd string) (project.DetectionResult, error) {
		calls++
		if cwd != "/supplied" {
			t.Errorf("detect got cwd=%q, want %q", cwd, "/supplied")
		}
		return det, nil
	}
	s := &Server{detect: detect}

	result, err := s.resolveProject("", "/supplied")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Project != "cwd-proj" {
		t.Errorf("project = %q, want %q", result.Project, "cwd-proj")
	}
}

func TestResolveProject_os_getenv_not_called_in_resolve(t *testing.T) {
	// Set env var to something; Server.defaultProj is empty.
	// resolveProject must NOT call os.Getenv — it reads from s.defaultProj only.
	t.Setenv("ION_MEM_PROJECT", "should-not-appear")
	det := project.DetectionResult{Project: "detect-result", Source: "dir_basename", Path: "/tmp"}
	detect, _ := makeDetectFunc(det, nil)

	// defaultProj is empty — config loader didn't set it.
	s := &Server{detect: detect}
	result, err := s.resolveProject("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return detect-result, NOT "should-not-appear".
	if result.Project == "should-not-appear" {
		t.Error("resolveProject called os.Getenv directly — it must not")
	}
}

func TestResolveProject_concurrent_safe(t *testing.T) {
	det := project.DetectionResult{Project: "safe", Source: "git_root", Path: "/repo"}
	detect, _ := makeDetectFunc(det, nil)
	s := &Server{detect: detect}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.resolveProject("", ""); err != nil {
				t.Errorf("concurrent resolve: %v", err)
			}
		}()
	}
	wg.Wait()
}

// Verify env var is read only at config-load time (Server.defaultProj), not inline.
func TestConfigLoader_reads_ION_MEM_PROJECT(t *testing.T) {
	t.Setenv("ION_MEM_PROJECT", "loaded-from-env")
	proj := configuredDefaultProject()
	if proj != "loaded-from-env" {
		t.Errorf("configuredDefaultProject() = %q, want %q", proj, "loaded-from-env")
	}
}

func TestConfigLoader_returns_empty_when_not_set(t *testing.T) {
	os.Unsetenv("ION_MEM_PROJECT")
	proj := configuredDefaultProject()
	if proj != "" {
		t.Errorf("configuredDefaultProject() = %q, want empty string when env unset", proj)
	}
}

// TestResolveProject_cache_expires_after_ttl verifies that the process-cwd
// detection cache is re-validated after projectCacheTTL, so detection-input
// changes on disk (new git remote, new config) are eventually picked up.
func TestResolveProject_cache_expires_after_ttl(t *testing.T) {
	detect, count := makeDetectFunc(project.DetectionResult{Project: "p1", Source: "git_root", Path: "/x"}, nil)
	s := &Server{detect: detect}

	for i := 0; i < 2; i++ {
		if _, err := s.resolveProject("", ""); err != nil {
			t.Fatalf("resolveProject call %d: %v", i, err)
		}
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("expected 1 detect call while cache is fresh, got %d", got)
	}

	// Age the cache past the TTL.
	s.cacheMu.Lock()
	s.cachedAt = time.Now().Add(-projectCacheTTL - time.Second)
	s.cacheMu.Unlock()

	if _, err := s.resolveProject("", ""); err != nil {
		t.Fatalf("resolveProject after expiry: %v", err)
	}
	if got := count.Load(); got != 2 {
		t.Errorf("expected re-detection after TTL expiry, got %d detect calls", got)
	}

	// The refreshed result must be re-cached (no detect on the next call).
	if _, err := s.resolveProject("", ""); err != nil {
		t.Fatalf("resolveProject after refresh: %v", err)
	}
	if got := count.Load(); got != 2 {
		t.Errorf("expected refreshed cache to be reused, got %d detect calls", got)
	}
}
