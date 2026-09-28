// cli_help.go — shared `--help` handling for every subcommand's flag set.
//
// Every parse*Flags function creates a flag.FlagSet with fs.SetOutput(io.Discard)
// so that Go's flag package doesn't print its own (unhelpfully terse) usage
// text on a bad flag — command errors are formatted and surfaced by the
// caller instead. That silence, however, was also swallowing `--help`: since
// -h/--help makes fs.Parse return flag.ErrHelp, `ion-mem <cmd> --help` printed
// nothing and exited 1, contradicting the promise in cli.go's usage() text
// ("Run ion-mem <command> --help for command-specific flags").
//
// parseFlagsWithHelp fixes this generically: it wraps fs.Parse and, on
// flag.ErrHelp specifically, prints a usage header plus fs's flag
// descriptions to out and returns the errHelpShown sentinel instead of the
// raw error. main() treats errHelpShown like a successful command (exit 0,
// no "ion-mem: <err>" line) — see main.go.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// errHelpShown is returned by parseFlagsWithHelp when --help/-h was
// requested and usage has already been printed. It is never a "real" error:
// callers propagate it unchanged (often wrapped via fmt.Errorf's %w) all the
// way up to main, which unwraps it via errors.Is and exits 0 silently.
var errHelpShown = errors.New("help requested")

// parseFlagsWithHelp parses fs and special-cases flag.ErrHelp: it prints
// "Usage: ion-mem <name> [flags]" followed by fs's flag descriptions to out,
// then returns errHelpShown. Any other outcome (success, or a bad-flag
// error) is returned unchanged, preserving existing error-handling behavior
// for invalid flags.
func parseFlagsWithHelp(fs *flag.FlagSet, args []string, out io.Writer) error {
	err := fs.Parse(args)
	if !errors.Is(err, flag.ErrHelp) {
		return err
	}
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintf(out, "Usage: ion-mem %s [flags]\n\n", fs.Name())
	// fs's own output was set to io.Discard by the caller (see package doc
	// above); point it at out so PrintDefaults is actually visible.
	fs.SetOutput(out)
	fs.PrintDefaults()
	return errHelpShown
}
