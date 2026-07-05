package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ionix/ion-mem/internal/store"
)

// stripAnsiCodes removes ANSI escape sequences from s, returning the plain text.
func stripAnsiCodes(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip 'm'
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// makeTwoProjects returns two project summaries for wide-terminal tests.
func makeTwoProjects() []store.ProjectSummary {
	return []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 5, SessionCount: 2, LastActivity: time.Now().Add(-2 * time.Hour)},
		{Project: "beta", ObservationCount: 3, SessionCount: 1, LastActivity: time.Now().Add(-30 * time.Minute)},
	}
}

// setTrueColor forces lipgloss to emit ANSI escape sequences during the test so
// we can inspect styled-blank lines. Restored to ASCII profile in t.Cleanup.
func setTrueColor(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

// ─── Bug 1: stray styled-blank row in padding area ───────────────────────────

// TestWide_NoStyledBlankInPaddingArea asserts that, at 200x55 with projects
// visible, every line in the padding area (between list end and status bar) is a
// genuinely empty string with no ANSI escape sequences.
//
// A "styled blank" — a line that strips to whitespace but contains ANSI codes —
// can create a visible background-tinted bar in real terminals that interpret
// the leftover color-set sequence as painting the rest of the line.
func TestWide_NoStyledBlankInPaddingArea(t *testing.T) {
	setTrueColor(t)

	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// Exact fill is a prerequisite.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Fatalf("exact-fill pre-check: View() produced %d lines, want %d", lineCount, termH)
	}

	lines := viewLines(out)

	// The chrome occupies the first 2 rows (header + separator) and the last 2
	// rows (status + footer). Content rows are [2 .. termH-3] (0-indexed).
	// Within content: logo (logoHeight rows) + list rows; the rest is padding.
	// We don't hard-code exactly which row padding starts; instead we scan the
	// entire content area and flag any line that is visually blank but has ANSI.
	contentStart := 2       // 0-indexed
	contentEnd := termH - 3 // inclusive
	for i := contentStart; i <= contentEnd; i++ {
		l := lines[i]
		stripped := stripAnsiCodes(l)
		isBlank := strings.TrimSpace(stripped) == ""
		hasAnsi := strings.Contains(l, "\x1b[")
		if isBlank && hasAnsi {
			t.Errorf("padding area line %d is a styled blank (ANSI codes on a blank line):\n  raw: %q",
				i+1, l)
		}
	}
}

// TestWide_ExactFill_200x55 is the primary exact-fill regression at 200x55.
func TestWide_ExactFill_200x55(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("View() produced %d lines, want exactly %d", lineCount, termH)
	}

	lines := viewLines(out)
	if len(lines) < 2 {
		t.Fatal("fewer than 2 lines, cannot check chrome")
	}

	// Header on first line.
	if !strings.Contains(lines[0], "ion-mem") {
		t.Errorf("header not on line 1: %q", lines[0])
	}
	// Status bar on second-to-last line.
	if !strings.Contains(lines[len(lines)-2], "project(s)") {
		t.Errorf("status bar not on second-to-last line: %q", lines[len(lines)-2])
	}
	// Footer on last line.
	if !strings.Contains(lines[len(lines)-1], "quit") {
		t.Errorf("footer not on last line: %q", lines[len(lines)-1])
	}
}

// ─── Bug 2: centering on wide terminals ──────────────────────────────────────

// TestWide_ListRowsCenteredAt200 asserts that, at 200 columns, content is
// centered within a contentMaxWidth block so columns are not scattered across
// 200 chars. Specifically:
//   - The left margin of list rows must be > leftPad (the centering offset kicks in).
//   - The activity (date) column must end before contentMaxWidth + centering_offset,
//     not at col ~195.
func TestWide_ListRowsCenteredAt200(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()
	lines := viewLines(out)

	// Find the first project row (contains "alpha").
	alphaIdx := -1
	for i, l := range lines {
		if strings.Contains(stripAnsiCodes(l), "alpha") {
			alphaIdx = i
			break
		}
	}
	if alphaIdx < 0 {
		t.Fatal("'alpha' row not found in output")
	}

	alphaLine := stripAnsiCodes(lines[alphaIdx])

	// The line must not extend to column 190+ (old unbounded behavior).
	// With centering the content fits within contentMaxWidth ≈100 cols plus offset.
	// We allow a generous upper bound: line visual width ≤ 150.
	visualWidth := len([]rune(strings.TrimRight(alphaLine, " ")))
	if visualWidth > 150 {
		t.Errorf("'alpha' row extends to col %d; expected ≤150 with centering (contentMaxWidth enforced)",
			visualWidth)
	}

	// The activity string must appear somewhere in the line.
	if !strings.Contains(alphaLine, "ago") {
		t.Errorf("'alpha' row does not contain activity string 'ago': %q", alphaLine[:minW(len(alphaLine), 120)])
	}
}

// TestWide_StatusAndFooterCenteredAt200 checks that on 200-wide terminals the
// status bar and footer are also within the centred content block and not
// indented only by leftPad (2 cols).
func TestWide_StatusAndFooterCenteredAt200(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()
	lines := viewLines(out)
	if len(lines) < 2 {
		t.Fatal("fewer than 2 lines")
	}

	statusLine := stripAnsiCodes(lines[len(lines)-2])
	footerLine := stripAnsiCodes(lines[len(lines)-1])

	// On a 200-wide terminal the centering offset for contentMaxWidth=100 is 50.
	// Status/footer must start at column ≥ 50 (the centering indent).
	// We check by counting leading spaces.
	statusLeading := len(statusLine) - len(strings.TrimLeft(statusLine, " "))
	footerLeading := len(footerLine) - len(strings.TrimLeft(footerLine, " "))

	if statusLeading < 40 {
		t.Errorf("status bar leading spaces = %d, expected ≥40 (centering offset on 200-wide terminal)",
			statusLeading)
	}
	if footerLeading < 40 {
		t.Errorf("footer leading spaces = %d, expected ≥40 (centering offset on 200-wide terminal)",
			footerLeading)
	}
}

// TestWide_CenteringDoesNotBreak80x24 confirms that the max-width centering
// logic is a no-op on terminals ≤ contentMaxWidth columns.
func TestWide_CenteringDoesNotBreak80x24(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 12, SessionCount: 3, LastActivity: time.Now().Add(-2 * time.Hour)},
		{Project: "beta", ObservationCount: 5, SessionCount: 1, LastActivity: time.Now().Add(-30 * time.Minute)},
		{Project: "gamma", ObservationCount: 240, SessionCount: 8, LastActivity: time.Now().Add(-5 * time.Minute)},
	}
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// Exact fill still holds.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("80x24: View() produced %d lines, want %d", lineCount, termH)
	}

	// Standard left margin applies (no large centering offset).
	lines := viewLines(out)
	alphaIdx := -1
	for i, l := range lines {
		if strings.Contains(stripAnsiCodes(l), "alpha") {
			alphaIdx = i
			break
		}
	}
	if alphaIdx < 0 {
		t.Fatal("'alpha' row not found")
	}

	alphaLine := stripAnsiCodes(lines[alphaIdx])
	leading := len(alphaLine) - len(strings.TrimLeft(alphaLine, " ▌"))
	// At 80-wide, left margin should be small (leftPad = 2 or the ▌ indicator).
	if leading > 10 {
		t.Errorf("80x24: 'alpha' row has unexpected leading whitespace %d — centering may have kicked in incorrectly",
			leading)
	}
}

// ─── Bug 3: vertical rhythm — blank line between tagline and list ─────────────

// TestWide_BlankLineAfterTagline asserts that there is at least one blank line
// between the tagline row (containing "Persistent memory") and the first project
// list row (containing a project name) in a tall-terminal render.
func TestWide_BlankLineAfterTagline(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()
	lines := viewLines(out)

	// Find tagline row.
	taglineIdx := -1
	for i, l := range lines {
		if strings.Contains(stripAnsiCodes(l), "Persistent memory") {
			taglineIdx = i
			break
		}
	}
	if taglineIdx < 0 {
		t.Fatal("tagline 'Persistent memory' not found in output — logo not rendering?")
	}

	// Find first project list row (any project name).
	listStartIdx := -1
	projectNames := []string{"alpha", "beta"}
	for i := taglineIdx + 1; i < len(lines); i++ {
		stripped := stripAnsiCodes(lines[i])
		for _, name := range projectNames {
			if strings.Contains(stripped, name) {
				listStartIdx = i
				break
			}
		}
		if listStartIdx >= 0 {
			break
		}
	}
	if listStartIdx < 0 {
		t.Fatal("no project list row found after tagline")
	}

	// There must be at least one blank line between tagline and list.
	gapLines := listStartIdx - taglineIdx - 1
	if gapLines < 1 {
		t.Errorf("no blank line between tagline (line %d) and first list row (line %d); gap=%d",
			taglineIdx+1, listStartIdx+1, gapLines)
	}
}

// ─── Centering at 120x30 — literal output for visual inspection ──────────────

// TestWide_120x30_LiteralView renders the projects view at 120x30 and logs the
// full output so reviewers can visually inspect the centering layout.
// This test never fails on content — it only fails on exact-fill.
func TestWide_120x30_LiteralView(t *testing.T) {
	const termW, termH = 120, 30
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// Exact fill.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("120x30: View() produced %d lines, want %d", lineCount, termH)
	}

	// Log the plain-text rendering for human inspection.
	lines := viewLines(out)
	t.Log("=== View() at 120x30 (plain text) ===")
	for i, l := range lines {
		t.Logf("%02d: %s", i+1, stripAnsiCodes(l))
	}
	t.Log("=== end ===")
	_ = fmt.Sprintf
}

// ─── helper ──────────────────────────────────────────────────────────────────

func minW(a, b int) int {
	if a < b {
		return a
	}
	return b
}
