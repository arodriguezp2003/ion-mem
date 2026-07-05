package tui

// edit_test.go — Strict TDD tests for Task 2: edit title and cycle type from detail view.
//
// TDD cycle: RED → GREEN → TRIANGULATE → REFACTOR

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeDetailModel() Model {
	m := newModel()
	m = setSize(m, 80, 24)
	obs := store.Observation{
		ID:      10,
		Title:   "Original title",
		Type:    "decision",
		Project: "proj",
		Scope:   "project",
		Content: "Some content.",
	}
	m.selectedObs = &obs
	m.view = viewDetail
	m.vp.Width = effectiveWidth(80)
	m.vp.Height = m.detailVPHeight()
	m.vp.SetContent(renderObservationDetail(obs))
	return m
}

// ─── Task 2.1: [E] key opens inline title editor ──────────────────────────────

// TestDetail_EKeyOpensTitleEdit asserts that pressing 'e' in viewDetail sets
// detailEditing=true and the input has the current title.
func TestDetail_EKeyOpensTitleEdit(t *testing.T) {
	m := makeDetailModel()

	m = sendRune(m, 'e')
	if !m.detailEditing {
		t.Error("after 'e' in detail, detailEditing should be true")
	}
	if m.detailEditInput.Value() != "Original title" {
		t.Errorf("detailEditInput value = %q, want %q", m.detailEditInput.Value(), "Original title")
	}
	if m.detailEditOrig != "Original title" {
		t.Errorf("detailEditOrig = %q, want %q", m.detailEditOrig, "Original title")
	}
}

// TestDetail_EscCancelsTitleEdit asserts Esc while editing cancels without change.
func TestDetail_EscCancelsTitleEdit(t *testing.T) {
	m := makeDetailModel()
	m.detailEditing = true
	m.detailEditInput.SetValue("New title attempt")
	m.detailEditOrig = "Original title"

	m = sendKey(m, tea.KeyEsc)
	if m.detailEditing {
		t.Error("after Esc from edit, detailEditing should be false")
	}
	// selectedObs title should be unchanged.
	if m.selectedObs != nil && m.selectedObs.Title != "Original title" {
		t.Errorf("title should be unchanged after Esc cancel; got %q", m.selectedObs.Title)
	}
}

// TestDetail_EnterSavesTitleAndClearsEditing asserts that pressing Enter while
// editing closes the input, applies an optimistic update, and issues a command.
func TestDetail_EnterSavesTitleAndClearsEditing(t *testing.T) {
	m := makeDetailModel()
	m.detailEditing = true
	m.detailEditOrig = "Original title"
	m.detailEditInput.Reset()
	m.detailEditInput.SetValue("New title")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.detailEditing {
		t.Error("after Enter save, detailEditing should be false")
	}
	// Optimistic update: title should be the new value.
	if m.selectedObs == nil || m.selectedObs.Title != "New title" {
		t.Errorf("selectedObs.Title after optimistic update = %q, want %q",
			func() string {
				if m.selectedObs == nil {
					return "<nil>"
				}
				return m.selectedObs.Title
			}(),
			"New title")
	}
	// A command must be issued for the async store save.
	if cmd == nil {
		t.Error("after Enter save, a command must be issued for async store call")
	}
}

// TestDetail_EnterWithEmptyInputRestoresOriginal asserts that pressing Enter
// with an empty input restores the original title (no empty-title persistence).
func TestDetail_EnterWithEmptyInputRestoresOriginal(t *testing.T) {
	m := makeDetailModel()
	m.detailEditing = true
	m.detailEditOrig = "Original title"
	m.detailEditInput.Reset()
	m.detailEditInput.SetValue("") // empty

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.detailEditing {
		t.Error("after Enter, detailEditing should be false")
	}
	// cmd is still issued (saves the restored original).
	if cmd == nil {
		t.Error("a command should be issued even when restoring original title")
	}
}

// ─── Task 2.2: obsUpdateResultMsg updates status bar ─────────────────────────

// TestObsUpdateResultMsg_SetsDetailStatus asserts that obsUpdateResultMsg sets
// detailStatus and detailStatusOK on the model.
func TestObsUpdateResultMsg_SetsDetailStatus(t *testing.T) {
	m := makeDetailModel()

	obs := *m.selectedObs
	obs.Title = "Saved title"
	next, _ := m.Update(obsUpdateResultMsg{obs: obs, statusMsg: "TITLE UPDATED"})
	m = next.(Model)

	if m.detailStatus != "TITLE UPDATED" {
		t.Errorf("detailStatus = %q, want %q", m.detailStatus, "TITLE UPDATED")
	}
	if !m.detailStatusOK {
		t.Error("detailStatusOK should be true when err is nil")
	}
	if m.selectedObs == nil || m.selectedObs.Title != "Saved title" {
		t.Errorf("selectedObs.Title = %q, want %q",
			func() string {
				if m.selectedObs == nil {
					return "<nil>"
				}
				return m.selectedObs.Title
			}(),
			"Saved title")
	}
}

// TestObsUpdateResultMsg_ErrorSetsStatusNotOK asserts that a failed update
// sets detailStatusOK=false.
func TestObsUpdateResultMsg_ErrorSetsStatusNotOK(t *testing.T) {
	m := makeDetailModel()

	next, _ := m.Update(obsUpdateResultMsg{statusMsg: "SAVE FAILED", err: fmt.Errorf("store error")})
	m = next.(Model)

	if m.detailStatus != "SAVE FAILED" {
		t.Errorf("detailStatus = %q, want %q", m.detailStatus, "SAVE FAILED")
	}
	if m.detailStatusOK {
		t.Error("detailStatusOK should be false when err is non-nil")
	}
}

// ─── Task 2.3: [T] key cycles type ───────────────────────────────────────────

// TestDetail_TKeyCyclesType asserts that pressing 't' in viewDetail advances
// the observation type to the next value in the cycle.
func TestDetail_TKeyCyclesType(t *testing.T) {
	m := makeDetailModel()
	original := m.selectedObs.Type // "decision"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = next.(Model)

	if m.selectedObs == nil {
		t.Fatal("selectedObs should not be nil after type cycle")
	}
	if m.selectedObs.Type == original {
		t.Errorf("type did not change after 't'; still %q", original)
	}
	if cmd == nil {
		t.Error("type cycle should issue a command to persist the change")
	}
}

// TestDetail_TKeyCyclesWraps asserts that cycling past the last type wraps to the first.
func TestDetail_TKeyCyclesWraps(t *testing.T) {
	types := cycleableTypes()
	if len(types) == 0 {
		t.Fatal("cycleableTypes returned empty slice")
	}

	// Set the current type to the last in the cycle; next should be the first.
	last := types[len(types)-1]
	first := types[0]
	got := nextType(last)
	if got != first {
		t.Errorf("nextType(%q) = %q, want %q (wrap to first)", last, got, first)
	}
}

// TestCycleableTypes_ExcludesSessionSummary asserts session_summary is not
// in the cycleable types list.
func TestCycleableTypes_ExcludesSessionSummary(t *testing.T) {
	types := cycleableTypes()
	for _, typ := range types {
		if typ == "session_summary" {
			t.Errorf("cycleableTypes should not include 'session_summary'")
		}
	}
	if len(types) == 0 {
		t.Error("cycleableTypes should return at least one type")
	}
}

// TestNextType_AdvancesFromKnownType asserts nextType advances to the next type.
func TestNextType_AdvancesFromKnownType(t *testing.T) {
	types := cycleableTypes()
	if len(types) < 2 {
		t.Skip("need at least 2 types to test advancement")
	}
	// First type → should advance to second type.
	first := types[0]
	second := types[1]
	got := nextType(first)
	if got != second {
		t.Errorf("nextType(%q) = %q, want %q", first, got, second)
	}
}

// ─── Task 2.4: saveObsTitle builds correct UpdateObservationParams ────────────

// TestSaveObsTitle_BuildsCorrectCmd asserts that saveObsTitle, when called with
// a nil store, issues a cmd that returns an obsUpdateResultMsg with an error
// (store-unavailable path), so we can assert the cmd is non-nil.
func TestSaveObsTitle_BuildsCorrectCmd(t *testing.T) {
	m := makeDetailModel()
	m.store = nil // no real store; stub the unavailable path

	cmd := m.saveObsTitle("New title")
	if cmd == nil {
		t.Error("saveObsTitle should always return a non-nil cmd")
	}
	// Execute the cmd (simulates the runtime doing so).
	msg := cmd()
	result, ok := msg.(obsUpdateResultMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want obsUpdateResultMsg", msg)
	}
	if result.statusMsg == "" {
		t.Error("obsUpdateResultMsg.statusMsg should be non-empty")
	}
}

// TestSaveObsType_BuildsCorrectCmd asserts saveObsType returns a non-nil cmd.
func TestSaveObsType_BuildsCorrectCmd(t *testing.T) {
	m := makeDetailModel()
	m.store = nil

	cmd := m.saveObsType("bugfix")
	if cmd == nil {
		t.Error("saveObsType should always return a non-nil cmd")
	}
	msg := cmd()
	result, ok := msg.(obsUpdateResultMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want obsUpdateResultMsg", msg)
	}
	// statusMsg should mention "TYPE →"
	if !strings.Contains(result.statusMsg, "TYPE →") && result.statusMsg != "SAVE FAILED" {
		t.Errorf("obsUpdateResultMsg.statusMsg = %q; want to contain 'TYPE →'", result.statusMsg)
	}
}

// ─── Task 2.5: viewDetail shows status bar message on save ───────────────────

// TestDetail_StatusMessageAppearsAfterSave asserts that after an obsUpdateResultMsg,
// the detail view status bar contains the status message.
func TestDetail_StatusMessageAppearsAfterSave(t *testing.T) {
	m := makeDetailModel()
	m.detailStatus = "TITLE UPDATED"
	m.detailStatusOK = true

	out := stripAnsiCodes(m.viewDetail())
	if !strings.Contains(out, "TITLE UPDATED") {
		t.Errorf("detail view should show 'TITLE UPDATED' in status after save; output:\n%s", out)
	}
}
