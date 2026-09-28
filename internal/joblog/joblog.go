// Package joblog provides a small, thread-safe file logger for long-running
// CLI jobs (e.g. backfill-embeddings). It intentionally does not aim to be a
// general-purpose logging framework: one file per job name, size-based
// rotation, and two levels (INFO, DEBUG).
package joblog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxLogSizeBytes is the size threshold checked at Open time. If the target
// log file already exceeds this size, it is rotated to <name>.log.1 before a
// fresh file is opened for appending.
const maxLogSizeBytes = 5 * 1024 * 1024 // 5 MiB

// timeLayout is the timestamp format used as the prefix of every log line:
// millisecond precision, RFC3339-like, e.g. "2006-01-02T15:04:05.000Z".
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// Logger writes leveled, timestamped lines to a single log file. The zero
// value is not directly useful; construct via Open or Nop. All methods are
// safe for concurrent use.
type Logger struct {
	mu      sync.Mutex
	f       *os.File
	path    string
	verbose bool
}

// Open creates <dataDir>/logs/ (0o755) if needed and opens (creating or
// appending to) <dataDir>/logs/<name>.log (0o644) for writing. If the file
// already exceeds maxLogSizeBytes, it is rotated to <name>.log.1 (overwriting
// any previous .1) before the fresh file is opened.
//
// Open is intended for single-process use: rotation and the subsequent open
// are not guarded by a file lock, so two processes opening the same log
// concurrently may race on rotation.
//
// verbose controls whether Debug calls produce output.
func Open(dataDir string, name string, verbose bool) (*Logger, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("joblog.Open: dataDir must not be empty")
	}
	if name == "" {
		return nil, fmt.Errorf("joblog.Open: name must not be empty")
	}

	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("joblog.Open: mkdir %s: %w", logsDir, err)
	}

	path := filepath.Join(logsDir, name+".log")
	if err := rotateIfOversized(path); err != nil {
		return nil, fmt.Errorf("joblog.Open: rotate %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("joblog.Open: open %s: %w", path, err)
	}

	return &Logger{f: f, path: path, verbose: verbose}, nil
}

// rotateIfOversized renames path to path+".1" (overwriting any existing .1)
// when path already exists and exceeds maxLogSizeBytes. A missing path is not
// an error; any other stat error is returned.
func rotateIfOversized(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() <= maxLogSizeBytes {
		return nil
	}

	rotated := path + ".1"
	_ = os.Remove(rotated) // best-effort: ignore a missing previous .1
	return os.Rename(path, rotated)
}

// Nop returns a Logger that discards everything written to it. Callers that
// fail to open a real log file (or tests that don't care about logging) can
// fall back to this so logging failures never crash the caller.
func Nop() *Logger {
	return &Logger{}
}

// Path returns the log file path, or "" for a Nop logger.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Info writes an INFO-level line, formatted like fmt.Sprintf.
func (l *Logger) Info(format string, args ...any) {
	l.write("INFO", format, args...)
}

// Debug writes a DEBUG-level line, formatted like fmt.Sprintf. It is a no-op
// unless the logger was opened (or configured) with verbose = true.
func (l *Logger) Debug(format string, args ...any) {
	if l == nil || !l.verbose {
		return
	}
	l.write("DEBUG", format, args...)
}

// write formats and appends one log line. It is a safe no-op for a Nop
// logger (l.f == nil) and deliberately ignores write errors: logging must
// never crash the caller.
func (l *Logger) write(level, format string, args ...any) {
	if l == nil || l.f == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	line := strings.Join([]string{time.Now().UTC().Format(timeLayout), level, msg}, " ") + "\n"

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.WriteString(line)
}

// Close closes the underlying file. Safe to call on a Nop logger (no-op).
func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// ResolveVerbose determines whether verbose logging should be enabled, in
// order of precedence:
//
//  1. flag, when non-nil (an explicit --verbose/--no-verbose on the command line)
//  2. env, when it is "1" or "true" (the ION_MEM_VERBOSE environment variable)
//  3. setting, when it equals "true" (the log.verbose stored setting)
//
// It is a pure function so callers can unit test it without touching the
// environment or flag package.
func ResolveVerbose(setting string, env string, flag *bool) bool {
	if flag != nil {
		return *flag
	}
	if env == "1" || env == "true" {
		return true
	}
	return setting == "true"
}
