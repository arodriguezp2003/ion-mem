package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/ionix/ion-mem/internal/store"
	"github.com/ionix/ion-mem/internal/tui"
)

// dashConfig collects parsed flags for the `dash` subcommand.
type dashConfig struct {
	dataDir string
}

// parseDashFlags parses the `ion-mem dash` flag set.
func parseDashFlags(args []string, homeDir func() (string, error)) (dashConfig, error) {
	fs := flag.NewFlagSet("dash", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	if err := fs.Parse(args); err != nil {
		return dashConfig{}, fmt.Errorf("ion-mem dash: %w", err)
	}

	return dashConfig{dataDir: *dataDir}, nil
}

// stdoutIsTerminal is a variable so tests can override the TTY check.
// In production it delegates to go-isatty; in tests the writer is a
// *strings.Builder and isatty returns false, which is what we want.
var stdoutIsTerminal = func() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// runDash opens the store and runs the TUI dashboard. It returns an error
// when stdout is not a terminal so that scripts and CI do not hang.
func runDash(args []string) error {
	if !stdoutIsTerminal() {
		return errors.New("ion-mem dash: not a terminal — dashboard requires an interactive terminal")
	}

	cfg, err := parseDashFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	return tui.Run(st)
}
