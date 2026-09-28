package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

func int64Ptr(n int64) *int64    { return &n }
func stringPtr(s string) *string { return &s }

// TestViewDetail_ShowsStatusLineWhenSuperseded verifies that the detail view
// renders a STATUS line with the lineage pointer and reason for a superseded
// observation.
func TestViewDetail_ShowsStatusLineWhenSuperseded(t *testing.T) {
	m := newModel()
	m = setSize(m, 100, 40)
	m.view = viewDetail
	obs := store.Observation{
		ID: 1, Title: "old decision", Type: "decision",
		CreatedAt: time.Now().Format(time.RFC3339Nano), UpdatedAt: time.Now().Format(time.RFC3339Nano),
		Status: store.StatusSuperseded, SupersededBy: int64Ptr(42), StatusReason: stringPtr("replaced by newer approach"),
	}
	m.selectedObs = &obs

	out := m.View()
	if !strings.Contains(out, "STATUS") {
		t.Fatalf("viewDetail output missing STATUS line:\n%s", out)
	}
	if !strings.Contains(out, "superseded") {
		t.Errorf("viewDetail output missing status value 'superseded':\n%s", out)
	}
	if !strings.Contains(out, "#42") {
		t.Errorf("viewDetail output missing supersession pointer '#42':\n%s", out)
	}
	if !strings.Contains(out, "replaced by newer approach") {
		t.Errorf("viewDetail output missing status reason:\n%s", out)
	}
}

// TestViewDetail_NoStatusLineWhenActive verifies the STATUS line is absent
// for an active (or zero-value) observation, so this feature doesn't clutter
// the normal case.
func TestViewDetail_NoStatusLineWhenActive(t *testing.T) {
	m := newModel()
	m = setSize(m, 100, 40)
	m.view = viewDetail
	obs := store.Observation{
		ID: 1, Title: "current decision", Type: "decision",
		CreatedAt: time.Now().Format(time.RFC3339Nano), UpdatedAt: time.Now().Format(time.RFC3339Nano),
		Status: store.StatusActive,
	}
	m.selectedObs = &obs

	out := m.View()
	if strings.Contains(out, "STATUS") {
		t.Errorf("viewDetail output should not show STATUS line for an active observation:\n%s", out)
	}
}

// TestViewObservations_SupersededRowStaysVisible verifies that a superseded
// row is de-emphasized but its title remains present in the rendered list —
// memory hygiene changes state, it never hides rows from the list either.
func TestViewObservations_SupersededRowStaysVisible(t *testing.T) {
	m := newModel()
	m = setSize(m, 100, 40)
	m.view = viewObservations
	m.selectedProject = "alpha"
	m.observations = []store.Observation{
		{ID: 1, Title: "active decision", Type: "decision", CreatedAt: time.Now().Format(time.RFC3339Nano), Status: store.StatusActive},
		{ID: 2, Title: "superseded decision", Type: "decision", CreatedAt: time.Now().Format(time.RFC3339Nano), Status: store.StatusSuperseded},
	}
	m.obsCursor = 0

	out := m.View()
	stripped := stripAnsiCodes(out)
	if !strings.Contains(stripped, "active decision") {
		t.Errorf("viewObservations output missing active row title:\n%s", stripped)
	}
	if !strings.Contains(stripped, "superseded decision") {
		t.Errorf("viewObservations output missing superseded row title (must stay visible, only de-emphasized):\n%s", stripped)
	}

	for i, line := range viewLines(out) {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d exceeds width %d: %d chars: %q", i, m.width, w, line)
		}
	}
}
