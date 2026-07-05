// Package main is the ion-mem CLI entry point.
//
// Supports subcommands:
//
//	ion-mem mcp           [--profile=...] [--data-dir=...] [--project=...]
//	ion-mem session-start --id=X --project=Y --cwd=Z [--data-dir=...]
//	ion-mem session-end   --id=X [--summary=Y] [--data-dir=...]
//	ion-mem context       --project=X [--scope=Y] [--data-dir=...]
//	ion-mem save-prompt   --session-id=X --content=Y [--project=Z] [--data-dir=...]
//	ion-mem search        <query> [--project=...] [--all-projects] [--limit=10] [--type=...] [--data-dir=...] [--json]
//	ion-mem version
//
// The CLI is intentionally thin: the mcp subcommand opens the local store
// and runs the MCP stdio server until the process receives SIGINT/SIGTERM.
// All other subcommands open the store, perform a single operation, then exit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/store"
)

// version is the ion-mem release identifier. Set at build time via
// -ldflags "-X main.version=v1.2.3"; when unset, versionString falls back
// to the module version recorded by the Go toolchain (go install @version).
var version = ""

// mcpConfig collects the parsed flags for the `mcp` subcommand.
type mcpConfig struct {
	profile string
	dataDir string
	project string
}

// parseMCPFlags parses the `ion-mem mcp` flag set. getenv is injected so tests
// can supply deterministic environment values; homeDir is injected so tests do
// not depend on the real user's home directory.
//
// Precedence for `project`: explicit --project flag > ION_MEM_PROJECT env > empty
// (server auto-detects per call via project.DetectFull).
func parseMCPFlags(args []string, getenv func(string) string, homeDir func() (string, error)) (mcpConfig, error) {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress flag library noise; caller decides how to surface errors.

	envProject := getenv("ION_MEM_PROJECT")

	profile := fs.String("profile", "agent", `Tool profile: "agent" (default) or "all".`)
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	project := fs.String("project", envProject, "Project name override (or ION_MEM_PROJECT env).")

	if err := fs.Parse(args); err != nil {
		return mcpConfig{}, fmt.Errorf("ion-mem mcp: %w", err)
	}

	return mcpConfig{
		profile: *profile,
		dataDir: *dataDir,
		project: *project,
	}, nil
}

// defaultDataDir returns the resolved data directory: `<home>/.ion-mem` when the
// home directory is available, else `.ion-mem` relative to cwd (best-effort
// fallback for headless / CI environments where HOME is unset).
func defaultDataDir(homeDir func() (string, error)) string {
	home, err := homeDir()
	if err != nil || home == "" {
		return ".ion-mem"
	}
	return filepath.Join(home, ".ion-mem")
}

// resolvedVersion returns the effective version. Resolution order:
// ldflags-injected version, module version from build info
// (go install @version), then "dev".
func resolvedVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// versionString returns the human-readable version banner printed by the
// `version` subcommand.
func versionString() string {
	return "ion-mem " + resolvedVersion()
}

// banner returns the ASCII-art identity card printed before usage() output.
// Rendered with figlet's `colossal` font from "ION MEM" — embed verbatim so
// the binary has no runtime dependency on figlet. A function (not a const)
// because the version is resolved at runtime via versionString.
func banner() string {
	return `
8888888 .d88888b. 888b    888   888b     d8888888888888888b     d888
  888  d88P" "Y88b8888b   888   8888b   d8888888       8888b   d8888
  888  888     88888888b  888   88888b.d88888888       88888b.d88888
  888  888     888888Y88b 888   888Y88888P8888888888   888Y88888P888
  888  888     888888 Y88b888   888 Y888P 888888       888 Y888P 888
  888  888     888888  Y88888   888  Y8P  888888       888  Y8P  888
  888  Y88b. .d88P888   Y8888   888   "   888888       888   "   888
8888888 "Y88888P" 888    Y888   888       8888888888888888       888

Persistent memory for AI coding agents — local-first, team-grade   ` + resolvedVersion() + `
`
}

// usage returns the top-level help text printed by `ion-mem`, `ion-mem help`,
// `--help`, or `-h`. Starts with the banner identity card.
func usage() string {
	return banner() + `
Usage: ion-mem <command> [flags]

Commands:
  mcp                  Start the MCP stdio server (default for agent integrations).
  dash                 Open the interactive TUI dashboard (requires a terminal).
  session-start        Create a new session in the store.
  session-end          Mark a session as ended.
  context              Print a markdown context summary for a project.
  save-prompt          Record a user prompt for a session.
  search               One-shot search: ion-mem search <query> [--project=...] [--json].
  status               One-shot health snapshot: stats, recent items, alerts.
  eval                 Run search quality evaluation against a golden query set.
  backfill-embeddings  Embed all observations that lack a vector row (requires Ollama).
  version              Print the ion-mem version.
  help                 Show this usage.

Run "ion-mem <command> --help" for command-specific flags.

When invoked with no arguments in an interactive terminal, ion-mem opens
the dashboard automatically (equivalent to "ion-mem dash").
`
}

// routeCommand dispatches the top-level CLI command. argv MUST be the full
// process arguments (including argv[0] = program name). out is the writer for
// help/version output (stderr is used internally for command errors).
//
// Returns nil on successful dispatch (or successful help/version). Returns a
// non-nil error when the command is unknown or no command was supplied.
//
// When no command is supplied AND stdout is an interactive terminal, the
// dashboard is launched instead of printing usage. On a non-TTY the historic
// behavior is preserved (print usage + return error) so scripts and CI are
// unaffected.
func routeCommand(argv []string, out io.Writer) error {
	if len(argv) < 2 {
		// Bare invocation: launch dashboard on TTY, show usage otherwise.
		if stdoutIsTerminal() {
			return runDash(nil)
		}
		if out != nil {
			fmt.Fprint(out, usage())
		}
		return errors.New("no command given")
	}
	cmd := argv[1]
	switch cmd {
	case "mcp":
		return runMCP(argv[2:])
	case "dash":
		return runDash(argv[2:])
	case "session-start":
		return runSessionStart(argv[2:])
	case "session-end":
		return runSessionEnd(argv[2:])
	case "context":
		return runContext(argv[2:], out)
	case "save-prompt":
		return runSavePrompt(argv[2:])
	case "search":
		return runSearch(argv[2:], out, os.Stderr)
	case "status":
		return runStatus(argv[2:], out)
	case "eval":
		return runEval(argv[2:], out)
	case "backfill-embeddings":
		return runBackfill(argv[2:], out)
	case "version":
		if out != nil {
			fmt.Fprintln(out, versionString())
		}
		return nil
	case "help", "--help", "-h":
		if out != nil {
			fmt.Fprint(out, usage())
		}
		return nil
	default:
		if out != nil {
			fmt.Fprint(out, usage())
		}
		return fmt.Errorf("unknown command: %q", cmd)
	}
}

// ─── session-start ────────────────────────────────────────────────────────────

// sessionStartConfig collects the parsed flags for the `session-start` subcommand.
type sessionStartConfig struct {
	id      string
	project string
	cwd     string
	dataDir string
}

// parseSessionStartFlags parses the `ion-mem session-start` flag set.
// Returns an error when any required flag (--id, --project, --cwd) is missing.
func parseSessionStartFlags(args []string, homeDir func() (string, error)) (sessionStartConfig, error) {
	fs := flag.NewFlagSet("session-start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	id := fs.String("id", "", "Session identifier (required).")
	project := fs.String("project", "", "Project name (required).")
	cwd := fs.String("cwd", "", "Working directory (required).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	if err := fs.Parse(args); err != nil {
		return sessionStartConfig{}, fmt.Errorf("ion-mem session-start: %w", err)
	}

	if *id == "" {
		return sessionStartConfig{}, fmt.Errorf("ion-mem session-start: --id is required")
	}
	if *project == "" {
		return sessionStartConfig{}, fmt.Errorf("ion-mem session-start: --project is required")
	}
	if *cwd == "" {
		return sessionStartConfig{}, fmt.Errorf("ion-mem session-start: --cwd is required")
	}

	return sessionStartConfig{
		id:      *id,
		project: *project,
		cwd:     *cwd,
		dataDir: *dataDir,
	}, nil
}

// runSessionStart opens the store and creates a session. Idempotent: if the
// session ID already exists the command returns 0 silently.
func runSessionStart(args []string) error {
	cfg, err := parseSessionStartFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	_, err = st.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        cfg.id,
		Project:   cfg.project,
		Directory: cfg.cwd,
	})
	if err != nil {
		// Idempotent: if the primary-key already exists, treat as success.
		if isUniqueConstraintError(err) {
			return nil
		}
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// isUniqueConstraintError returns true when err is a SQLite UNIQUE constraint
// violation, used to implement idempotency in session-start.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint")
}

// ─── session-end ──────────────────────────────────────────────────────────────

// sessionEndConfig collects the parsed flags for the `session-end` subcommand.
type sessionEndConfig struct {
	id      string
	summary string
	dataDir string
}

// parseSessionEndFlags parses the `ion-mem session-end` flag set.
// Returns an error when the required --id flag is missing.
func parseSessionEndFlags(args []string, homeDir func() (string, error)) (sessionEndConfig, error) {
	fs := flag.NewFlagSet("session-end", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	id := fs.String("id", "", "Session identifier (required).")
	summary := fs.String("summary", "", "Optional session summary.")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	if err := fs.Parse(args); err != nil {
		return sessionEndConfig{}, fmt.Errorf("ion-mem session-end: %w", err)
	}

	if *id == "" {
		return sessionEndConfig{}, fmt.Errorf("ion-mem session-end: --id is required")
	}

	return sessionEndConfig{
		id:      *id,
		summary: *summary,
		dataDir: *dataDir,
	}, nil
}

// runSessionEnd opens the store and marks a session as ended. Silently returns
// 0 when the session does not exist (already ended or never created).
func runSessionEnd(args []string) error {
	cfg, err := parseSessionEndFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	err = st.EndSession(context.Background(), cfg.id, cfg.summary)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // idempotent: already ended or never existed
		}
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

// ─── context ──────────────────────────────────────────────────────────────────

// contextConfig collects the parsed flags for the `context` subcommand.
type contextConfig struct {
	project string
	scope   string
	dataDir string
}

// parseContextFlags parses the `ion-mem context` flag set.
// Returns an error when the required --project flag is missing.
func parseContextFlags(args []string, homeDir func() (string, error)) (contextConfig, error) {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	project := fs.String("project", "", "Project name (required).")
	scope := fs.String("scope", "project", "Scope filter (default: project).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	if err := fs.Parse(args); err != nil {
		return contextConfig{}, fmt.Errorf("ion-mem context: %w", err)
	}

	if *project == "" {
		return contextConfig{}, fmt.Errorf("ion-mem context: --project is required")
	}

	return contextConfig{
		project: *project,
		scope:   *scope,
		dataDir: *dataDir,
	}, nil
}

// runContext opens the store, composes a markdown context summary for the
// given project, and prints it to out. Empty store prints empty markdown (not an error).
func runContext(args []string, out io.Writer) error {
	cfg, err := parseContextFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	sessions, err := st.RecentSessions(ctx, cfg.project, 10)
	if err != nil {
		return fmt.Errorf("fetch sessions: %w", err)
	}

	obsParams := store.RecentObservationsParams{
		Project: cfg.project,
		Scope:   cfg.scope,
		Limit:   10,
	}
	obs, err := st.RecentObservations(ctx, obsParams)
	if err != nil {
		return fmt.Errorf("fetch observations: %w", err)
	}

	md := buildContextMarkdownCLI(cfg.project, sessions, obs)
	if out != nil {
		fmt.Fprint(out, md)
	}
	return nil
}

// buildContextMarkdownCLI assembles the context markdown string from sessions
// and observations. Mirrors the logic in internal/mcp/tool_context.go's
// buildContextMarkdown but is inlined here to avoid a package-level dependency
// from cmd/ into internal/mcp (which also imports internal/store — creating a
// layering issue). If this format diverges in the future, extract to a shared
// internal/context package.
func buildContextMarkdownCLI(project string, sessions []store.Session, obs []store.Observation) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Context: %s\n\n", project))

	sb.WriteString("## Recent Sessions\n\n")
	if len(sessions) == 0 {
		sb.WriteString("_No sessions found._\n\n")
	} else {
		for _, sess := range sessions {
			sb.WriteString(fmt.Sprintf("- **%s** (status: %s, started: %s)\n",
				sess.ID, sess.Status, sess.StartedAt.Format("2006-01-02 15:04:05")))
			if sess.Summary != nil && *sess.Summary != "" {
				sb.WriteString(fmt.Sprintf("  - Summary: %s\n", *sess.Summary))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Recent Observations\n\n")
	if len(obs) == 0 {
		sb.WriteString("_No observations found._\n\n")
	} else {
		for _, o := range obs {
			sb.WriteString(fmt.Sprintf("- [%d] **%s** (%s)\n", o.ID, o.Title, o.Type))
			if o.Content != "" {
				preview := o.Content
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("  > %s\n", preview))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ─── save-prompt ──────────────────────────────────────────────────────────────

// savePromptConfig collects the parsed flags for the `save-prompt` subcommand.
type savePromptConfig struct {
	sessionID string
	content   string
	project   string
	dataDir   string
}

// parseSavePromptFlags parses the `ion-mem save-prompt` flag set.
// Returns an error when required flags (--session-id, --content) are missing.
func parseSavePromptFlags(args []string, homeDir func() (string, error)) (savePromptConfig, error) {
	fs := flag.NewFlagSet("save-prompt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	sessionID := fs.String("session-id", "", "Session identifier (required).")
	content := fs.String("content", "", "Prompt content (required).")
	project := fs.String("project", "", "Project name (optional; derived from session if empty).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	if err := fs.Parse(args); err != nil {
		return savePromptConfig{}, fmt.Errorf("ion-mem save-prompt: %w", err)
	}

	if *sessionID == "" {
		return savePromptConfig{}, fmt.Errorf("ion-mem save-prompt: --session-id is required")
	}
	if *content == "" {
		return savePromptConfig{}, fmt.Errorf("ion-mem save-prompt: --content is required")
	}

	return savePromptConfig{
		sessionID: *sessionID,
		content:   *content,
		project:   *project,
		dataDir:   *dataDir,
	}, nil
}

// runSavePrompt opens the store and records a user prompt. When --project is
// empty it looks up the session's project. Returns an error when the session
// does not exist (FK constraint).
func runSavePrompt(args []string) error {
	cfg, err := parseSavePromptFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()
	project := cfg.project

	// If project was not supplied, derive it from the session.
	if project == "" {
		sess, err := st.GetSession(ctx, cfg.sessionID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("save-prompt: session %q not found", cfg.sessionID)
			}
			return fmt.Errorf("save-prompt: lookup session: %w", err)
		}
		project = sess.Project
	}

	_, err = st.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: cfg.sessionID,
		Content:   cfg.content,
		Project:   project,
	})
	if err != nil {
		return fmt.Errorf("save-prompt: %w", err)
	}
	return nil
}

// ─── mcp ─────────────────────────────────────────────────────────────────────

// runMCP wires the local store + mcp server and blocks until ctx is cancelled
// (SIGINT/SIGTERM) or the server returns an error. Not unit-tested — exercised
// by the `ion-mem mcp` smoke test.
func runMCP(args []string) error {
	cfg, err := parseMCPFlags(args, os.Getenv, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store at %q: %w", cfg.dataDir, err)
	}
	defer st.Close()

	srv := mcp.New(st,
		mcp.WithProfile(cfg.profile),
		mcp.WithDefaultProject(cfg.project),
	)

	ctx, cancel := signalNotifyContext(context.Background())
	defer cancel()

	if err := srv.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp serve: %w", err)
	}
	return nil
}
