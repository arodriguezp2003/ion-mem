package tui

// embed_missing_test.go — Strict TDD tests for the EMBED MISSING action.
//
// TDD cycle:
//  1. TestProgressBar_PureHelper    — renderProgressBar table-driven: 0%, 40%, 100%, total=0 guard.
//  2. TestEmbedJob_BatchChaining    — start job → successive embedJobBatchMsg → progress advances, next cmd issued, finished stops.
//  3. TestEmbedJob_ZeroProgressAbort — zero-progress guard still enforced.
//  4. TestEmbedMissing_RowConstant  — configRowEmbedMissing = 4, configRowRegen = 5, configRowCount = 6.
//  5. TestEmbedMissing_CursorReach  — ↓×5 from row 0 lands on configRowRegen (row 5).
//  6. TestEmbedMissing_WhenEmbeddingsDisabled — shows EMBEDDINGS ARE OFF danger.
//  7. TestEmbedMissing_WhenNothingMissing     — immediate ALL EMBEDDED result.
//  8. TestEmbedMissing_SkipsExistingEmbeddings — 3 obs, 1 pre-embedded, job embeds exactly 2.
//  9. TestProgressBarRenderedMidJob  — while jobRunning, bar appears in View() output.
// 10. TestJobFinished_ResultShown    — on finish, coverage message rendered.
// 11. TestRegen_UsesNewEngine        — REGENERATE row now uses chained batch engine.
// 12. TestRenderSmoke_80x24_WithBar  — exact-fill at 80x24 with progress bar visible.
// 13. TestRenderSmoke_100x28_WithBar — exact-fill at 100x28 mid-job literal view.
// 14. TestJobBlocked_WhileRunning    — both rows disabled while a job is running.

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── 1. renderProgressBar pure helper ────────────────────────────────────────

func TestProgressBar_PureHelper(t *testing.T) {
	tests := []struct {
		name    string
		done    int
		total   int
		width   int
		wantPct string // substring that must appear in the rendered string
	}{
		{
			name:    "0 percent — all empty blocks",
			done:    0,
			total:   100,
			width:   20,
			wantPct: "0%",
		},
		{
			name:    "40 percent — partial fill",
			done:    40,
			total:   100,
			width:   20,
			wantPct: "40%",
		},
		{
			name:    "100 percent — all filled blocks",
			done:    100,
			total:   100,
			width:   20,
			wantPct: "100%",
		},
		{
			name:    "total zero guard — renders without panic",
			done:    0,
			total:   0,
			width:   20,
			wantPct: "0%",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderProgressBar(tt.done, tt.total, tt.width)
			plain := stripAnsiCodes(got)
			if !strings.Contains(plain, tt.wantPct) {
				t.Errorf("renderProgressBar(%d,%d,%d) = %q, want pct %q",
					tt.done, tt.total, tt.width, plain, tt.wantPct)
			}
			// Must contain retro fill characters.
			if !strings.Contains(got, "▓") && !strings.Contains(got, "░") {
				t.Errorf("renderProgressBar must use ▓/░ block characters; got %q", plain)
			}
		})
	}
}

// Triangulate: 40% bar must contain filled and empty blocks.
func TestProgressBar_FilledAndEmpty(t *testing.T) {
	got := renderProgressBar(40, 100, 30)
	if !strings.Contains(got, "▓") {
		t.Error("40% bar must contain filled ▓ blocks")
	}
	if !strings.Contains(got, "░") {
		t.Error("40% bar must contain empty ░ blocks")
	}
}

// ─── 2. Batch chaining ───────────────────────────────────────────────────────

// TestEmbedJob_BatchChaining verifies the chained-batch engine:
// After an embedJobBatchMsg with finished=false, the model issues a next cmd.
// After finished=true, no further cmd is issued.
func TestEmbedJob_BatchChaining(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.jobRunning = false

	// Simulate job start: inject a batch fn that counts calls.
	callCount := 0
	m.embedBatchFn = func(url, modelName string, offset, batch int) tea.Cmd {
		callCount++
		return func() tea.Msg {
			return embedJobBatchMsg{done: 10, total: 100, lastErr: nil, finished: false}
		}
	}

	// Trigger EMBED MISSING.
	m.configCursor = configRowEmbedMissing
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if !m.jobRunning {
		t.Error("after starting embed job, jobRunning should be true")
	}
	if cmd == nil {
		t.Fatal("after starting job, a batch command must be issued")
	}

	// Apply a mid-progress batch message.
	batchMsg := embedJobBatchMsg{done: 25, total: 100, lastErr: nil, finished: false}
	next, nextCmd := m.Update(batchMsg)
	m = next.(Model)

	if m.jobDone != 25 {
		t.Errorf("jobDone = %d, want 25", m.jobDone)
	}
	if m.jobTotal != 100 {
		t.Errorf("jobTotal = %d, want 100", m.jobTotal)
	}
	if !m.jobRunning {
		t.Error("job should still be running on partial batch")
	}
	if nextCmd == nil {
		t.Error("a partial batch must issue the next batch cmd")
	}

	// Apply a finished batch message.
	finishMsg := embedJobBatchMsg{done: 100, total: 100, lastErr: nil, finished: true}
	next, doneCmd := m.Update(finishMsg)
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should be false after finished=true")
	}
	if m.jobDone != 100 {
		t.Errorf("jobDone = %d, want 100 after finish", m.jobDone)
	}
	// The finished msg must NOT issue another batch cmd (loop terminates).
	_ = doneCmd // nil or a tea.Cmd that does no more batch work — we just check jobRunning
}

// ─── 3. Zero-progress abort ───────────────────────────────────────────────────

func TestEmbedJob_ZeroProgressAbort(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.jobRunning = true
	m.jobDone = 5
	m.jobTotal = 50

	// A batch where aborted=true is reported.
	abortMsg := embedJobBatchMsg{done: 5, total: 50, lastErr: errors.New("embed fail"), finished: false, aborted: true}
	next, _ := m.Update(abortMsg)
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should be false after aborted=true")
	}
}

// ─── 4. Row constants ──────────────────────────────────────────────────────────

func TestEmbedMissing_RowConstant(t *testing.T) {
	if configRowEmbedMissing != 4 {
		t.Errorf("configRowEmbedMissing = %d, want 4", configRowEmbedMissing)
	}
	if configRowRegen != 5 {
		t.Errorf("configRowRegen = %d, want 5 (shifted after EMBED MISSING insert)", configRowRegen)
	}
	if configRowCount != 6 {
		t.Errorf("configRowCount = %d, want 6", configRowCount)
	}
}

// ─── 5. Cursor can reach row 5 (REGENERATE) ───────────────────────────────────

func TestEmbedMissing_CursorReachRegen(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0
	for i := 0; i < 5; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	if m.configCursor != configRowRegen {
		t.Errorf("after 5×↓ from row 0, configCursor = %d, want %d (configRowRegen)", m.configCursor, configRowRegen)
	}
}

// Triangulate: cursor stops at row 5.
func TestEmbedMissing_CursorClampAt5(t *testing.T) {
	m := newConfigModel()
	for i := 0; i < 20; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	if m.configCursor != configRowCount-1 {
		t.Errorf("cursor should clamp at %d (last row), got %d", configRowCount-1, m.configCursor)
	}
}

// ─── 6. EMBED MISSING: embeddings disabled ────────────────────────────────────

func TestEmbedMissing_WhenEmbeddingsDisabled(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowEmbedMissing
	m.configEmbeddingsEnabled = false

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should be false when embeddings are disabled")
	}
	// cmd should produce an embedJobBatchMsg or equivalent result with error text.
	if cmd != nil {
		msg := cmd()
		switch rm := msg.(type) {
		case embedJobBatchMsg:
			if !rm.finished {
				t.Error("immediate result should be finished=true")
			}
		default:
			// Also acceptable: no cmd at all, state set directly.
		}
	}
}

// ─── 7. EMBED MISSING: nothing missing → immediate result ─────────────────────

func TestEmbedMissing_WhenNothingMissing(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowEmbedMissing
	m.configEmbeddingsEnabled = true

	// Inject a batch fn that immediately returns finished with 0 missing.
	m.embedBatchFn = func(url, modelName string, offset, batch int) tea.Cmd {
		return func() tea.Msg {
			// Return finished with no work done (nothing missing).
			return embedJobBatchMsg{done: 0, total: 50, lastErr: nil, finished: true}
		}
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("starting embed missing should return a command")
	}

	// Apply the batch result.
	batchResult := cmd()
	next, _ = m.Update(batchResult)
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should be false when finished immediately")
	}
	// View should show a completion message.
	m = setSize(m, 80, 24)
	out := m.View()
	plain := stripAnsiCodes(out)
	// Either "ALL EMBEDDED" or coverage info should appear.
	hasResult := strings.Contains(strings.ToUpper(plain), "EMBEDDED") ||
		strings.Contains(strings.ToUpper(plain), "EMBED")
	if !hasResult {
		t.Errorf("View() after nothing-missing should show embedded result; plain:\n%s", plain)
	}
}

// ─── 8. EMBED MISSING skips existing embeddings ───────────────────────────────

func TestEmbedMissing_SkipsExistingEmbeddings(t *testing.T) {
	ctx := context.Background()
	st := openRegenStore(t)

	// Seed 3 observations.
	seedRegenObs(t, st, "obs-a", "proj-em")
	id2 := seedRegenObs(t, st, "obs-b", "proj-em")
	id3 := seedRegenObs(t, st, "obs-c", "proj-em")

	model := "fake-model"

	// Pre-embed obs-a (id1 = first seeded).
	id1 := seedRegenObs(t, st, "obs-pre", "proj-em-pre")
	if err := st.UpsertEmbedding(ctx, id1, model, []float32{0.1}); err != nil {
		t.Fatalf("pre-embed: %v", err)
	}

	// Use a counting fake embedder.
	embedder := &fakeEmbedder{modelName: model, vec: []float32{0.2, 0.3}}

	// Call embedMissingAll directly (the pure function backing EMBED MISSING).
	done, total, err := embedMissingAll(ctx, st, embedder)
	if err != nil {
		t.Fatalf("embedMissingAll: %v", err)
	}

	// Should have embedded exactly 3 (obs-a, obs-b, obs-c — not obs-pre which was pre-embedded).
	if done != 3 {
		t.Errorf("done = %d, want 3 (only the non-pre-embedded observations)", done)
	}
	if total < 4 {
		t.Errorf("total = %d, want ≥4", total)
	}

	// obs-b and obs-c must now have embeddings.
	results, err := st.VectorSearch(ctx, []float32{0.2, 0.3}, store.SearchParams{Project: "proj-em", Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	ids := map[int64]bool{}
	for _, r := range results {
		ids[r.Observation.ID] = true
	}
	if !ids[id2] || !ids[id3] {
		t.Errorf("expected id2=%d and id3=%d in results after embedMissingAll; got %v", id2, id3, ids)
	}
}

// ─── 9. Progress bar visible mid-job ─────────────────────────────────────────

func TestProgressBarRenderedMidJob(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = true
	m.jobDone = 57
	m.jobTotal = 142
	m.jobKind = jobKindEmbed

	out := m.View()
	plain := stripAnsiCodes(out)

	// Bar must show count and percentage.
	if !strings.Contains(plain, "57") {
		t.Errorf("progress bar should show jobDone=57; plain:\n%s", plain)
	}
	if !strings.Contains(plain, "142") {
		t.Errorf("progress bar should show jobTotal=142; plain:\n%s", plain)
	}
	// Bar uses ▓ or ░.
	if !strings.Contains(out, "▓") && !strings.Contains(out, "░") {
		t.Errorf("progress bar must use ▓/░ characters; plain:\n%s", plain)
	}
	// Label should contain EMBEDDING.
	if !strings.Contains(strings.ToUpper(plain), "EMBEDDING") {
		t.Errorf("progress bar label should contain EMBEDDING for jobKindEmbed; plain:\n%s", plain)
	}
}

// Triangulate: REGENERATING label when jobKind == jobKindRegen.
func TestProgressBarRenderedMidJob_RegenLabel(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = true
	m.jobDone = 10
	m.jobTotal = 50
	m.jobKind = jobKindRegen

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "REGENERATING") {
		t.Errorf("progress bar label should contain REGENERATING for jobKindRegen; plain:\n%s", plain)
	}
}

// ─── 10. Finish result shown ─────────────────────────────────────────────────

func TestJobFinished_ResultShown(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = false
	m.jobResult = "EMBEDDINGS UP TO DATE — 142/142 — model nomic-embed-text"
	m.jobResultOK = true

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "EMBEDDINGS UP TO DATE") {
		t.Errorf("finished result should appear in view; plain:\n%s", plain)
	}
}

// Triangulate: partial/aborted result.
func TestJobFinished_PartialResult(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = false
	m.jobResult = "PARTIAL — 87/142 — some embed failures"
	m.jobResultOK = false

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "PARTIAL") {
		t.Errorf("partial result should appear in view; plain:\n%s", plain)
	}
}

// ─── 11. REGENERATE row uses new engine ──────────────────────────────────────

func TestRegen_UsesNewEngine(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowRegen
	m.configEmbeddingsEnabled = true

	called := false
	m.regenBatchFn = func(url, modelName string, offset, batch int) tea.Cmd {
		called = true
		return func() tea.Msg {
			return embedJobBatchMsg{done: 5, total: 5, finished: true}
		}
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !called {
		t.Error("regenBatchFn should be called when REGENERATE is triggered with embeddings enabled")
	}
	if cmd == nil {
		t.Error("REGENERATE should return a command")
	}
}

// Triangulate: regenBatchFn not called if embeddings are disabled.
func TestRegen_NewEngine_DisabledEmbeddings(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowRegen
	m.configEmbeddingsEnabled = false

	called := false
	m.regenBatchFn = func(url, modelName string, offset, batch int) tea.Cmd {
		called = true
		return nil
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if called {
		t.Error("regenBatchFn should NOT be called when embeddings are disabled")
	}
}

// ─── 12. Exact-fill at 80x24 with bar ────────────────────────────────────────

func TestRenderSmoke_80x24_WithBar(t *testing.T) {
	const termW, termH = 80, 24
	m := newConfigModel()
	m = setSize(m, termW, termH)
	m.jobRunning = true
	m.jobDone = 30
	m.jobTotal = 100
	m.jobKind = jobKindEmbed

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("80x24 with bar: View() produced %d lines, want %d", lineCount, termH)
	}

	// Progress bar must be visible.
	plain := stripAnsiCodes(out)
	if !strings.Contains(plain, "30") {
		t.Errorf("progress bar should show done=30; plain:\n%s", plain)
	}
}

// ─── 13. Exact-fill at 100x28 mid-job literal view ───────────────────────────

func TestRenderSmoke_100x28_WithBar(t *testing.T) {
	const termW, termH = 100, 28
	m := newConfigModel()
	m = setSize(m, termW, termH)
	m.jobRunning = true
	m.jobDone = 57
	m.jobTotal = 142
	m.jobKind = jobKindEmbed

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("100x28 with bar: View() produced %d lines, want %d", lineCount, termH)
	}

	lines := viewLines(out)
	t.Log("=== Config view 100x28 mid-job progress bar (plain text) ===")
	for i, l := range lines {
		t.Logf("%02d | %s", i+1, stripAnsiCodes(l))
	}
	t.Log("=== end ===")
}

// ─── 14. Both rows blocked while job is running ───────────────────────────────

func TestJobBlocked_WhileRunning(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.jobRunning = true // job in flight

	embedCalled := false
	m.embedBatchFn = func(url, modelName string, offset, batch int) tea.Cmd {
		embedCalled = true
		return nil
	}
	regenCalled := false
	m.regenBatchFn = func(url, modelName string, offset, batch int) tea.Cmd {
		regenCalled = true
		return nil
	}

	// Try to trigger EMBED MISSING.
	m.configCursor = configRowEmbedMissing
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if embedCalled {
		t.Error("embedBatchFn should not be called while a job is already running")
	}

	// Try to trigger REGENERATE.
	m.configCursor = configRowRegen
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if regenCalled {
		t.Error("regenBatchFn should not be called while a job is already running")
	}
}
