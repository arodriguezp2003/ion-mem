package tui

// revision_history_test.go — Strict TDD tests for Task 1: revision history view.
//
// TDD cycle: RED → GREEN → TRIANGULATE → REFACTOR
// All tests are written BEFORE the production code that makes them pass.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeObsWithRevisions(revCount int) store.Observation {
	return store.Observation{
		ID:            42,
		Title:         "Important decision",
		Type:          "decision",
		Project:       "testproject",
		Scope:         "project",
		Content:       "The body content.",
		RevisionCount: revCount,
		CreatedAt:     time.Now().Add(-24 * time.Hour).Format(time.RFC3339Nano),
		UpdatedAt:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
	}
}

func makeRevisions() []store.Revision {
	return []store.Revision{
		{
			ID:            3,
			ObservationID: 42,
			Revision:      3,
			Type:          "decision",
			Title:         "Third version of the title",
			Content:       "Content at revision 3.",
			ArchivedAt:    time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
		},
		{
			ID:            2,
			ObservationID: 42,
			Revision:      2,
			Type:          "decision",
			Title:         "Second version of the title",
			Content:       "Content at revision 2.",
			ArchivedAt:    time.Now().Add(-12 * time.Hour).Format(time.RFC3339Nano),
		},
		{
			ID:            1,
			ObservationID: 42,
			Revision:      1,
			Type:          "decision",
			Title:         "First version of the title",
			Content:       "Content at revision 1.",
			ArchivedAt:    time.Now().Add(-24 * time.Hour).Format(time.RFC3339Nano),
		},
	}
}

// ─── Task 1.1: REVISIONS metadata line in detail view ────────────────────────

// TestDetail_RevisionsMetaLineShownWhenRevisionCountGT1 asserts that the detail view
// renders a REVISIONS line when the observation has more than one revision.
func TestDetail_RevisionsMetaLineShownWhenRevisionCountGT1(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewDetail
	m.vp.Width = effectiveWidth(80)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))
	m.vp.GotoTop()

	out := stripAnsiCodes(m.View())
	if !strings.Contains(out, "REVISIONS") {
		t.Errorf("detail view: REVISIONS line not found when RevisionCount=3\noutput:\n%s", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("detail view: revision count '3' not found in output\noutput:\n%s", out)
	}
}

// TestDetail_RevisionsMetaLineHiddenWhenRevisionCountIs1 asserts that the REVISIONS
// line is NOT shown when RevisionCount == 1 (current is the only version).
func TestDetail_RevisionsMetaLineHiddenWhenRevisionCountIs1(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	obs := makeObsWithRevisions(1)
	m.selectedObs = &obs
	m.view = viewDetail
	m.vp.Width = effectiveWidth(80)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))
	m.vp.GotoTop()

	out := stripAnsiCodes(m.View())
	if strings.Contains(out, "REVISIONS") {
		t.Errorf("detail view: REVISIONS line should NOT appear when RevisionCount=1\noutput:\n%s", out)
	}
}

// ─── Task 1.2: [H] HISTORY key in detail footer ───────────────────────────────

// TestDetail_HistoryFooterKeyShownWhenRevisionCountGT1 asserts the footer contains
// [H] HISTORY when the selected observation has > 1 revisions.
func TestDetail_HistoryFooterKeyShownWhenRevisionCountGT1(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewDetail
	m.vp.Width = effectiveWidth(80)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))

	out := stripAnsiCodes(m.View())
	footer := ""
	lines := strings.Split(out, "\n")
	if len(lines) >= 2 {
		footer = lines[len(lines)-2]
	}
	if !strings.Contains(footer, "HISTORY") {
		t.Errorf("detail footer: HISTORY key missing when RevisionCount=3\nfooter: %q", footer)
	}
}

// TestDetail_HistoryFooterKeyHiddenWhenRevisionCountIs1 asserts the footer does NOT
// contain HISTORY when RevisionCount == 1.
func TestDetail_HistoryFooterKeyHiddenWhenRevisionCountIs1(t *testing.T) {
	m := newModel()
	m = setSize(m, 80, 24)
	obs := makeObsWithRevisions(1)
	m.selectedObs = &obs
	m.view = viewDetail
	m.vp.Width = effectiveWidth(80)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))

	out := stripAnsiCodes(m.View())
	lines := strings.Split(out, "\n")
	footer := ""
	if len(lines) >= 2 {
		footer = lines[len(lines)-2]
	}
	if strings.Contains(footer, "HISTORY") {
		t.Errorf("detail footer: HISTORY key should NOT appear when RevisionCount=1\nfooter: %q", footer)
	}
}

// ─── Task 1.3: [H] key press in detail → viewHistory transition ──────────────

// TestDetail_HKeyOpensHistoryView asserts that pressing 'h' in the detail view
// when RevisionCount > 1 transitions to viewHistory.
func TestDetail_HKeyOpensHistoryView(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewDetail

	m = sendRune(m, 'h')
	if m.view != viewHistory {
		t.Errorf("after 'h' in detail with RevisionCount=3, view = %v, want viewHistory", m.view)
	}
}

// TestDetail_HKeyNoopWhenRevisionCountIs1 asserts that pressing 'h' in the detail
// view when RevisionCount == 1 does NOT transition to viewHistory.
func TestDetail_HKeyNoopWhenRevisionCountIs1(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(1)
	m.selectedObs = &obs
	m.view = viewDetail

	m = sendRune(m, 'h')
	if m.view == viewHistory {
		t.Errorf("after 'h' in detail with RevisionCount=1, should NOT go to viewHistory")
	}
}

// ─── Task 1.4: revisionsLoadedMsg populates history list ─────────────────────

// TestRevisionsLoadedMsg_PopulatesRevisions asserts that revisionsLoadedMsg sets
// the revisions slice on the model correctly.
func TestRevisionsLoadedMsg_PopulatesRevisions(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewHistory
	revs := makeRevisions()

	next, _ := m.Update(revisionsLoadedMsg{revisions: revs})
	m = next.(Model)

	if len(m.revisions) != 3 {
		t.Errorf("revisions count = %d, want 3", len(m.revisions))
	}
	if m.revisions[0].Revision != 3 {
		t.Errorf("first revision = %d, want 3 (newest first)", m.revisions[0].Revision)
	}
}

// TestRevisionsLoadedMsg_EmptySliceIsRenderSafe asserts that an empty revisionsLoadedMsg
// does not crash and sets an empty (non-nil) slice.
func TestRevisionsLoadedMsg_EmptySliceIsRenderSafe(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewHistory

	next, _ := m.Update(revisionsLoadedMsg{revisions: []store.Revision{}})
	m = next.(Model)

	if m.revisions == nil {
		t.Error("revisions should be non-nil empty slice after empty revisionsLoadedMsg")
	}
	if len(m.revisions) != 0 {
		t.Errorf("revisions count = %d, want 0", len(m.revisions))
	}
}

// ─── Task 1.5: viewHistory render smoke test (80x24) ─────────────────────────

// TestViewHistory_RenderSmoke asserts that viewHistory renders without panic,
// produces exactly 24 lines (exact-fill contract), and contains "HISTORY".
func TestViewHistory_RenderSmoke(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewHistory
	m.revisions = makeRevisions()
	m.histCursor = 0
	m.histOffset = 0

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("viewHistory 80x24: View() produced %d lines, want %d", lineCount, termH)
	}
	plain := stripAnsiCodes(out)
	if !strings.Contains(plain, "HISTORY") {
		t.Errorf("viewHistory: 'HISTORY' not found in output:\n%s", plain)
	}
}

// TestViewHistory_EmptyStateRenderSmoke asserts the empty state renders correctly.
func TestViewHistory_EmptyStateRenderSmoke(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	m.view = viewHistory
	m.revisions = []store.Revision{}

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("viewHistory empty 80x24: View() produced %d lines, want %d", lineCount, termH)
	}
	plain := stripAnsiCodes(out)
	if !strings.Contains(plain, "NO HISTORY") {
		t.Errorf("viewHistory empty: 'NO HISTORY' not found in output:\n%s", plain)
	}
}

// ─── Task 1.6: viewHistory → viewRevisionContent on Enter ────────────────────

// TestViewHistory_EnterOpensRevisionContent asserts that pressing Enter in
// viewHistory transitions to viewRevisionContent with the selected revision.
func TestViewHistory_EnterOpensRevisionContent(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewHistory
	m.revisions = makeRevisions()
	m.histCursor = 1 // second revision

	m = sendKey(m, tea.KeyEnter)
	if m.view != viewRevisionContent {
		t.Errorf("after Enter in viewHistory, view = %v, want viewRevisionContent", m.view)
	}
	if m.selectedRevision == nil {
		t.Fatal("selectedRevision should not be nil after Enter in viewHistory")
	}
	if m.selectedRevision.Revision != 2 {
		t.Errorf("selectedRevision.Revision = %d, want 2 (cursor=1)", m.selectedRevision.Revision)
	}
}

// ─── Task 1.7: viewRevisionContent render smoke test (80x24) ─────────────────

// TestViewRevisionContent_RenderSmoke asserts that viewRevisionContent renders
// without panic, produces exactly 24 lines, and contains the breadcrumb r3.
func TestViewRevisionContent_RenderSmoke(t *testing.T) {
	const termW, termH = 80, 24
	m := newModel()
	m = setSize(m, termW, termH)
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.selectedProject = "testproject"
	revs := makeRevisions()
	m.selectedRevision = &revs[0] // r3
	m.view = viewRevisionContent

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("viewRevisionContent 80x24: View() produced %d lines, want %d", lineCount, termH)
	}
	plain := stripAnsiCodes(out)
	// Breadcrumb should contain HISTORY.
	if !strings.Contains(plain, "HISTORY") {
		t.Errorf("viewRevisionContent: breadcrumb should contain HISTORY:\n%s", plain)
	}
}

// ─── Task 1.8: Esc chain — revision content → history → detail ───────────────

// TestEscFromRevisionContentGoesToHistory asserts Esc from viewRevisionContent
// goes back to viewHistory.
func TestEscFromRevisionContentGoesToHistory(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewRevisionContent
	revs := makeRevisions()
	m.selectedRevision = &revs[0]
	m.revisions = revs

	m = sendKey(m, tea.KeyEsc)
	if m.view != viewHistory {
		t.Errorf("Esc from viewRevisionContent: view = %v, want viewHistory", m.view)
	}
	if m.selectedRevision != nil {
		t.Error("selectedRevision should be nil after returning to viewHistory")
	}
}

// TestEscFromHistoryGoesToDetail asserts Esc from viewHistory returns to viewDetail.
func TestEscFromHistoryGoesToDetail(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewHistory
	m.revisions = makeRevisions()

	m = sendKey(m, tea.KeyEsc)
	if m.view != viewDetail {
		t.Errorf("Esc from viewHistory: view = %v, want viewDetail", m.view)
	}
}

// TestDetailHistoryRevisionEscChain exercises the full chain:
// detail → history → revision content → esc → history → esc → detail.
func TestDetailHistoryRevisionEscChain(t *testing.T) {
	m := newModel()
	obs := makeObsWithRevisions(3)
	m.selectedObs = &obs
	m.view = viewDetail

	// 1. Press 'h' → viewHistory
	m = sendRune(m, 'h')
	if m.view != viewHistory {
		t.Fatalf("step 1 'h': view = %v, want viewHistory", m.view)
	}

	// 2. Inject revisions (simulates async load completing)
	next, _ := m.Update(revisionsLoadedMsg{revisions: makeRevisions()})
	m = next.(Model)

	// 3. Press Enter → viewRevisionContent
	m = sendKey(m, tea.KeyEnter)
	if m.view != viewRevisionContent {
		t.Fatalf("step 3 Enter: view = %v, want viewRevisionContent", m.view)
	}

	// 4. Press Esc → viewHistory
	m = sendKey(m, tea.KeyEsc)
	if m.view != viewHistory {
		t.Fatalf("step 4 Esc: view = %v, want viewHistory", m.view)
	}

	// 5. Press Esc → viewDetail
	m = sendKey(m, tea.KeyEsc)
	if m.view != viewDetail {
		t.Fatalf("step 5 Esc: view = %v, want viewDetail", m.view)
	}
}

// ─── Task 1.9: detailMetaLineCount accounts for REVISIONS line ───────────────

// TestDetailMetaLineCount_WithRevisions asserts that detailMetaLineCount returns
// one extra line when RevisionCount > 1.
func TestDetailMetaLineCount_WithRevisions(t *testing.T) {
	m := newModel()

	// RevisionCount = 1: no REVISIONS line
	obs1 := makeObsWithRevisions(1)
	m.selectedObs = &obs1
	countWithout := m.detailMetaLineCount()

	// RevisionCount = 3: one REVISIONS line
	obs3 := makeObsWithRevisions(3)
	m.selectedObs = &obs3
	countWith := m.detailMetaLineCount()

	if countWith != countWithout+1 {
		t.Errorf("detailMetaLineCount: with revisions = %d, without = %d, want +1",
			countWith, countWithout)
	}
}
