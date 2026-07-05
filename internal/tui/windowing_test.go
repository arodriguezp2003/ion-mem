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

	visible := m.listVisibleHeight(false)

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
	visible := m.listVisibleHeight(false)
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
	visible := m.listVisibleHeight(false)

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

// TestRenderSmoke builds a model with 50 fake observations at 80x24, moves the
// cursor beyond the first window, and verifies:
//  1. The selected row IS present in View() output.
//  2. The number of rendered list rows fits within the terminal height.
//  3. The position indicator ("cursor/total") appears in the output.
func TestRenderSmoke_ObservationsScrolledMidList(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
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

	// 2. Line count must not exceed terminal height.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 24 {
		t.Errorf("View() produced %d lines, want <= 24 (terminal height)\noutput:\n%s",
			len(lines), out)
	}

	// 3. Position indicator must be present.
	want := fmt.Sprintf("%d/50", m.obsCursor+1)
	if !strings.Contains(out, want) {
		t.Errorf("View() does not contain position indicator %q\noutput:\n%s", want, out)
	}
}

func TestRenderSmoke_ProjectsView(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
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

	// Line count must not exceed terminal height.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 24 {
		t.Errorf("View() produced %d lines, want <= 24\noutput:\n%s", len(lines), out)
	}

	// Position indicator.
	if !strings.Contains(out, "1/3") {
		t.Errorf("View() does not contain position indicator '1/3'\noutput:\n%s", out)
	}
}
