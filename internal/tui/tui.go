package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// Run opens the interactive TUI dashboard backed by st. It uses the alternate
// screen buffer and runs until the user quits (q or ctrl+c) or an error occurs.
func Run(st *store.Store) error {
	m := newModel()
	m.store = st

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
