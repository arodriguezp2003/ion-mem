package tui

// edit.go — Inline edit for observation title and type cycling from detail view.
//
// Task 2 implementation:
//   [E] EDIT TITLE — opens an inline textinput (accent border pattern from config view).
//                    Enter saves via store.UpdateObservation(Title).
//                    Esc cancels without saving.
//   [T] TYPE       — cycles through store.ValidObservationTypes (excluding session_summary).
//                    Each step persists via UpdateObservation(Type).
//
// Content editing is out of scope (documented decision: needs a textarea component).

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── cycling type order ───────────────────────────────────────────────────────

// cycleableTypes returns the sorted list of observation types available for
// cycling from the detail view. session_summary is excluded because it is
// system-generated and not a user-assignable type.
func cycleableTypes() []string {
	var types []string
	for t := range store.ValidObservationTypes {
		if t == "session_summary" {
			continue
		}
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// nextType returns the next type in the cycling order after current.
func nextType(current string) string {
	types := cycleableTypes()
	for i, t := range types {
		if t == current {
			return types[(i+1)%len(types)]
		}
	}
	// current not in list → start from first.
	if len(types) > 0 {
		return types[0]
	}
	return current
}

// ─── messages ────────────────────────────────────────────────────────────────

// obsUpdateResultMsg is sent when UpdateObservation completes (title or type).
// When err is nil, the updated observation is returned.
type obsUpdateResultMsg struct {
	obs store.Observation
	err error
	// statusMsg is shown in the detail status bar on success.
	statusMsg string
}

// ─── model fields (wired into Model struct) ───────────────────────────────────
//
// These fields are declared here for documentation; they are added to the Model
// struct in model.go.
//
//   detailEditing   bool            — true when inline title-edit input is open
//   detailEditInput textinput.Model — the inline textinput for title editing
//   detailEditOrig  string          — original title (for Esc cancel)
//   detailStatus    string          — transient status shown in the status bar
//   detailStatusOK  bool            — true = OK style, false = danger style

// ─── styles ──────────────────────────────────────────────────────────────────

var (
	detailInputActiveStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(defaultTheme.accent).
		Padding(0, 1)
)

// ─── start / cancel title edit ────────────────────────────────────────────────

func (m Model) startTitleEdit() (tea.Model, tea.Cmd) {
	if m.selectedObs == nil {
		return m, nil
	}
	m.detailEditing = true
	m.detailEditOrig = m.selectedObs.Title
	m.detailEditInput.Reset()
	m.detailEditInput.SetValue(m.selectedObs.Title)
	m.detailEditInput.Focus()
	return m, textinput.Blink
}

// handleDetailEditKey routes keys when the inline title editor is open.
func (m Model) handleDetailEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		// Cancel: restore original title.
		m.detailEditing = false
		m.detailEditInput.Blur()
		return m, nil

	case msg.Type == tea.KeyEnter:
		// Save title.
		val := strings.TrimSpace(m.detailEditInput.Value())
		if val == "" {
			val = m.detailEditOrig
		}
		m.detailEditing = false
		m.detailEditInput.Blur()
		// Optimistic update in UI while async save runs.
		if m.selectedObs != nil {
			obs := *m.selectedObs
			obs.Title = val
			m.selectedObs = &obs
		}
		return m, m.saveObsTitle(val)

	default:
		var cmd tea.Cmd
		m.detailEditInput, cmd = m.detailEditInput.Update(msg)
		return m, cmd
	}
}

// saveObsTitle builds the UpdateObservation command for a title change.
func (m Model) saveObsTitle(title string) tea.Cmd {
	if m.store == nil || m.selectedObs == nil {
		return func() tea.Msg {
			return obsUpdateResultMsg{statusMsg: "TITLE UPDATED", err: fmt.Errorf("store unavailable")}
		}
	}
	st := m.store
	id := m.selectedObs.ID
	return func() tea.Msg {
		updated, err := st.UpdateObservation(context.Background(), id, store.UpdateObservationParams{
			Title: &title,
		})
		if err != nil {
			return obsUpdateResultMsg{err: err, statusMsg: "SAVE FAILED"}
		}
		return obsUpdateResultMsg{obs: updated, statusMsg: "TITLE UPDATED"}
	}
}

// ─── cycle type ───────────────────────────────────────────────────────────────

func (m Model) cycleObsType() (tea.Model, tea.Cmd) {
	if m.selectedObs == nil {
		return m, nil
	}
	next := nextType(m.selectedObs.Type)
	// Optimistic update in UI.
	obs := *m.selectedObs
	obs.Type = next
	m.selectedObs = &obs
	m.vp.SetContent(renderObservationDetail(obs))
	return m, m.saveObsType(next)
}

// saveObsType builds the UpdateObservation command for a type change.
func (m Model) saveObsType(typeName string) tea.Cmd {
	if m.store == nil || m.selectedObs == nil {
		return func() tea.Msg {
			badge := renderBadge(typeName)
			return obsUpdateResultMsg{statusMsg: fmt.Sprintf("TYPE → %s", stripAnsiCodes(badge)), err: fmt.Errorf("store unavailable")}
		}
	}
	st := m.store
	id := m.selectedObs.ID
	return func() tea.Msg {
		updated, err := st.UpdateObservation(context.Background(), id, store.UpdateObservationParams{
			Type: &typeName,
		})
		badge := renderBadge(typeName)
		if err != nil {
			return obsUpdateResultMsg{err: err, statusMsg: "SAVE FAILED"}
		}
		return obsUpdateResultMsg{obs: updated, statusMsg: fmt.Sprintf("TYPE → %s", stripAnsiCodes(badge))}
	}
}
