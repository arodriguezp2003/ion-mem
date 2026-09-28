package tui

// embed_missing_test.go — pure-helper and row-layout tests around the
// EMBED MISSING / REGENERATE rows. The job-engine behaviour itself (start,
// event handling, cancel, retry, rendering while running) lives in
// jobengine_test.go — this file keeps only what does not depend on it.
//
// TDD cycle:
//  1. TestProgressBar_PureHelper     — renderProgressBar table-driven: 0%, 40%, 100%, total=0 guard.
//  2. TestProgressBar_FilledAndEmpty — 40% bar has both filled and empty blocks.
//  3. TestEmbedMissing_RowConstant   — configRowEmbedMissing/configRowRegen/configRowCount.
//  4. TestEmbedMissing_CursorReachRegen / CursorClampAt5 — cursor navigation to the last rows.
//  5. TestProgressBarRenderedMidJob / RegenLabel — bar + label appear in View() while running.
//  6. TestJobFinished_ResultShown / PartialResult — finished result line appears in View().

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
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

// ─── 2. Row constants ──────────────────────────────────────────────────────────

func TestEmbedMissing_RowConstant(t *testing.T) {
	if configRowEmbedMissing != 5 {
		t.Errorf("configRowEmbedMissing = %d, want 5", configRowEmbedMissing)
	}
	if configRowRegen != 6 {
		t.Errorf("configRowRegen = %d, want 6", configRowRegen)
	}
	if configRowCount != 7 {
		t.Errorf("configRowCount = %d, want 7", configRowCount)
	}
}

// ─── 3. Cursor can reach the last row (REGENERATE) ────────────────────────────

func TestEmbedMissing_CursorReachRegen(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0
	for i := 0; i < configRowRegen; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	if m.configCursor != configRowRegen {
		t.Errorf("after %d×↓ from row 0, configCursor = %d, want %d (configRowRegen)", configRowRegen, m.configCursor, configRowRegen)
	}
}

// Triangulate: cursor stops at the last row.
func TestEmbedMissing_CursorClampAtLast(t *testing.T) {
	m := newConfigModel()
	for i := 0; i < 20; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	if m.configCursor != configRowCount-1 {
		t.Errorf("cursor should clamp at %d (last row), got %d", configRowCount-1, m.configCursor)
	}
}

// ─── 4. Progress bar visible mid-job ─────────────────────────────────────────

func TestProgressBarRenderedMidJob(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = true
	m.jobStarted = time.Now()
	m.jobDone = 57
	m.jobTotal = 142
	m.jobKind = embedjob.KindEmbedMissing

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
		t.Errorf("progress bar label should contain EMBEDDING for KindEmbedMissing; plain:\n%s", plain)
	}
}

// Triangulate: REGENERATING label when jobKind == embedjob.KindRegenerate.
func TestProgressBarRenderedMidJob_RegenLabel(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = true
	m.jobStarted = time.Now()
	m.jobDone = 10
	m.jobTotal = 50
	m.jobKind = embedjob.KindRegenerate

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "REGENERATING") {
		t.Errorf("progress bar label should contain REGENERATING for KindRegenerate; plain:\n%s", plain)
	}
}

// ─── 5. Finish result shown ─────────────────────────────────────────────────

func TestJobFinished_ResultShown(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = false
	m.jobResult = "COMPLETE — 142/142 — 1m12s"
	m.jobResultOK = true

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "COMPLETE") {
		t.Errorf("finished result should appear in view; plain:\n%s", plain)
	}
}

// Triangulate: partial/aborted result.
func TestJobFinished_PartialResult(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = false
	m.jobResult = "PARTIAL — 87/142 — 5 failed — [R] RETRY — log: /tmp/embeddings.log"
	m.jobResultOK = false

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "PARTIAL") {
		t.Errorf("partial result should appear in view; plain:\n%s", plain)
	}
}

// ─── 6. Exact-fill still holds with the live progress block ─────────────────

func TestRenderSmoke_80x24_WithBar(t *testing.T) {
	const termW, termH = 80, 24
	m := newConfigModel()
	m = setSize(m, termW, termH)
	m.jobRunning = true
	m.jobStarted = time.Now()
	m.jobDone = 30
	m.jobTotal = 100
	m.jobKind = embedjob.KindEmbedMissing

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

func TestRenderSmoke_100x28_WithBar(t *testing.T) {
	const termW, termH = 100, 28
	m := newConfigModel()
	m = setSize(m, termW, termH)
	m.jobRunning = true
	m.jobStarted = time.Now()
	m.jobDone = 57
	m.jobTotal = 142
	m.jobKind = embedjob.KindEmbedMissing

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("100x28 with bar: View() produced %d lines, want %d", lineCount, termH)
	}

	lines := viewLines(out)
	t.Log("=== Config view 100x28 mid-job progress (plain text) ===")
	for i, l := range lines {
		t.Logf("%02d | %s", i+1, stripAnsiCodes(l))
	}
	t.Log("=== end ===")
}
