package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// signalNotifyContext returns a context that cancels on SIGINT or SIGTERM.
// Extracted so the platform-specific syscall import lives next to the only
// caller and not in cli.go (which stays platform-agnostic for testability).
func signalNotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

// exitCoder is implemented by errors that carry a specific process exit
// code (e.g. doctorExitError, used by `ion-mem doctor` to distinguish
// degraded (1) from down (2)). Errors that don't implement it exit 1, as
// before.
type exitCoder interface {
	ExitCode() int
}

func main() {
	err := routeCommand(os.Args, os.Stdout)
	if err == nil {
		return
	}
	// Distinguish "user error" (usage problems) from "operational error" (server
	// failure). Both exit non-zero; usage errors get a calmer message.
	if errors.Is(err, context.Canceled) {
		return // graceful shutdown
	}
	if errors.Is(err, errHelpShown) {
		return // usage already printed by parseFlagsWithHelp; exit 0, no error line
	}
	fmt.Fprintf(os.Stderr, "ion-mem: %v\n", err)
	code := 1
	var ec exitCoder
	if errors.As(err, &ec) {
		code = ec.ExitCode()
	}
	os.Exit(code)
}
