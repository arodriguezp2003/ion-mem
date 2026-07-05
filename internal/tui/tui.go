package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// Run opens the interactive TUI dashboard backed by st using default options.
// It uses the alternate screen buffer and runs until the user quits (q or
// ctrl+c) or an error occurs.
//
// Deprecated: prefer RunWithOptions for new call sites.
func Run(st *store.Store) error {
	return RunWithOptions(st, Options{})
}

// RunWithOptions opens the TUI dashboard with the given Options.
// opts.Version is shown in the header bar; it defaults to "dev" when empty.
func RunWithOptions(st *store.Store, opts Options) error {
	m := newModelWithOptions(opts)
	m.store = st

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
