package tui

// wordwrap_test.go — TDD tests for wrapForViewport.
// Written BEFORE the production helper — these tests drive the RED→GREEN cycle.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── unit tests for wrapForViewport ──────────────────────────────────────────

// TestWrapForViewport_LongLineWraps asserts that a line longer than width is
// split into multiple lines, each at most width columns wide.
func TestWrapForViewport_LongLineWraps(t *testing.T) {
	// Build a long line with distinct words so word-boundary wrapping is visible.
	words := strings.Repeat("hello world ", 30) // 360 chars
	got := wrapForViewport(words, 80)
	for i, line := range strings.Split(got, "\n") {
		if len(line) > 80 {
			t.Errorf("line %d exceeds width 80 (len=%d): %q", i, len(line), line)
		}
	}
	// At least one wrap must have occurred (output has > 1 line).
	if !strings.Contains(got, "\n") {
		t.Error("wrapForViewport: no wrapping occurred on a 360-char line with width=80")
	}
}

// TestWrapForViewport_OverlongTokenHardWraps asserts that a single word longer
// than width is hard-broken (no line exceeds width).
func TestWrapForViewport_OverlongTokenHardWraps(t *testing.T) {
	token := strings.Repeat("x", 200)
	got := wrapForViewport(token, 80)
	for i, line := range strings.Split(got, "\n") {
		if len(line) > 80 {
			t.Errorf("hard-wrap: line %d exceeds width 80 (len=%d)", i, len(line))
		}
	}
	// The content must be completely preserved (no bytes lost).
	content := strings.ReplaceAll(got, "\n", "")
	if content != token {
		t.Errorf("hard-wrap: content changed — got len=%d, want len=%d", len(content), len(token))
	}
}

// TestWrapForViewport_ShortLinesUntouched asserts that lines already within
// the width limit pass through unchanged.
func TestWrapForViewport_ShortLinesUntouched(t *testing.T) {
	input := "short line\nanother short line"
	got := wrapForViewport(input, 80)
	if got != input {
		t.Errorf("wrapForViewport changed short lines:\ngot:  %q\nwant: %q", got, input)
	}
}

// TestWrapForViewport_EmptyContentSafe asserts that empty input returns empty
// output without panic.
func TestWrapForViewport_EmptyContentSafe(t *testing.T) {
	got := wrapForViewport("", 80)
	if got != "" {
		t.Errorf("wrapForViewport(\"\", 80) = %q, want %q", got, "")
	}
}

// TestWrapForViewport_ZeroWidthFallback asserts that width ≤ 0 is safe (no
// infinite loop or panic). The function may use a minimum internal width.
func TestWrapForViewport_ZeroWidthFallback(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("wrapForViewport panicked with width=0: %v", r)
		}
	}()
	_ = wrapForViewport("hello world", 0)
}

// ─── detail view render test at 80x24 with a 300-char line ───────────────────

// makeObsWithLongContent returns an observation whose content has one very long line.
func makeObsWithLongContent() store.Observation {
	// Build a 300-char content line with many recognisable words.
	var sb strings.Builder
	for sb.Len() < 300 {
		sb.WriteString("persistent memory observation content ")
	}
	longLine := sb.String()
	// Append a sentinel word that appears only at the end of the long line.
	longLine += " SENTINELWORD"

	return store.Observation{
		ID:        99,
		Project:   "testproject",
		Title:     "Long content observation",
		Type:      "decision",
		Scope:     "project",
		Content:   longLine,
		CreatedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
		UpdatedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
	}
}

// TestDetail_NoLineExceedsTerminalWidth asserts that at 80x24 with an
// observation whose content has a 300-char line, no rendered line (after
// stripping ANSI) exceeds 80 columns.
func TestDetail_NoLineExceedsTerminalWidth(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsWithLongContent()
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewDetail
	m.vp.Width = effectiveWidth(termW)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(wrapForViewport(renderObservationDetail(obs), m.vp.Width))
	m.vp.GotoTop()

	out := m.View()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		plain := stripAnsiCodes(l)
		if len([]rune(plain)) > termW {
			t.Errorf("line %d exceeds terminal width %d (width=%d): %q",
				i+1, termW, len([]rune(plain)), plain)
		}
	}
}

// TestDetail_SentinelWordVisibleAfterWrap asserts that content from the END of
// the long line is visible in the rendered viewport (proves wrap not clip).
func TestDetail_SentinelWordVisibleAfterWrap(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsWithLongContent()
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewDetail
	m.vp.Width = effectiveWidth(termW)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(wrapForViewport(renderObservationDetail(obs), m.vp.Width))
	m.vp.GotoTop()

	// After setting content, GotoTop shows the first page. Scroll to end to
	// expose SENTINELWORD (which is at the end of the long line after wrapping).
	m.vp.GotoBottom()

	vpContent := m.vp.View()
	// The viewport content (before adding cOffset) must contain the sentinel.
	if !strings.Contains(vpContent, "SENTINELWORD") {
		t.Errorf("SENTINELWORD not visible in viewport content — content was clipped not wrapped.\nvp content:\n%s", vpContent)
	}
}

// ─── resize test: re-wrap on WindowSizeMsg ────────────────────────────────────

// TestDetail_ResizeRewrapsContent asserts that after a resize from 120 to 60
// columns, the viewport body content has no lines wider than 60. Chrome
// elements (header, footer, status bar) are fixed-width strings and are not
// subject to this assertion.
func TestDetail_ResizeRewrapsContent(t *testing.T) {
	obs := makeObsWithLongContent()

	// Initial render at 120 wide.
	m := newModel()
	m = setSize(m, 120, 30)
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewDetail
	m.vp.Width = effectiveWidth(120)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(wrapForViewport(renderObservationDetail(obs), m.vp.Width))
	m.vp.GotoTop()

	// Resize to 60 wide — the model should re-wrap the viewport content.
	m = setSize(m, 60, 30)

	// Inspect the viewport body content directly (not the full View() which
	// includes fixed-width chrome like the footer).
	vpContent := m.vp.View()
	lines := strings.Split(vpContent, "\n")
	for i, l := range lines {
		plain := stripAnsiCodes(l)
		if len([]rune(plain)) > 60 {
			t.Errorf("after resize to 60: vp line %d exceeds width 60 (width=%d): %q",
				i+1, len([]rune(plain)), plain)
		}
	}
}

// TestDetail_ExactFill_80x24_WithLongContent asserts exact-fill is maintained
// when a long-content observation is shown in the detail view.
func TestDetail_ExactFill_80x24_WithLongContent(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsWithLongContent()
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewDetail
	m.vp.Width = effectiveWidth(termW)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(wrapForViewport(renderObservationDetail(obs), m.vp.Width))
	m.vp.GotoTop()

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("exact-fill: View() produced %d lines, want %d", lineCount, termH)
	}
}

// ─── revision content view wrap test ─────────────────────────────────────────

// TestRevisionContent_NoLineExceedsTerminalWidth asserts that the revision
// content view also word-wraps long content.
func TestRevisionContent_NoLineExceedsTerminalWidth(t *testing.T) {
	const termW, termH = 80, 24

	// Build a revision with a 300-char content line.
	var sb strings.Builder
	for sb.Len() < 300 {
		sb.WriteString("revision content word ")
	}
	longContent := sb.String() + " REVSENTINEL"

	obs := makeObsWithRevisions(3)
	rev := store.Revision{
		ID:            1,
		ObservationID: obs.ID,
		Revision:      1,
		Type:          "decision",
		Title:         "Old revision title",
		Content:       longContent,
		ArchivedAt:    time.Now().Add(-24 * time.Hour).Format(time.RFC3339Nano),
	}

	m := newModel()
	m = setSize(m, termW, termH)
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.selectedRevision = &rev
	m.view = viewRevisionContent
	m.revVP.Width = effectiveWidth(termW)
	m.revVP.Height = m.revVPHeight()
	m.revVP.SetContent(wrapForViewport(rev.Content, m.revVP.Width))
	m.revVP.GotoTop()

	out := m.View()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		plain := stripAnsiCodes(l)
		if len([]rune(plain)) > termW {
			t.Errorf("revision content: line %d exceeds terminal width %d (width=%d): %q",
				i+1, termW, len([]rune(plain)), plain)
		}
	}
}

// ─── integration: setSize triggers re-wrap via Update ────────────────────────

// TestDetail_SetSizeTriggersRewrap verifies that the model's Update handler
// for tea.WindowSizeMsg re-wraps the viewport body content so that no body
// line exceeds the new viewport width. Chrome elements (header, footer, status
// bar) are fixed-width strings and are not subject to this assertion.
func TestDetail_SetSizeTriggersRewrap(t *testing.T) {
	obs := makeObsWithLongContent()

	m := newModel()
	// Open detail view at 120 wide so initial content is wrapped at 120.
	m = setSize(m, 120, 30)
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewDetail
	m.vp.Width = effectiveWidth(120)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(wrapForViewport(renderObservationDetail(obs), m.vp.Width))
	m.vp.GotoTop()

	// Simulate a resize to 60 via the Update path (what the real runtime does).
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = next.(Model)

	// Check the viewport body content directly.
	vpContent := m.vp.View()
	lines := strings.Split(vpContent, "\n")
	for i, l := range lines {
		plain := stripAnsiCodes(l)
		if len([]rune(plain)) > 60 {
			t.Errorf("post-resize vp line %d exceeds 60 cols (width=%d): %q",
				i+1, len([]rune(plain)), plain)
		}
	}
}
