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
	fmt.Fprintf(os.Stderr, "ion-mem: %v\n", err)
	os.Exit(1)
}
