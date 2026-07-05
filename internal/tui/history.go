package tui

// history.go — Revision history view and revision content view.
//
// viewHistory: windowed list of revisions for the currently selected observation.
//   Key 'h' in viewDetail (only when RevisionCount > 1) → viewHistory.
//   Enter on a revision → viewRevisionContent (read-only body viewport).
//   Esc → viewDetail.
//
// viewRevisionContent: read-only viewport for a single historical revision.
//   Esc → viewHistory.
//
// Async loading: pressing 'h' issues fetchRevisions() which returns a
// revisionsLoadedMsg that populates m.revisions. The empty state
// "░░ NO HISTORY ░░" is render-safe (RevisionCount > 1 gates the key so this
// state should not be reachable in practice, but the renderer handles it).

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── messages ─────────────────────────────────────────────────────────────────

// revisionsLoadedMsg is sent when ListRevisions returns.
type revisionsLoadedMsg struct {
	revisions []store.Revision
}

// ─── footer legend ────────────────────────────────────────────────────────────

const (
	bbsFooterHistory         = "[↑↓] MOVE  [⏎] VIEW  [ESC] BACK  [Q] QUIT"
	bbsFooterRevisionContent = "[↑↓] SCROLL  [ESC] BACK  [Q] QUIT"
)

// ─── command ─────────────────────────────────────────────────────────────────

// fetchRevisions loads the revision history for the currently selected
// observation. It posts a revisionsLoadedMsg when done.
func (m Model) fetchRevisions() tea.Cmd {
	if m.store == nil || m.selectedObs == nil {
		return func() tea.Msg {
			return revisionsLoadedMsg{revisions: []store.Revision{}}
		}
	}
	st := m.store
	id := m.selectedObs.ID
	return func() tea.Msg {
		revs, err := st.ListRevisions(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return revisionsLoadedMsg{revisions: revs}
	}
}

// ─── key handler ─────────────────────────────────────────────────────────────

func (m Model) handleKeyHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.view = viewDetail
		return m, nil

	case msg.Type == tea.KeyUp || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'k'):
		if m.histCursor > 0 {
			m.histCursor--
			m.histOffset = clampWindow(m.histCursor, m.histOffset, m.listVisibleHeight(false, false), len(m.revisions))
		}

	case msg.Type == tea.KeyDown || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'j'):
		if m.histCursor < len(m.revisions)-1 {
			m.histCursor++
			m.histOffset = clampWindow(m.histCursor, m.histOffset, m.listVisibleHeight(false, false), len(m.revisions))
		}

	case msg.Type == tea.KeyEnter:
		if len(m.revisions) == 0 {
			return m, nil
		}
		rev := m.revisions[m.histCursor]
		m.selectedRevision = &rev
		// Set up the viewport to display the revision content.
		m.revVP.Width = effectiveWidth(m.width)
		m.revVP.Height = m.revVPHeight()
		m.revVP.SetContent(rev.Content)
		m.revVP.GotoTop()
		m.view = viewRevisionContent
		return m, nil

	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 'q') || msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleKeyRevisionContent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.selectedRevision = nil
		m.view = viewHistory
		return m, nil

	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 'q') || msg.Type == tea.KeyCtrlC:
		return m, tea.Quit

	default:
		var cmd tea.Cmd
		m.revVP, cmd = m.revVP.Update(msg)
		return m, cmd
	}
}

// ─── viewport height ─────────────────────────────────────────────────────────

// revVPMetaLineCount returns the number of header/meta lines consumed in the
// revision content view above the body viewport. Fixed: title(1) + rule(1) = 2.
func revVPMetaLineCount() int {
	return 2
}

// revVPHeight returns the available viewport height for the revision content view.
func (m Model) revVPHeight() int {
	meta := revVPMetaLineCount()
	h := m.height - headerRows - statusRows - meta
	if h < 1 {
		h = 1
	}
	return h
}

// ─── view: history list ───────────────────────────────────────────────────────

func (m Model) viewHistoryPage() string {
	if m.selectedObs == nil {
		return ""
	}

	// ── chrome ──────────────────────────────────────────────────────────────
	breadcrumb := "Projects // " + m.selectedProject + " // Detail // History"
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	cOffset := contentOffset(m.width)
	cWidth := effectiveWidth(m.width)
	if cWidth < 40 {
		cWidth = 40
	}
	rowIndent := strings.Repeat(" ", cOffset+leftPad)

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	if len(m.revisions) == 0 {
		emptyText := "░░ NO HISTORY ░░"
		content.WriteString(rowIndent + dimStyle.Render(emptyText) + "\n")
	} else {
		visible := m.listVisibleHeight(false, false)
		offset := m.histOffset
		total := len(m.revisions)

		showUp, showDown := overflowMarkers(offset, visible, total)
		if showUp {
			content.WriteString(rowIndent + mutedStyle.Render("↑ more") + "\n")
		}

		// Column widths for: revTag(4) + space + age(12) + space + title(rest)
		revTagWidth := 4 // "r999"
		ageWidth := 12   // "26d ago" max ~12 chars
		titleWidth := cWidth - leftPad - revTagWidth - 1 - ageWidth - 1 - rightPad
		if titleWidth < 10 {
			titleWidth = 10
		}

		end := offset + visible
		if end > total {
			end = total
		}
		for i := offset; i < end; i++ {
			rev := m.revisions[i]
			revTag := fmt.Sprintf("r%-3d", rev.Revision)
			ageStr := humanizeTime(parseCreatedAt(rev.ArchivedAt))
			title := truncStr(rev.Title, titleWidth)

			titleFmt := fmt.Sprintf("%-*s", titleWidth, title)
			usedLeft := leftPad + revTagWidth + 1 + titleWidth + 1
			gap := cWidth - usedLeft - len(ageStr) - rightPad
			if gap < 1 {
				gap = 1
			}
			ageRendered := dimStyle.Render(ageStr)

			var row string
			if i == m.histCursor {
				rowContent := strings.Repeat(" ", leftPad) + revTag + " " + titleFmt + strings.Repeat(" ", gap) + ageStr
				row = strings.Repeat(" ", cOffset) + selectedRowStyle.Render(rowContent)
			} else {
				row = rowIndent + dimStyle.Render(revTag) + " " + titleFmt + strings.Repeat(" ", gap) + ageRendered
			}
			content.WriteString(row + "\n")
		}

		if showDown {
			content.WriteString(rowIndent + mutedStyle.Render("↓ more") + "\n")
		}
	}

	// ── status and footer ────────────────────────────────────────────────────
	pos := positionIndicator(m.histCursor, len(m.revisions))
	statusText := fmt.Sprintf("HISTORY — %d REVISION(S)", len(m.revisions))
	if pos != "" {
		statusText += "  " + pos
	}
	if m.err != nil {
		statusText = "ERROR: " + m.err.Error()
	}
	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusText)
	footerLine := strings.Repeat(" ", cOffset+leftPad) + dimStyle.Render(bbsFooterHistory)

	// ── compose full-height layout ───────────────────────────────────────────
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}

// ─── view: revision content (read-only viewport) ──────────────────────────────

func (m Model) viewRevisionContentPage() string {
	if m.selectedRevision == nil || m.selectedObs == nil {
		return ""
	}
	rev := m.selectedRevision
	obs := m.selectedObs

	// ── chrome ──────────────────────────────────────────────────────────────
	breadcrumb := fmt.Sprintf("Projects // %s // Detail // History // r%d", m.selectedProject, rev.Revision)
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	cOffset := contentOffset(m.width)
	cWidth := effectiveWidth(m.width)
	if cWidth < 40 {
		cWidth = 40
	}
	metaIndent := strings.Repeat(" ", cOffset+leftPad)

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	// Title: show old title from revision (not the current obs title)
	content.WriteString(metaIndent + selectedRowStyle.Render(rev.Title) + "\n")

	// Rule separating title from content.
	ruleWidth := cWidth
	if ruleWidth < 1 {
		ruleWidth = 40
	}
	content.WriteString(strings.Repeat(" ", cOffset) + mutedStyle.Render(strings.Repeat("═", ruleWidth)) + "\n")

	// Viewport body.
	vpContent := m.revVP.View()
	if cOffset > 0 && vpContent != "" {
		indent := strings.Repeat(" ", cOffset)
		bodyLines := strings.Split(vpContent, "\n")
		for i, l := range bodyLines {
			if l != "" {
				bodyLines[i] = indent + l
			}
		}
		vpContent = strings.Join(bodyLines, "\n")
	}
	content.WriteString(vpContent)

	// ── status and footer ────────────────────────────────────────────────────
	statusText := fmt.Sprintf("r%d — OBSERVATION #%d (READ-ONLY)", rev.Revision, obs.ID)
	if m.err != nil {
		statusText = "ERROR: " + m.err.Error()
	}
	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusText)
	footerLine := strings.Repeat(" ", cOffset+leftPad) + dimStyle.Render(bbsFooterRevisionContent)

	// ── compose full-height layout ───────────────────────────────────────────
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}
