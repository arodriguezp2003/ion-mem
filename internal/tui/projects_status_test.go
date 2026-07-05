package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// TestViewProjects_statusBarShowsLastDirectory verifies that when a project is
// selected in the projects view and its summary includes a LastDirectory, the
// rendered status bar contains that directory path.
func TestViewProjects_statusBarShowsLastDirectory(t *testing.T) {
	m := newModel()
	m.width = 80
	m.height = 24

	m.projects = []store.ProjectSummary{
		{
			Project:          "ion-mem",
			ObservationCount: 3,
			SessionCount:     1,
			LastActivity:     time.Now().Add(-1 * time.Hour),
			LastDirectory:    "/home/alice/ionix/ion-mem",
		},
	}
	m.projectCursor = 0
	m.view = viewProjects

	rendered := m.viewProjects()

	// The status bar must contain the most-recent directory for the selected project.
	if !strings.Contains(rendered, "/home/alice/ionix/ion-mem") {
		t.Errorf("status bar does not contain LastDirectory %q\nrendered:\n%s",
			"/home/alice/ionix/ion-mem", rendered)
	}
}

// TestViewProjects_statusBarNoDirectoryWhenEmpty verifies that when
// LastDirectory is empty the status bar falls back to the count-only format
// (no empty placeholder is shown).
func TestViewProjects_statusBarNoDirectoryWhenEmpty(t *testing.T) {
	m := newModel()
	m.width = 80
	m.height = 24

	m.projects = []store.ProjectSummary{
		{
			Project:          "fresh",
			ObservationCount: 0,
			SessionCount:     0,
			LastActivity:     time.Time{},
			LastDirectory:    "", // no directory known
		},
	}
	m.projectCursor = 0
	m.view = viewProjects

	rendered := m.viewProjects()

	// Status must contain the project count.
	if !strings.Contains(rendered, "1 PROJECT(S)") {
		t.Errorf("status bar should contain project count\nrendered:\n%s", rendered)
	}
	// Must not render a spurious empty path element.
	if strings.Contains(rendered, "//") {
		// Only fail if it looks like a broken double-slash from an empty path.
		// The brand "ION//MEM" is fine; "// " with a leading space is the problem.
		if strings.Contains(rendered, "  // ") {
			t.Errorf("status bar appears to contain empty directory placeholder\nrendered:\n%s", rendered)
		}
	}
}
