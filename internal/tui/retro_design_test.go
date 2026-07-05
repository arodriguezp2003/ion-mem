package tui

// retro_design_test.go — Tests for the retro BBS / CRT design system.
//
// TDD cycle order:
//   1. TestSearchBarRendersOnce — Bug fix: search bar border must appear exactly once at 200x55.
//   2. TestBadgeFixedWidth       — Each badge type produces a 7-char visible string [XXXXX].
//   3. TestDetailBodyCenteredMargin — Detail viewport body lines are indented by cOffset on wide terminals.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── Helpers (local) ─────────────────────────────────────────────────────────

func makeObsForRetroTest() []store.Observation {
	types := []string{"decision", "architecture", "bugfix", "discovery", "config", "preference"}
	obs := make([]store.Observation, 6)
	for i := range obs {
		obs[i] = store.Observation{
			ID:        int64(i + 1),
			Project:   "testproject",
			Title:     fmt.Sprintf("Retro observation title %d — a bit longer so columns exercise", i+1),
			Type:      types[i%len(types)],
			Scope:     "project",
			Content:   "Some content for the detail view body.",
			CreatedAt: time.Now().Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339Nano),
			UpdatedAt: time.Now().Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339Nano),
		}
	}
	return obs
}

// countOccurrences counts the number of non-overlapping occurrences of substr
// in s (after stripping ANSI codes for predictable matching).
func countOccurrences(s, substr string) int {
	plain := stripAnsiCodes(s)
	count := 0
	idx := 0
	for {
		pos := strings.Index(plain[idx:], substr)
		if pos < 0 {
			break
		}
		count++
		idx += pos + len(substr)
	}
	return count
}

// ─── Bug: search bar rendered twice at 200 cols ───────────────────────────────

// TestSearchBarRendersOnce asserts that the search bar border top character
// sequence (╔) appears EXACTLY ONCE in the observations view at 200x55.
// The bug: double-applying centering offset caused two border boxes to render.
func TestSearchBarRendersOnce(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewObservations
	m.selectedProject = "testproject"
	m.observations = makeObsForRetroTest()
	m.obsCursor = 0
	m.obsOffset = 0

	out := m.View()

	// The double-border top-left corner ╔ must appear exactly once.
	n := countOccurrences(out, "╔")
	if n != 1 {
		lines := viewLines(out)
		t.Errorf("search bar top border (╔) appears %d times, want exactly 1 — double-rendering bug", n)
		for i, l := range lines {
			t.Logf("%02d: %s", i+1, stripAnsiCodes(l))
		}
	}
}

// TestSearchBarStartsAtCenteredMargin asserts that the search bar (the line
// containing ╔) starts at the centering margin — not at column 0 and not at
// double the centering margin.
func TestSearchBarStartsAtCenteredMargin(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewObservations
	m.selectedProject = "testproject"
	m.observations = makeObsForRetroTest()
	m.obsCursor = 0
	m.obsOffset = 0

	out := m.View()
	lines := viewLines(out)

	// Find the search bar line (contains ╔).
	searchBarIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "╔") {
			searchBarIdx = i
			break
		}
	}
	if searchBarIdx < 0 {
		t.Fatal("search bar top border (╔) not found in output — double-border style not applied?")
	}

	plain := stripAnsiCodes(lines[searchBarIdx])
	leading := len(plain) - len(strings.TrimLeft(plain, " "))

	// At 200 cols with contentMaxWidth=100, cOffset = (200-100)/2 = 50.
	// The border starts at cOffset, so leading spaces ≈ 50 (allow ±5 tolerance).
	expectedOffset := (termW - contentMaxWidth) / 2
	if leading < expectedOffset-5 || leading > expectedOffset+5 {
		t.Errorf("search bar starts at column %d (leading spaces), want ~%d (centering offset=%d)",
			leading, expectedOffset, expectedOffset)
	}
}

// TestHeaderBarPresentObservationsView asserts the header bar renders on the
// observations view (first line contains the brand / ION).
func TestHeaderBarPresentObservationsView(t *testing.T) {
	for _, termW := range []int{80, 120, 200} {
		termW := termW
		t.Run(fmt.Sprintf("width_%d", termW), func(t *testing.T) {
			m := newModel()
			m = setSize(m, termW, 30)
			m.view = viewObservations
			m.selectedProject = "proj"
			m.observations = makeObsForRetroTest()

			out := m.View()
			lines := viewLines(out)
			if len(lines) == 0 {
				t.Fatal("no lines in View()")
			}
			header := stripAnsiCodes(lines[0])
			if !strings.Contains(header, "ION") && !strings.Contains(header, "ion") {
				t.Errorf("width %d: header line does not contain brand 'ION' or 'ion': %q", termW, header)
			}
		})
	}
}

// ─── Badge fixed-width contract ───────────────────────────────────────────────

// TestBadgeFixedWidth asserts that every known observation type produces a
// badge whose plain-text visual width is exactly badgeVisibleWidth characters.
// The retro design uses [XXXXX] format: "[" + 5 chars + "]" = 7 chars total.
func TestBadgeFixedWidth(t *testing.T) {
	cases := []struct {
		typeName string
	}{
		{"architecture"},
		{"decision"},
		{"bugfix"},
		{"discovery"},
		{"config"},
		{"preference"},
		{"pattern"},
		{"session_summary"},
		{"manual"},
		{"unknown_type_xyz"},
	}

	for _, tc := range cases {
		t.Run(tc.typeName, func(t *testing.T) {
			badge := renderBadge(tc.typeName)
			plain := stripAnsiCodes(badge)
			// Count visible rune width.
			runeLen := len([]rune(plain))
			if runeLen != badgeVisibleWidth {
				t.Errorf("renderBadge(%q): visible width = %d, want %d (got %q)",
					tc.typeName, runeLen, badgeVisibleWidth, plain)
			}
			// Must start with "[" and end with "]".
			if !strings.HasPrefix(plain, "[") || !strings.HasSuffix(plain, "]") {
				t.Errorf("renderBadge(%q): must start with '[' and end with ']', got %q",
					tc.typeName, plain)
			}
		})
	}
}

// ─── Detail view: body aligned with cOffset on wide terminals ─────────────────

// TestDetailBodyCenteredMargin asserts that the viewport body in viewDetail
// is indented to the centering offset (not zero) on a 200-wide terminal.
// Bug: viewport was set without the body lines having the cOffset prepended.
func TestDetailBodyCenteredMargin(t *testing.T) {
	const termW, termH = 200, 55
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewDetail

	obs := makeObsForRetroTest()[0]
	obs.Content = "This is the body text that should be indented to match the meta block."
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	// Recompute viewport to pick up the wide terminal dimensions.
	m.vp.Width = effectiveWidth(termW)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))
	m.vp.GotoTop()

	out := m.View()
	lines := viewLines(out)

	// Find the horizontal rule (═ or ─ separating meta from body).
	// Skip first 2 chrome lines.
	ruleIdx := -1
	for i := 2; i < len(lines); i++ {
		plain := stripAnsiCodes(lines[i])
		inner := strings.Trim(plain, " ")
		if len(inner) > 0 && (strings.TrimRight(inner, "═") == "" || strings.TrimRight(inner, "─") == "") {
			ruleIdx = i
			break
		}
	}
	if ruleIdx < 0 {
		t.Fatal("horizontal rule not found in viewDetail output (after header chrome)")
	}

	// The body starts at ruleIdx+1. Find first non-empty body line.
	bodyIdx := -1
	for i := ruleIdx + 1; i < len(lines); i++ {
		plain := strings.TrimSpace(stripAnsiCodes(lines[i]))
		if plain != "" {
			bodyIdx = i
			break
		}
	}
	if bodyIdx < 0 {
		// No body content visible — acceptable if viewport is empty, skip test.
		t.Skip("no body content visible in viewport — viewport height may be 0")
	}

	bodyLine := stripAnsiCodes(lines[bodyIdx])
	leading := len(bodyLine) - len(strings.TrimLeft(bodyLine, " "))

	// At 200 cols, cOffset = (200-100)/2 = 50. Body lines must be indented ≥ 40.
	if leading < 40 {
		t.Errorf("detail body line leading indent = %d, want ≥40 (centering offset on 200-wide terminal); line: %q",
			leading, bodyLine)
	}
}

// ─── Retro chrome invariants (status bar, footer, separator) ─────────────────

// TestRetroStatusBarInverseVideo checks that the status bar line is non-empty
// and contains context information on all views at 120x30.
func TestRetroStatusBarContent(t *testing.T) {
	const termW, termH = 120, 30

	t.Run("projects_view", func(t *testing.T) {
		m := newModel()
		m = setSize(m, termW, termH)
		m.view = viewProjects
		m.projects = makeProjectSummaries()

		lines := viewLines(m.View())
		status := stripAnsiCodes(lines[len(lines)-2])
		// Retro style: uppercase PROJECT(S).
		if !strings.Contains(strings.ToUpper(status), "PROJECT") {
			t.Errorf("projects status bar missing PROJECT: %q", status)
		}
	})

	t.Run("observations_view", func(t *testing.T) {
		m := newModel()
		m = setSize(m, termW, termH)
		m.view = viewObservations
		m.selectedProject = "testproject"
		m.observations = makeObsForRetroTest()

		lines := viewLines(m.View())
		status := stripAnsiCodes(lines[len(lines)-2])
		// Retro style: uppercase OBSERVATION(S).
		if !strings.Contains(strings.ToUpper(status), "OBSERVATION") {
			t.Errorf("observations status bar missing OBSERVATION: %q", status)
		}
	})

	t.Run("detail_view", func(t *testing.T) {
		m := newModel()
		m = setSize(m, termW, termH)
		obs := makeObsForRetroTest()[0]
		m.view = viewDetail
		m.selectedObs = &obs
		m.selectedProject = "testproject"
		m.vp.Width = effectiveWidth(termW)
		m.vp.Height = m.detailVPHeight()
		m.vp.SetContent(renderObservationDetail(obs))

		lines := viewLines(m.View())
		status := stripAnsiCodes(lines[len(lines)-2])
		// Retro style: uppercase OBSERVATION.
		if !strings.Contains(strings.ToUpper(status), "OBSERVATION") {
			t.Errorf("detail status bar missing OBSERVATION: %q", status)
		}
	})
}

// TestRetroSeparatorIsDoubleRule checks that the separator below the header
// uses ═ (double horizontal box-drawing) as part of the retro design.
func TestRetroSeparatorIsDoubleRule(t *testing.T) {
	for _, view := range []struct {
		name    string
		setupFn func() Model
	}{
		{"projects", func() Model {
			m := newModel()
			m = setSize(m, 120, 30)
			m.view = viewProjects
			m.projects = makeProjectSummaries()
			return m
		}},
		{"observations", func() Model {
			m := newModel()
			m = setSize(m, 120, 30)
			m.view = viewObservations
			m.selectedProject = "proj"
			m.observations = makeObsForRetroTest()
			return m
		}},
	} {
		view := view
		t.Run(view.name, func(t *testing.T) {
			m := view.setupFn()
			out := m.View()
			lines := viewLines(out)
			// Separator is line index 1 (0-indexed second line).
			sep := stripAnsiCodes(lines[1])
			if !strings.Contains(sep, "═") {
				t.Errorf("%s: separator does not use double-rule ═; got %q", view.name, sep[:min2(len(sep), 20)])
			}
		})
	}
}

// TestRetroHeaderBrand asserts the retro brand format ION//MEM appears in
// the header on both projects and observations views.
func TestRetroHeaderBrand(t *testing.T) {
	for _, tc := range []struct {
		name string
		view viewState
	}{
		{"projects", viewProjects},
		{"observations", viewObservations},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			m = setSize(m, 120, 30)
			m.view = tc.view
			if tc.view == viewObservations {
				m.selectedProject = "proj"
				m.observations = makeObsForRetroTest()
			} else {
				m.projects = makeProjectSummaries()
			}
			out := m.View()
			lines := viewLines(out)
			header := stripAnsiCodes(lines[0])
			if !strings.Contains(header, "ION//MEM") {
				t.Errorf("%s: header does not contain 'ION//MEM': %q", tc.name, header)
			}
		})
	}
}

// TestRetroBreadcrumbUppercase asserts that breadcrumbs in the header are
// uppercase (PROJECTS, OBSERVATIONS, etc.) as specified by the retro design.
func TestRetroBreadcrumbUppercase(t *testing.T) {
	cases := []struct {
		name      string
		view      viewState
		wantInHdr string
	}{
		{
			name:      "projects view",
			view:      viewProjects,
			wantInHdr: "PROJECTS",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			m = setSize(m, 120, 30)
			m.view = tc.view
			m.projects = makeProjectSummaries()
			out := m.View()
			lines := viewLines(out)
			header := stripAnsiCodes(lines[0])
			if !strings.Contains(header, tc.wantInHdr) {
				t.Errorf("%s: header does not contain %q; got %q", tc.name, tc.wantInHdr, header)
			}
		})
	}
}

// TestRetroEmptyStateDecoration asserts that the empty state message uses
// the ░░ decoration pattern from the retro design.
func TestRetroEmptyStateDecoration(t *testing.T) {
	m := newModel()
	m = setSize(m, 120, 30)
	m.view = viewObservations
	m.selectedProject = "emptyproject"
	m.observations = nil // no observations

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(plain, "░") {
		t.Errorf("empty state should contain ░ decoration; output plain text:\n%s", plain)
	}
}

// TestRetroTaglineRetroStyle asserts the tagline uses the retro `──` prefix/suffix.
func TestRetroTaglineRetroStyle(t *testing.T) {
	const termW, termH = 120, 40 // tall enough for logo
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeProjectSummaries()

	out := m.View()
	plain := stripAnsiCodes(out)
	// Retro tagline uses ── as decorators.
	if !strings.Contains(plain, "──") {
		t.Errorf("retro tagline should contain '──' decoration; plain text snippet:\n%s", plain[:min2(len(plain), 400)])
	}
}

// TestRetroFooterBBSFormat asserts the footer uses the BBS bracket key legend
// format [↑↓] MOVE rather than the standard bubbles help format.
func TestRetroFooterBBSFormat(t *testing.T) {
	m := newModel()
	m = setSize(m, 120, 30)
	m.view = viewObservations
	m.selectedProject = "proj"
	m.observations = makeObsForRetroTest()

	out := m.View()
	lines := viewLines(out)
	footer := stripAnsiCodes(lines[len(lines)-1])
	// The retro footer uses [↑↓] MOVE style brackets.
	if !strings.Contains(footer, "[") || !strings.Contains(footer, "]") {
		t.Errorf("retro footer should use bracket [] format; got: %q", footer)
	}
	// Should contain at least MOVE or OPEN or SEARCH as uppercase BBS labels.
	hasBBSLabel := strings.Contains(footer, "MOVE") ||
		strings.Contains(footer, "OPEN") ||
		strings.Contains(footer, "SEARCH") ||
		strings.Contains(footer, "QUIT")
	if !hasBBSLabel {
		t.Errorf("retro footer should contain uppercase BBS labels (MOVE, OPEN, SEARCH, QUIT); got: %q", footer)
	}
}

// TestRetroSelectedRowInverseVideo verifies that the selected row in the
// observations list does NOT contain the ▌ glyph (dropped in retro design).
func TestRetroSelectedRowNoGlyph(t *testing.T) {
	m := newModel()
	m = setSize(m, 120, 30)
	m.view = viewObservations
	m.selectedProject = "proj"
	m.observations = makeObsForRetroTest()
	m.obsCursor = 0

	out := m.View()
	plain := stripAnsiCodes(out)
	if strings.Contains(plain, "▌") {
		t.Errorf("retro design must not use ▌ glyph for selection; plain output:\n%s", plain)
	}
}

// TestRetroFuzzyChip verifies that when fuzzyResults is true, the status bar
// contains the ~FUZZY indicator (uppercase in retro design).
func TestRetroFuzzyChip(t *testing.T) {
	m := newModel()
	m = setSize(m, 120, 30)
	m.view = viewObservations
	m.selectedProject = "proj"
	m.observations = makeObsForRetroTest()
	m.searchQuery = "foo"
	m.fuzzyResults = true

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(plain, "FUZZY") {
		t.Errorf("fuzzy chip should show FUZZY; plain output:\n%s", plain)
	}
}

// ─── Literal view output for visual inspection at 120x30 ─────────────────────

// TestRetro_120x30_ProjectsLiteralView renders projects at 120x30 and logs
// the plain-text output for human inspection. Only fails on exact-fill.
func TestRetro_120x30_ProjectsLiteralView(t *testing.T) {
	const termW, termH = 120, 30
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewProjects
	m.projects = makeTwoProjects()
	m.projectCursor = 0
	m.projOffset = 0

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("120x30 projects: View() produced %d lines, want %d", lineCount, termH)
	}
	lines := viewLines(out)
	t.Log("=== RETRO projects 120x30 (plain text) ===")
	for i, l := range lines {
		t.Logf("%02d | %s", i+1, stripAnsiCodes(l))
	}
	t.Log("=== end ===")
}

// TestRetro_120x30_ObservationsLiteralView renders observations at 120x30
// and logs the plain-text output for human inspection. Only fails on exact-fill.
func TestRetro_120x30_ObservationsLiteralView(t *testing.T) {
	const termW, termH = 120, 30
	m := newModel()
	m = setSize(m, termW, termH)
	m.view = viewObservations
	m.selectedProject = "testproject"
	m.observations = makeObsForRetroTest()
	m.obsCursor = 2 // cursor on 3rd row to show inverse-video selection
	m.obsOffset = 0

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("120x30 observations: View() produced %d lines, want %d", lineCount, termH)
	}
	lines := viewLines(out)
	t.Log("=== RETRO observations 120x30 (plain text, selection stripped) ===")
	for i, l := range lines {
		t.Logf("%02d | %s", i+1, stripAnsiCodes(l))
	}
	t.Log("=== end ===")
}

// TestRetro_120x30_DetailLiteralView renders the detail view at 120x30
// and logs the plain-text output for human inspection. Only fails on exact-fill.
func TestRetro_120x30_DetailLiteralView(t *testing.T) {
	const termW, termH = 120, 30
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsForRetroTest()[0]
	obs.Content = "This is the detail body content. It should align with the meta block's indentation on wide terminals.\n\nSecond paragraph of content here."
	m.view = viewDetail
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.vp.Width = effectiveWidth(termW)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))
	m.vp.GotoTop()

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("120x30 detail: View() produced %d lines, want %d", lineCount, termH)
	}
	lines := viewLines(out)
	t.Log("=== RETRO detail 120x30 (plain text) ===")
	for i, l := range lines {
		t.Logf("%02d | %s", i+1, stripAnsiCodes(l))
	}
	t.Log("=== end ===")
}

// min2 returns the smaller of two ints.
func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
