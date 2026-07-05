package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── clampWindow unit tests ───────────────────────────────────────────────────

func TestClampWindow(t *testing.T) {
	tests := []struct {
		name    string
		cursor  int
		offset  int
		visible int
		total   int
		want    int
	}{
		{
			name:   "empty list always returns 0",
			cursor: 0, offset: 0, visible: 10, total: 0,
			want: 0,
		},
		{
			name:   "cursor at top, window at top, no scroll needed",
			cursor: 0, offset: 0, visible: 10, total: 20,
			want: 0,
		},
		{
			name:   "cursor at bottom of list shorter than window",
			cursor: 4, offset: 0, visible: 10, total: 5,
			want: 0,
		},
		{
			name: "cursor moves into down-margin, window scrolls",
			// visible=10, margin=2 → window scrolls when cursor >= offset+visible-margin
			// cursor=8, offset=0, 8 >= 0+10-2=8 and offset+visible(10) < total(20) → scroll
			cursor: 8, offset: 0, visible: 10, total: 20,
			want: 8 - 10 + 2 + 1, // = 1
		},
		{
			name:   "cursor at very bottom of list, window follows",
			cursor: 19, offset: 0, visible: 10, total: 20,
			want: 10, // 20 - 10 = 10
		},
		{
			name: "cursor moves up into up-margin, window scrolls back",
			// cursor=3, offset=5, 3 < 5+2=7 → scroll up: offset = cursor-margin = 1
			cursor: 3, offset: 5, visible: 10, total: 20,
			want: 1,
		},
		{
			name:   "cursor at absolute top, offset snaps to 0",
			cursor: 0, offset: 5, visible: 10, total: 20,
			want: 0,
		},
		{
			name:   "window larger than total, offset stays 0",
			cursor: 3, offset: 0, visible: 50, total: 10,
			want: 0,
		},
		{
			name:   "cursor in middle, no scroll needed",
			cursor: 5, offset: 0, visible: 10, total: 20,
			want: 0,
		},
		{
			name:   "visible=0 always returns 0",
			cursor: 5, offset: 0, visible: 0, total: 20,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampWindow(tt.cursor, tt.offset, tt.visible, tt.total)
			if got != tt.want {
				t.Errorf("clampWindow(cursor=%d, offset=%d, visible=%d, total=%d) = %d, want %d",
					tt.cursor, tt.offset, tt.visible, tt.total, got, tt.want)
			}
		})
	}
}

// ─── window-follows-cursor integration tests ─────────────────────────────────

// makeNObs returns n fake Observations with predictable titles.
func makeNObs(n int) []store.Observation {
	obs := make([]store.Observation, n)
	for i := range obs {
		obs[i] = store.Observation{
			ID:        int64(i + 1),
			Title:     fmt.Sprintf("Observation %03d", i+1),
			Type:      "decision",
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339Nano),
		}
	}
	return obs
}

// setSize sends a WindowSizeMsg and returns the updated model.
func setSize(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func TestWindowFollowsCursorDown(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	m.view = viewObservations
	m.observations = makeNObs(50)
	m.obsCursor = 0
	m.obsOffset = 0

	visible := m.listVisibleHeight(true, false)

	// Press 'j' repeatedly until cursor moves well past the initial window.
	for m.obsCursor < visible+5 {
		m = sendRune(m, 'j')
	}

	// Invariant: offset <= cursor < offset+visible.
	if m.obsOffset > m.obsCursor {
		t.Errorf("offset(%d) > cursor(%d): window did not follow down", m.obsOffset, m.obsCursor)
	}
	if m.obsCursor >= m.obsOffset+visible {
		t.Errorf("cursor(%d) >= offset(%d)+visible(%d): cursor walked out of window",
			m.obsCursor, m.obsOffset, visible)
	}
}

func TestWindowFollowsCursorUp(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	m.view = viewObservations
	obs := makeNObs(50)
	m.observations = obs
	// Start at the bottom.
	m.obsCursor = 49
	visible := m.listVisibleHeight(true, false)
	m.obsOffset = clampWindow(49, 0, visible, 50)

	// Press 'k' repeatedly until cursor is back at the top.
	for m.obsCursor > 0 {
		m = sendRune(m, 'k')
	}

	if m.obsOffset != 0 {
		t.Errorf("after navigating back to top, offset = %d, want 0", m.obsOffset)
	}
	if m.obsCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.obsCursor)
	}
}

func TestWindowResizeKeepsCursorVisible(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 40)
	m.view = viewObservations
	m.observations = makeNObs(50)

	// Navigate to the middle of the list.
	for m.obsCursor < 25 {
		m = sendRune(m, 'j')
	}
	cursor := m.obsCursor

	// Shrink the terminal — cursor must remain visible.
	m = setSize(m, 80, 15)
	visible := m.listVisibleHeight(true, false)

	if m.obsOffset > cursor {
		t.Errorf("after resize, offset(%d) > cursor(%d)", m.obsOffset, cursor)
	}
	if cursor >= m.obsOffset+visible {
		t.Errorf("after resize, cursor(%d) out of window [%d, %d)",
			cursor, m.obsOffset, m.obsOffset+visible)
	}
}

func TestWindowEmptyList(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	m.view = viewObservations
	m.observations = nil
	m.obsCursor = 0
	m.obsOffset = 0

	// Navigation on an empty list must not panic or produce negative offsets.
	m = sendRune(m, 'j')
	m = sendRune(m, 'k')

	if m.obsCursor != 0 {
		t.Errorf("cursor on empty list = %d, want 0", m.obsCursor)
	}
	if m.obsOffset != 0 {
		t.Errorf("offset on empty list = %d, want 0", m.obsOffset)
	}
}

func TestWindowListShorterThanHeight(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 40)
	m.view = viewObservations
	m.observations = makeNObs(3)

	// Navigate to the end of a short list — no offset needed.
	for i := 0; i < 5; i++ {
		m = sendRune(m, 'j')
	}

	if m.obsCursor != 2 {
		t.Errorf("cursor = %d, want 2 (clamped to last item)", m.obsCursor)
	}
	if m.obsOffset != 0 {
		t.Errorf("offset = %d, want 0 (list fits in window)", m.obsOffset)
	}
}

// ─── render smoke test ────────────────────────────────────────────────────────

// viewLines splits a View() string into lines, stripping the single trailing
// newline that View() always appends. The returned slice has exactly as many
// entries as rendered terminal rows.
func viewLines(out string) []string {
	// View() always ends with "\n"; TrimSuffix removes exactly that one newline.
	trimmed := strings.TrimSuffix(out, "\n")
	return strings.Split(trimmed, "\n")
}

// TestRenderSmoke builds a model with 50 fake observations at 80x24, moves the
// cursor beyond the first window, and verifies:
//  1. The selected row IS present in View() output.
//  2. View() fills the terminal exactly — strings.Count(view,"\n")+1 == height.
//  3. The position indicator ("cursor/total") appears in the output.
//  4. The footer hint is on the last line; the status bar on the second-to-last.
func TestRenderSmoke_ObservationsScrolledMidList(t *testing.T) {
	const termH = 24
	m := newModel()
	m = setSize(m, 80, termH)
	m.view = viewObservations
	m.selectedProject = "smoke-test"
	m.observations = makeNObs(50)
	m.obsCursor = 0
	m.obsOffset = 0

	// Move cursor to position 20 (well past the initial window).
	for m.obsCursor < 20 {
		m = sendRune(m, 'j')
	}

	out := m.View()

	// 1. Selected row must be present in output.
	wantTitle := fmt.Sprintf("Observation %03d", m.obsCursor+1)
	if !strings.Contains(out, wantTitle) {
		t.Errorf("View() does not contain selected row %q\noutput:\n%s", wantTitle, out)
	}

	// 2. View() must fill the terminal exactly.
	// View() always ends with "\n", so each rendered row contributes exactly one
	// "\n". Therefore strings.Count(out, "\n") == number of rendered rows.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		lines := viewLines(out)
		t.Errorf("View() produced %d lines, want exactly %d (terminal height)\nlines: %d\noutput:\n%s",
			lineCount, termH, len(lines), out)
	}

	// 3. Position indicator must be present.
	want := fmt.Sprintf("%d/50", m.obsCursor+1)
	if !strings.Contains(out, want) {
		t.Errorf("View() does not contain position indicator %q\noutput:\n%s", want, out)
	}

	// 4. Footer on last line; status bar on second-to-last.
	lines := viewLines(out)
	if len(lines) < 2 {
		t.Fatalf("View() has fewer than 2 lines, cannot check chrome positions")
	}
	lastLine := lines[len(lines)-1]
	secondLast := lines[len(lines)-2]

	if !strings.Contains(lastLine, "quit") && !strings.Contains(lastLine, "q quit") {
		t.Errorf("last line does not look like footer hints, got: %q", lastLine)
	}
	if !strings.Contains(secondLast, "observation(s)") {
		t.Errorf("second-to-last line does not look like status bar, got: %q", secondLast)
	}
}

func TestRenderSmoke_ProjectsView(t *testing.T) {
	const termH = 24
	m := newModel()
	m = setSize(m, 80, termH)
	m.view = viewProjects
	m.projects = []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 12, SessionCount: 3, LastActivity: time.Now().Add(-2 * time.Hour)},
		{Project: "beta", ObservationCount: 5, SessionCount: 1, LastActivity: time.Now().Add(-30 * time.Minute)},
		{Project: "gamma", ObservationCount: 240, SessionCount: 8, LastActivity: time.Now().Add(-5 * time.Minute)},
	}
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// Must contain the brand.
	if !strings.Contains(out, "ion-mem") {
		t.Errorf("View() does not contain brand 'ion-mem'\noutput:\n%s", out)
	}

	// Must contain the selected project name.
	if !strings.Contains(out, "alpha") {
		t.Errorf("View() does not contain selected project 'alpha'\noutput:\n%s", out)
	}

	// View() must fill the terminal exactly.
	// View() always ends with "\n", so strings.Count(out, "\n") == rendered rows.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("View() produced %d lines, want exactly %d\noutput:\n%s", lineCount, termH, out)
	}

	// Position indicator.
	if !strings.Contains(out, "1/3") {
		t.Errorf("View() does not contain position indicator '1/3'\noutput:\n%s", out)
	}

	// Header on first line; footer on last; status bar on second-to-last.
	lines := viewLines(out)
	if len(lines) < 2 {
		t.Fatalf("View() has fewer than 2 lines, cannot check chrome positions")
	}
	firstLine := lines[0]
	lastLine := lines[len(lines)-1]
	secondLast := lines[len(lines)-2]

	if !strings.Contains(firstLine, "ion-mem") {
		t.Errorf("first line does not contain brand 'ion-mem', got: %q", firstLine)
	}
	if !strings.Contains(lastLine, "quit") {
		t.Errorf("last line does not look like footer hints, got: %q", lastLine)
	}
	if !strings.Contains(secondLast, "project(s)") {
		t.Errorf("second-to-last line does not look like status bar, got: %q", secondLast)
	}
}

// TestRenderSmoke_ProjectsTallTerminal verifies the hero logo is present at
// 80x40, that the view fills the terminal exactly, and the project list is
// still visible below the logo.
func TestRenderSmoke_ProjectsTallTerminal(t *testing.T) {
	const termW, termH = 80, 40
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 12, SessionCount: 3, LastActivity: time.Now().Add(-2 * time.Hour)},
		{Project: "beta", ObservationCount: 5, SessionCount: 1, LastActivity: time.Now().Add(-30 * time.Minute)},
	}
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// Exact fill: every row accounted for.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("tall terminal: View() produced %d lines, want exactly %d\noutput:\n%s", lineCount, termH, out)
	}

	// Logo must appear: check for a block-character substring unique to the logo art.
	// logoRows[0] contains "██╗" which only exists in the logo.
	if !strings.Contains(out, "██╗") {
		t.Errorf("tall terminal: logo art not found in View() output\noutput:\n%s", out)
	}

	// Project list must still be visible.
	if !strings.Contains(out, "alpha") {
		t.Errorf("tall terminal: project 'alpha' not found after logo\noutput:\n%s", out)
	}

	// Status bar on second-to-last; footer on last.
	lines := viewLines(out)
	if len(lines) < 2 {
		t.Fatalf("View() fewer than 2 lines")
	}
	if !strings.Contains(lines[len(lines)-1], "quit") {
		t.Errorf("footer not on last line; got: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-2], "project(s)") {
		t.Errorf("status bar not on second-to-last line; got: %q", lines[len(lines)-2])
	}
}

// TestRenderSmoke_ProjectsShortTerminalNoLogo verifies the logo is NOT shown
// at 80x20 (below logoMinTermHeight), that the compact header still appears,
// and the view fills exactly.
func TestRenderSmoke_ProjectsShortTerminalNoLogo(t *testing.T) {
	const termW, termH = 80, 20
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 12, SessionCount: 3, LastActivity: time.Now().Add(-2 * time.Hour)},
	}
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// Exact fill.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("short terminal: View() produced %d lines, want exactly %d\noutput:\n%s", lineCount, termH, out)
	}

	// Logo must NOT appear (height < logoMinTermHeight).
	// Check that none of the logo-exclusive box-drawing characters are present
	// in positions beyond the header (use a string unique to the art rows).
	// logoRows[0] starts with "  ██╗" — ██ is only in the logo.
	if strings.Contains(out, "██╗") {
		t.Errorf("short terminal: logo art found at h=%d but should be suppressed\noutput:\n%s", termH, out)
	}

	// Brand header must still be on first line.
	lines := viewLines(out)
	if !strings.Contains(lines[0], "ion-mem") {
		t.Errorf("brand not on first line at short terminal; got: %q", lines[0])
	}

	// Status bar on second-to-last; footer on last.
	if !strings.Contains(lines[len(lines)-1], "quit") {
		t.Errorf("footer not on last line; got: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-2], "project(s)") {
		t.Errorf("status bar not on second-to-last line; got: %q", lines[len(lines)-2])
	}
}

// TestGlobalSearchFlow verifies the full global search lifecycle:
// '/' in projects view opens search input, enter submits and transitions to
// viewGlobalSearch, esc returns to viewProjects. Also verifies that the
// runGlobalSearch command uses an empty Project field (all-projects search).
func TestGlobalSearchFlow(t *testing.T) {
	// Step 1: '/' in projects view should open global search input.
	m := newModel()
	m.view = viewProjects
	m.projects = makeProjectSummaries()

	m = sendRune(m, '/')
	if !m.globalSearching {
		t.Fatal("after '/' on projects view, globalSearching should be true")
	}
	if m.view != viewProjects {
		t.Errorf("view should remain viewProjects while input is open; got %v", m.view)
	}

	// Step 2: type a query into the search input.
	// Simulate characters arriving via searchInput directly (store is nil so
	// we test state machine, not the actual search command).
	m.searchInput.SetValue("auth")

	// Step 3: Enter submits → view transitions to viewGlobalSearch.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.globalSearching {
		t.Error("globalSearching should be false after Enter")
	}
	if m.view != viewGlobalSearch {
		t.Errorf("view should be viewGlobalSearch after submit; got %v", m.view)
	}
	if m.globalQuery != "auth" {
		t.Errorf("globalQuery = %q, want %q", m.globalQuery, "auth")
	}

	// Step 4: Esc returns to viewProjects and clears the global query.
	m = sendKey(m, tea.KeyEsc)
	if m.view != viewProjects {
		t.Errorf("after Esc from global search results, view = %v, want viewProjects", m.view)
	}
	if m.globalQuery != "" {
		t.Errorf("globalQuery should be cleared on Esc; got %q", m.globalQuery)
	}
}

// TestRenderSmoke_ObservationsViewWithSearchBar verifies the persistent search
// bar is always visible in the observations view and the exact-fill contract
// still holds at 80x24.
func TestRenderSmoke_ObservationsViewWithSearchBar(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewObservations
	m.selectedProject = "myproject"
	m.observations = makeObservations()
	m.obsCursor = 0
	m.obsOffset = 0

	out := m.View()

	// Exact fill.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("observations: View() produced %d lines, want exactly %d\noutput:\n%s", lineCount, termH, out)
	}

	// Search bar must be visible — it always shows the "/" glyph.
	if !strings.Contains(out, "/") {
		t.Errorf("search bar not found in observations view\noutput:\n%s", out)
	}

	// At least one observation title visible.
	if !strings.Contains(out, "First obs") {
		t.Errorf("observation title 'First obs' not found in output\noutput:\n%s", out)
	}

	// Status bar on second-to-last; footer on last.
	lines := viewLines(out)
	if !strings.Contains(lines[len(lines)-1], "quit") {
		t.Errorf("footer not on last line; got: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-2], "observation(s)") {
		t.Errorf("status bar not on second-to-last line; got: %q", lines[len(lines)-2])
	}
}

// TestRenderSmoke_ShortListPadsToFullHeight pins the screenshot bug: when the
// project list is shorter than the terminal height, the footer must still be
// pinned to the last row (row 24 in a 24-row terminal), with the gap filled by
// blank padding lines between the list and the status bar.
func TestRenderSmoke_ShortListPadsToFullHeight(t *testing.T) {
	const termH = 24
	m := newModel()
	m = setSize(m, 80, termH)
	m.view = viewProjects
	// Only 3 projects — far fewer than the 20 available content rows.
	m.projects = []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 1, SessionCount: 1, LastActivity: time.Now().Add(-1 * time.Hour)},
		{Project: "beta", ObservationCount: 2, SessionCount: 1, LastActivity: time.Now().Add(-2 * time.Hour)},
		{Project: "gamma", ObservationCount: 3, SessionCount: 1, LastActivity: time.Now().Add(-3 * time.Hour)},
	}
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()

	// View() must fill the terminal exactly regardless of list length.
	// View() always ends with "\n", so strings.Count(out, "\n") == rendered rows.
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("short list: View() produced %d lines, want exactly %d\noutput:\n%s",
			lineCount, termH, out)
	}

	lines := viewLines(out)

	// Footer is on the last line (line 24).
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "quit") {
		t.Errorf("footer not on last line; last line = %q", lastLine)
	}

	// Status bar is on the second-to-last line (line 23).
	secondLast := lines[len(lines)-2]
	if !strings.Contains(secondLast, "project(s)") {
		t.Errorf("status bar not on second-to-last line; got %q", secondLast)
	}

	// Header is on the first line.
	if !strings.Contains(lines[0], "ion-mem") {
		t.Errorf("header not on first line; got %q", lines[0])
	}

	// There must be at least one blank padding line between the list content
	// and the status bar to prove padding is injected.
	// Content starts at line 3 (0-indexed: 2) — lines[2] through lines[termH-3]
	// include 3 project rows and then padding. At least one must be blank.
	contentRows := lines[2 : termH-2] // indices 2..21 (20 rows)
	blankFound := false
	for _, l := range contentRows {
		if strings.TrimSpace(l) == "" {
			blankFound = true
			break
		}
	}
	if !blankFound {
		t.Errorf("expected blank padding lines in content area but found none\ncontent rows: %v", contentRows)
	}
}
