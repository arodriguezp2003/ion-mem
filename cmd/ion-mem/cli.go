// Package main is the ion-mem CLI entry point.
//
// Currently supports two subcommands:
//
//	ion-mem mcp [--profile=...] [--data-dir=...] [--project=...]
//	ion-mem version
//
// The CLI is intentionally thin: the mcp subcommand opens the local store
// and runs the MCP stdio server until the process receives SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/store"
)

// version is the ion-mem release identifier. Bumped manually on tagged releases.
const version = "0.1.0"

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

// versionString returns the human-readable version banner printed by the
// `version` subcommand.
func versionString() string {
	return "ion-mem " + version
}

// usage returns the top-level help text printed by `ion-mem`, `ion-mem help`,
// `--help`, or `-h`.
func usage() string {
	return `Usage: ion-mem <command> [flags]

Commands:
  mcp        Start the MCP stdio server (default for agent integrations).
  version    Print the ion-mem version.
  help       Show this usage.

Run "ion-mem <command> --help" for command-specific flags.
`
}

// routeCommand dispatches the top-level CLI command. argv MUST be the full
// process arguments (including argv[0] = program name). out is the writer for
// help/version output (stderr is used internally for command errors).
//
// Returns nil on successful dispatch (or successful help/version). Returns a
// non-nil error when the command is unknown or no command was supplied.
func routeCommand(argv []string, out io.Writer) error {
	if len(argv) < 2 {
		if out != nil {
			fmt.Fprint(out, usage())
		}
		return errors.New("no command given")
	}
	cmd := argv[1]
	switch cmd {
	case "mcp":
		return runMCP(argv[2:])
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
