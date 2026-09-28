package joblog_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/joblog"
)

// ─── Open / Info / Debug ──────────────────────────────────────────────────────

func TestOpen_CreatesLogsDirAndFile(t *testing.T) {
	dir := t.TempDir()

	lg, err := joblog.Open(dir, "embeddings", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer lg.Close()

	wantPath := filepath.Join(dir, "logs", "embeddings.log")
	if lg.Path() != wantPath {
		t.Errorf("Path() = %q, want %q", lg.Path(), wantPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); err != nil {
		t.Errorf("logs dir not created: %v", err)
	}
}

func TestInfo_WritesLineWithLevelAndTimestamp(t *testing.T) {
	dir := t.TempDir()
	lg, err := joblog.Open(dir, "embeddings", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	lg.Info("job started model=%s", "bge-m3")
	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readLines(t, lg.Path())
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
	line := lines[0]
	if !strings.Contains(line, " INFO ") {
		t.Errorf("line %q does not contain level INFO", line)
	}
	if !strings.Contains(line, "job started model=bge-m3") {
		t.Errorf("line %q does not contain the message", line)
	}
	fields := strings.SplitN(line, " ", 2)
	if len(fields) < 1 || fields[0] == "" {
		t.Fatalf("line %q missing timestamp prefix", line)
	}
}

func TestDebug_SuppressedWhenNotVerbose(t *testing.T) {
	dir := t.TempDir()
	lg, err := joblog.Open(dir, "embeddings", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	lg.Debug("should not appear")
	lg.Info("should appear")
	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readLines(t, lg.Path())
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (Debug must be suppressed): %v", len(lines), lines)
	}
	if strings.Contains(lines[0], "should not appear") {
		t.Errorf("Debug line leaked when verbose=false: %q", lines[0])
	}
}

func TestDebug_EmittedWhenVerbose(t *testing.T) {
	dir := t.TempDir()
	lg, err := joblog.Open(dir, "embeddings", true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	lg.Debug("debug line %d", 1)
	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readLines(t, lg.Path())
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], " DEBUG ") {
		t.Errorf("line %q does not contain level DEBUG", lines[0])
	}
	if !strings.Contains(lines[0], "debug line 1") {
		t.Errorf("line %q does not contain the message", lines[0])
	}
}

// ─── rotation ──────────────────────────────────────────────────────────────────

func TestOpen_RotatesOversizedLogFile(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(logsDir, "embeddings.log")

	oldContent := strings.Repeat("x", 6*1024*1024) // 6 MiB, over the 5 MiB threshold
	if err := os.WriteFile(path, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("seed old log: %v", err)
	}

	lg, err := joblog.Open(dir, "embeddings", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer lg.Close()

	rotatedPath := path + ".1"
	rotated, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("rotated file not found: %v", err)
	}
	if string(rotated) != oldContent {
		t.Errorf("rotated file content does not match the original oversized log")
	}

	lg.Info("fresh line")
	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("new log file got %d lines, want 1 (should start fresh): %v", len(lines), lines)
	}
}

func TestOpen_RotationOverwritesPreviousDotOne(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(logsDir, "embeddings.log")
	rotatedPath := path + ".1"

	if err := os.WriteFile(rotatedPath, []byte("ancient rotated content"), 0o644); err != nil {
		t.Fatalf("seed old rotated file: %v", err)
	}
	oldContent := strings.Repeat("y", 6*1024*1024)
	if err := os.WriteFile(path, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("seed oversized log: %v", err)
	}

	lg, err := joblog.Open(dir, "embeddings", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer lg.Close()

	rotated, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("rotated file not found: %v", err)
	}
	if string(rotated) != oldContent {
		t.Errorf("rotated .1 file was not overwritten with the newly rotated content")
	}
}

func TestOpen_DoesNotRotateUndersizedLogFile(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(logsDir, "embeddings.log")
	if err := os.WriteFile(path, []byte("small\n"), 0o644); err != nil {
		t.Fatalf("seed small log: %v", err)
	}

	lg, err := joblog.Open(dir, "embeddings", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer lg.Close()

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("rotated .1 file should not exist for an undersized log")
	}

	lg.Info("appended")
	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (append, not truncate): %v", len(lines), lines)
	}
	if lines[0] != "small" {
		t.Errorf("first line = %q, want %q (original content preserved)", lines[0], "small")
	}
}

// ─── Nop ───────────────────────────────────────────────────────────────────────

func TestNop_NeverPanicsAndPathIsEmpty(t *testing.T) {
	lg := joblog.Nop()
	lg.Info("info %d", 1)
	lg.Debug("debug %d", 2)
	if lg.Path() != "" {
		t.Errorf("Nop Path() = %q, want empty", lg.Path())
	}
	if err := lg.Close(); err != nil {
		t.Errorf("Nop Close() = %v, want nil", err)
	}
}

// ─── concurrency ────────────────────────────────────────────────────────────────

func TestLogger_ConcurrentWritesAreSafe(t *testing.T) {
	dir := t.TempDir()
	lg, err := joblog.Open(dir, "embeddings", true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			lg.Info("info %d", i)
			lg.Debug("debug %d", i)
		}(i)
	}
	wg.Wait()

	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readLines(t, lg.Path())
	if len(lines) != n*2 {
		t.Fatalf("got %d lines, want %d", len(lines), n*2)
	}
}

// ─── ResolveVerbose ─────────────────────────────────────────────────────────────

func TestResolveVerbose(t *testing.T) {
	trueFlag := true
	falseFlag := false

	tests := []struct {
		name    string
		setting string
		env     string
		flag    *bool
		want    bool
	}{
		{"flag true wins over everything", "false", "0", &trueFlag, true},
		{"flag false wins over env and setting", "true", "1", &falseFlag, false},
		{"no flag, env 1 is verbose", "false", "1", nil, true},
		{"no flag, env true is verbose", "false", "true", nil, true},
		{"no flag, env empty falls back to setting true", "true", "", nil, true},
		{"no flag, env empty falls back to setting false", "false", "", nil, false},
		{"no flag, env unrecognized falls back to setting", "true", "yes", nil, true},
		{"nothing set at all", "", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joblog.ResolveVerbose(tt.setting, tt.env, tt.flag)
			if got != tt.want {
				t.Errorf("ResolveVerbose(%q, %q, %v) = %v, want %v", tt.setting, tt.env, tt.flag, got, tt.want)
			}
		})
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────────

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}
