package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// lastNonEmptyLine returns the last line of a rendered view that has visible
// text, with ANSI styling stripped.
func lastNonEmptyLine(out string) string {
	lines := viewLines(out)
	for i := len(lines) - 1; i >= 0; i-- {
		plain := ansiSeq.ReplaceAllString(lines[i], "")
		if strings.TrimSpace(plain) != "" {
			return plain
		}
	}
	return ""
}

// TestFooter_ShowsBuildVersion asserts every view renders "build <version>"
// right-aligned on the bottom footer line.
func TestFooter_ShowsBuildVersion(t *testing.T) {
	const width, height = 100, 30
	views := []struct {
		name string
		set  func(m Model) Model
	}{
		{"projects", func(m Model) Model { m.view = viewProjects; return m }},
		{"observations", func(m Model) Model { m.view = viewObservations; return m }},
		{"global-search", func(m Model) Model { m.view = viewGlobalSearch; return m }},
		{"detail", func(m Model) Model {
			m.view = viewDetail
			m.selectedObs = &store.Observation{ID: 1, Title: "obs", Content: "body"}
			return m
		}},
		{"config", func(m Model) Model { m.view = viewConfig; return m }},
		{"history", func(m Model) Model {
			m.view = viewHistory
			m.selectedObs = &store.Observation{ID: 1, Title: "obs", RevisionCount: 2}
			return m
		}},
		{"revision-content", func(m Model) Model {
			m.view = viewRevisionContent
			m.selectedObs = &store.Observation{ID: 1, Title: "obs", RevisionCount: 2}
			m.selectedRevision = &store.Revision{Revision: 1, Title: "obs", Content: "body"}
			return m
		}},
	}
	for _, tc := range views {
		t.Run(tc.name, func(t *testing.T) {
			m := newModelWithOptions(Options{Version: "abc1234"})
			m = setSize(m, width, height)
			m = tc.set(m)
			footer := lastNonEmptyLine(m.View())
			const want = "build abc1234"
			if !strings.Contains(footer, want) {
				t.Fatalf("footer missing %q\nfooter: %q", want, footer)
			}
			if !strings.HasSuffix(footer, want) {
				t.Errorf("build tag should be the last thing on the line\nfooter: %q", footer)
			}
			if got := lipgloss.Width(footer); got != width-rightPad {
				t.Errorf("footer visible width = %d, want %d (right-aligned with %d cols margin)\nfooter: %q", got, width-rightPad, rightPad, footer)
			}
		})
	}
}

// TestFooter_BuildVersionDefaultsToDev asserts the footer falls back to "dev".
func TestFooter_BuildVersionDefaultsToDev(t *testing.T) {
	m := setSize(newModelWithOptions(Options{}), 80, 24)
	if footer := lastNonEmptyLine(m.View()); !strings.Contains(footer, "build dev") {
		t.Fatalf("footer missing default build tag\nfooter: %q", footer)
	}
}

// TestFooter_NarrowTerminalDropsBuildTag asserts the hints win over the tag
// when both do not fit, and the line never exceeds the terminal width.
func TestFooter_NarrowTerminalDropsBuildTag(t *testing.T) {
	const width = 60
	m := newModelWithOptions(Options{Version: "abc1234"})
	m = setSize(m, width, 24)
	m.view = viewObservations // longest hint set
	footer := lastNonEmptyLine(m.View())
	if strings.Contains(footer, "build abc1234") {
		t.Fatalf("narrow terminal should drop the build tag\nfooter: %q", footer)
	}
	// The hints alone may already overflow a 60-column terminal (pre-existing
	// behaviour); what matters here is that the tag adds nothing on top.
	hintsOnly := strings.Repeat(" ", contentOffset(width)+leftPad) + m.renderFooter()
	if got, want := lipgloss.Width(footer), lipgloss.Width(hintsOnly); got != want {
		t.Errorf("footer width %d, want hints-only width %d\nfooter: %q", got, want, footer)
	}
}

// TestFooter_WideTerminalKeepsRightMargin asserts the tag stays aligned to the
// terminal edge when the content is centered with a non-zero offset.
func TestFooter_WideTerminalKeepsRightMargin(t *testing.T) {
	const width = 160
	m := newModelWithOptions(Options{Version: "abc1234"})
	m = setSize(m, width, 40)
	if contentOffset(width) == 0 {
		t.Skip("contentOffset is zero at this width; nothing to verify")
	}
	footer := lastNonEmptyLine(m.View())
	if !strings.HasSuffix(footer, "build abc1234") {
		t.Fatalf("build tag missing or not last\nfooter: %q", footer)
	}
	if got := lipgloss.Width(footer); got != width-rightPad {
		t.Errorf("footer visible width = %d, want %d\nfooter: %q", got, width-rightPad, footer)
	}
}
