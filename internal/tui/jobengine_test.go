package tui

// jobengine_test.go — Strict TDD tests for the background embed job engine
// (jobengine.go) that replaced the old synchronous chained-batch engine.
//
// TDD cycle:
//  1. TestStartJob_SetsRunningStateAndIssuesCmd     — start → jobRunning, jobCancel, jobKind, a cmd is returned.
//  2. TestHandleJobEvent_ItemSuccessUpdatesCounters — EventItem (success) updates done/current, keeps running, re-subscribes.
//  3. TestHandleJobEvent_ItemFailureUpdatesLastErr  — EventItem (failure) sets jobLastErr, keeps running.
//  4. TestJobProgress_ViewNeverExceedsWidth         — View() while running fits at 80 and 120 columns.
//  5. TestHandleJobEvent_Done*                      — EventDone for each Status: result text, jobRunning false, retry affordance.
//  6. TestStopKey_CancelsRunningJobContext           — 's' calls jobCancel; ctx.Err() != nil afterwards.
//  7. TestStopKey_NoOpWhenNotRunning
//  8. TestRetryKey_*                                — 'r' after Partial/Aborted/Stopped starts KindEmbedMissing; no-op after Complete or while running.
//  9. TestEsc_KeepsProcessingJobEventsInBackground   — Esc switches view; a later jobEventMsg is still applied.
// 10. TestJobBlocked_WhileRunning                    — Enter on either row is a no-op while jobRunning.
// 11. TestChannelClose_StopsRedrainLoop              — ok=false stops the drain chain (no further cmd).
// 12. TestStartJobRunner_WritesEmbeddingsLog          — default runJobFn integration: real store + httptest Ollama.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── 1. Start job ─────────────────────────────────────────────────────────────

func TestStartJob_SetsRunningStateAndIssuesCmd(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.configCursor = configRowEmbedMissing

	ch := make(chan embedjob.Event)
	var capturedKind embedjob.Kind
	m.runJobFn = func(_ context.Context, kind embedjob.Kind) <-chan embedjob.Event {
		capturedKind = kind
		return ch
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if !m.jobRunning {
		t.Error("jobRunning should be true after starting a job")
	}
	if m.jobCancel == nil {
		t.Error("jobCancel should be set after starting a job")
	}
	if capturedKind != embedjob.KindEmbedMissing {
		t.Errorf("runJobFn kind = %v, want KindEmbedMissing", capturedKind)
	}
	if cmd == nil {
		t.Fatal("starting a job should issue a command batch (waitForJobEvent + spinner tick)")
	}
}

// ─── 2. EventItem success ─────────────────────────────────────────────────────

func TestHandleJobEvent_ItemSuccessUpdatesCounters(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)

	next, cmd := m.Update(jobEventMsg{
		ok: true,
		ev: embedjob.Event{Type: embedjob.EventItem, Title: "widget-1", Done: 5, Failed: 1, Total: 10},
	})
	m = next.(Model)

	if m.jobDone != 5 || m.jobTotal != 10 {
		t.Errorf("jobDone/jobTotal = %d/%d, want 5/10", m.jobDone, m.jobTotal)
	}
	if m.jobFailed != 1 {
		t.Errorf("jobFailed = %d, want 1", m.jobFailed)
	}
	if m.jobCurrent != "widget-1" {
		t.Errorf("jobCurrent = %q, want %q", m.jobCurrent, "widget-1")
	}
	if m.jobOK != 1 {
		t.Errorf("jobOK = %d, want 1 (one successful item)", m.jobOK)
	}
	if !m.jobRunning {
		t.Error("jobRunning should remain true after a mid-run item event")
	}
	if cmd == nil {
		t.Error("a non-terminal event must re-issue waitForJobEvent")
	}
}

// ─── 3. EventItem failure ─────────────────────────────────────────────────────

func TestHandleJobEvent_ItemFailureUpdatesLastErr(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)

	next, _ := m.Update(jobEventMsg{
		ok: true,
		ev: embedjob.Event{Type: embedjob.EventItem, Title: "widget-2", Err: errors.New("boom"), Done: 3, Failed: 2, Total: 10},
	})
	m = next.(Model)

	if m.jobLastErr != "boom" {
		t.Errorf("jobLastErr = %q, want %q", m.jobLastErr, "boom")
	}
	if m.jobOK != 0 {
		t.Errorf("jobOK = %d, want 0 (failed item must not count as a success)", m.jobOK)
	}
}

// EventWarmedUp flips jobWarmedUp and switches line 2 from "warming up" to "→ ".
func TestHandleJobEvent_WarmedUp(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.jobRunning = true
	m.jobKind = embedjob.KindEmbedMissing
	m.jobEvents = make(chan embedjob.Event)

	out := m.View()
	if !strings.Contains(strings.ToUpper(stripAnsiCodes(out)), "WARMING UP") {
		t.Fatalf("before EventWarmedUp, View() should show warming up; got:\n%s", stripAnsiCodes(out))
	}

	next, _ := m.Update(jobEventMsg{ok: true, ev: embedjob.Event{Type: embedjob.EventWarmedUp}})
	m = next.(Model)
	if !m.jobWarmedUp {
		t.Fatal("jobWarmedUp should be true after EventWarmedUp")
	}

	out = m.View()
	if strings.Contains(strings.ToUpper(stripAnsiCodes(out)), "WARMING UP") {
		t.Errorf("after EventWarmedUp, View() should no longer show warming up; got:\n%s", stripAnsiCodes(out))
	}
}

// ─── 4. Width safety ──────────────────────────────────────────────────────────

func TestJobProgress_ViewNeverExceedsWidth(t *testing.T) {
	for _, w := range []int{80, 120} {
		m := newConfigModel()
		m = setSize(m, w, 30)
		m.jobRunning = true
		m.jobKind = embedjob.KindEmbedMissing
		m.jobStarted = time.Now().Add(-90 * time.Second)
		m.jobDone = 120
		m.jobTotal = 392
		m.jobOK = 118
		m.jobFailed = 2
		m.jobWarmedUp = true
		m.jobCurrent = strings.Repeat("very-long-observation-title-", 5)
		m.jobLastErr = strings.Repeat("some very long transient network error message ", 3)

		out := m.View()
		for i, line := range viewLines(out) {
			if got := visibleWidth(line); got > w {
				t.Errorf("width=%d: line %d exceeds terminal width: got %d, line=%q", w, i, got, stripAnsiCodes(line))
			}
		}
	}
}

// ─── 5. EventDone per status ──────────────────────────────────────────────────

func TestHandleJobEvent_DoneComplete(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)
	m.jobCancel = func() {}

	next, _ := m.Update(jobEventMsg{ok: true, ev: embedjob.Event{Type: embedjob.EventDone, Summary: &embedjob.Summary{
		Status: embedjob.StatusComplete, Done: 392, Total: 392, Elapsed: 72 * time.Second,
	}}})
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should be false after EventDone")
	}
	if !m.jobResultOK {
		t.Error("jobResultOK should be true for StatusComplete")
	}
	if !strings.Contains(m.jobResult, "COMPLETE") || !strings.Contains(m.jobResult, "392/392") {
		t.Errorf("jobResult = %q, want COMPLETE with 392/392", m.jobResult)
	}
	if m.jobSummary == nil || m.jobSummary.Status != embedjob.StatusComplete {
		t.Error("jobSummary should be stored with StatusComplete")
	}
}

func TestHandleJobEvent_DonePartial(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)
	m.jobCancel = func() {}

	next, _ := m.Update(jobEventMsg{ok: true, ev: embedjob.Event{Type: embedjob.EventDone, Summary: &embedjob.Summary{
		Status: embedjob.StatusPartial, Done: 380, Total: 392, Failed: 12, LogPath: "/tmp/embeddings.log",
	}}})
	m = next.(Model)

	if m.jobResultOK {
		t.Error("jobResultOK should be false for StatusPartial")
	}
	if !strings.Contains(m.jobResult, "PARTIAL") || !strings.Contains(m.jobResult, "380/392") ||
		!strings.Contains(m.jobResult, "12 failed") || !strings.Contains(m.jobResult, "[R] RETRY") ||
		!strings.Contains(m.jobResult, "/tmp/embeddings.log") {
		t.Errorf("jobResult = %q, missing expected PARTIAL fields", m.jobResult)
	}
}

func TestHandleJobEvent_DoneAborted(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)
	m.jobCancel = func() {}

	next, _ := m.Update(jobEventMsg{ok: true, ev: embedjob.Event{Type: embedjob.EventDone, Summary: &embedjob.Summary{
		Status: embedjob.StatusAborted, LastErr: errors.New("consecutive failures: boom"), LogPath: "/tmp/embeddings.log",
	}}})
	m = next.(Model)

	if m.jobResultOK {
		t.Error("jobResultOK should be false for StatusAborted")
	}
	if !strings.Contains(m.jobResult, "ABORTED") || !strings.Contains(m.jobResult, "[R] RETRY") {
		t.Errorf("jobResult = %q, missing ABORTED / [R] RETRY", m.jobResult)
	}
}

func TestHandleJobEvent_DoneStopped(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)
	m.jobCancel = func() {}

	next, _ := m.Update(jobEventMsg{ok: true, ev: embedjob.Event{Type: embedjob.EventDone, Summary: &embedjob.Summary{
		Status: embedjob.StatusStopped, Done: 120, Total: 392,
	}}})
	m = next.(Model)

	if m.jobResultOK {
		t.Error("jobResultOK should be false for StatusStopped")
	}
	if !strings.Contains(m.jobResult, "STOPPED") || !strings.Contains(m.jobResult, "120/392") ||
		!strings.Contains(m.jobResult, "[R] RESUME") {
		t.Errorf("jobResult = %q, missing STOPPED / 120/392 / [R] RESUME", m.jobResult)
	}
}

// Footer/status show [R] RETRY only when the last summary isn't Complete.
func TestFooter_RetryHintOnlyWhenApplicable(t *testing.T) {
	complete := newConfigModel()
	complete.jobSummary = &embedjob.Summary{Status: embedjob.StatusComplete}
	if strings.Contains(complete.renderFooter(), "RETRY") {
		t.Error("footer should not show [R] RETRY after a Complete job")
	}

	partial := newConfigModel()
	partial.jobSummary = &embedjob.Summary{Status: embedjob.StatusPartial}
	if !strings.Contains(partial.renderFooter(), "RETRY") {
		t.Error("footer should show [R] RETRY after a Partial job")
	}

	running := newConfigModel()
	running.jobRunning = true
	if !strings.Contains(running.renderFooter(), "STOP") {
		t.Error("footer should show [S] STOP while a job is running")
	}
}

// ─── 6-7. STOP key ────────────────────────────────────────────────────────────

func TestStopKey_CancelsRunningJobContext(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.configCursor = configRowEmbedMissing

	var capturedCtx context.Context
	m.runJobFn = func(ctx context.Context, _ embedjob.Kind) <-chan embedjob.Event {
		capturedCtx = ctx
		return make(chan embedjob.Event)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if capturedCtx.Err() != nil {
		t.Fatal("ctx should not be cancelled right after starting the job")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)

	if capturedCtx.Err() == nil {
		t.Error("'s' should cancel the running job's context")
	}
}

func TestStopKey_NoOpWhenNotRunning(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = false

	// Must not panic even though jobCancel is nil.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)
	if m.jobRunning {
		t.Error("'s' with no job running should not start one")
	}
	_ = cmd
}

// ─── 8. RETRY key ─────────────────────────────────────────────────────────────

func TestRetryKey_AfterPartialStartsEmbedMissing(t *testing.T) {
	m := newConfigModel()
	m.jobSummary = &embedjob.Summary{Status: embedjob.StatusPartial}

	var capturedKind embedjob.Kind
	called := false
	m.runJobFn = func(_ context.Context, kind embedjob.Kind) <-chan embedjob.Event {
		called = true
		capturedKind = kind
		return make(chan embedjob.Event)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(Model)

	if !called {
		t.Fatal("'r' after Partial should start a new job")
	}
	if capturedKind != embedjob.KindEmbedMissing {
		t.Errorf("retry kind = %v, want KindEmbedMissing", capturedKind)
	}
	if !m.jobRunning || cmd == nil {
		t.Error("retry should set jobRunning and issue a command")
	}
}

func TestRetryKey_AfterCompleteIsNoOp(t *testing.T) {
	m := newConfigModel()
	m.jobSummary = &embedjob.Summary{Status: embedjob.StatusComplete}

	called := false
	m.runJobFn = func(_ context.Context, _ embedjob.Kind) <-chan embedjob.Event {
		called = true
		return make(chan embedjob.Event)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if called {
		t.Error("'r' after a Complete job should be a no-op")
	}
}

func TestRetryKey_WhileRunningIsNoOp(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobSummary = &embedjob.Summary{Status: embedjob.StatusPartial}

	called := false
	m.runJobFn = func(_ context.Context, _ embedjob.Kind) <-chan embedjob.Event {
		called = true
		return make(chan embedjob.Event)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if called {
		t.Error("'r' while a job is already running should be a no-op")
	}
}

func TestRetryKey_NoPriorJobIsNoOp(t *testing.T) {
	m := newConfigModel()
	// jobSummary is nil (no job has ever finished).
	called := false
	m.runJobFn = func(_ context.Context, _ embedjob.Kind) <-chan embedjob.Event {
		called = true
		return make(chan embedjob.Event)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if called {
		t.Error("'r' with no prior job summary should be a no-op")
	}
}

// ─── 9. Esc keeps processing job events in background ────────────────────────

func TestEsc_KeepsProcessingJobEventsInBackground(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true
	m.jobEvents = make(chan embedjob.Event)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.view != viewProjects {
		t.Fatal("Esc should switch to viewProjects")
	}
	if !m.jobRunning {
		t.Fatal("Esc must not stop the running job")
	}

	// A later jobEventMsg must still be applied even though view != viewConfig.
	next, _ = m.Update(jobEventMsg{ok: true, ev: embedjob.Event{Type: embedjob.EventItem, Title: "bg-item", Done: 1, Total: 5}})
	m = next.(Model)

	if m.jobCurrent != "bg-item" {
		t.Errorf("job events should still update the model while away from viewConfig; jobCurrent = %q", m.jobCurrent)
	}
}

// ─── 10. Blocked while running ────────────────────────────────────────────────

func TestJobBlocked_WhileRunning(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.jobRunning = true

	called := false
	m.runJobFn = func(context.Context, embedjob.Kind) <-chan embedjob.Event {
		called = true
		return make(chan embedjob.Event)
	}

	m.configCursor = configRowEmbedMissing
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if called {
		t.Error("runJobFn should not be called for EMBED MISSING while a job is already running")
	}

	m.configCursor = configRowRegen
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if called {
		t.Error("runJobFn should not be called for REGENERATE while a job is already running")
	}
}

// ─── 11. Channel close stops the drain loop ──────────────────────────────────

func TestChannelClose_StopsRedrainLoop(t *testing.T) {
	m := newConfigModel()
	m.jobRunning = true

	_, cmd := m.Update(jobEventMsg{ok: false})
	if cmd != nil {
		t.Error("a closed channel (ok=false) must not re-issue waitForJobEvent")
	}
}

// ─── 12. startJobRunner integration ──────────────────────────────────────────

// TestStartJobRunner_WritesEmbeddingsLog exercises the production runJobFn
// end to end against a real temporary store and an httptest Ollama stand-in,
// and verifies it opens the embeddings joblog under dataDir.
func TestStartJobRunner_WritesEmbeddingsLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID: "job-sess-1", Project: "proj-job", Directory: "/job",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "job-sess-1", Type: "manual", Title: "job-obs-1", Content: "hello", Project: "proj-job", Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	m := newModel()
	m.store = st
	m.dataDir = dataDir
	m.configOllamaURL = srv.URL
	m.configModel = "fake-model"

	ch := m.startJobRunner(context.Background(), embedjob.KindEmbedMissing)

	var lastSummary *embedjob.Summary
	for ev := range ch {
		if ev.Type == embedjob.EventDone {
			lastSummary = ev.Summary
		}
	}
	if lastSummary == nil {
		t.Fatal("startJobRunner: no terminal EventDone observed")
	}
	if lastSummary.Status != embedjob.StatusComplete {
		t.Errorf("lastSummary.Status = %v, want StatusComplete", lastSummary.Status)
	}

	logPath := filepath.Join(dataDir, "logs", "embeddings.log")
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected embeddings log at %s: %v", logPath, err)
	}
	logContent := string(logBytes)
	if !strings.Contains(logContent, "embedjob start") {
		t.Errorf("embeddings.log should contain %q; content:\n%s", "embedjob start", logContent)
	}
	if !strings.Contains(logContent, "status=complete") {
		t.Errorf("embeddings.log should contain %q; content:\n%s", "status=complete", logContent)
	}
}

// ─── 13. 'q' cancels a running job before quitting ───────────────────────────
//
// cli_dash.go defers st.Close() right after tui.RunWithOptions returns, so if
// tea.Quit is issued while a job's goroutine is still mid-query, that
// goroutine can race a closed *sql.DB. Quitting must cancel any running job
// first so embedjob.Run observes ctx.Done() and stops before the program
// exits.

func TestQuitKey_CancelsRunningJob_ConfigView(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.configCursor = configRowEmbedMissing

	var capturedCtx context.Context
	m.runJobFn = func(ctx context.Context, _ embedjob.Kind) <-chan embedjob.Event {
		capturedCtx = ctx
		return make(chan embedjob.Event)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if capturedCtx.Err() != nil {
		t.Fatal("ctx should not be cancelled right after starting the job")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if capturedCtx.Err() == nil {
		t.Error("'q' in the config view should cancel the running job's context before quitting")
	}
	if cmd == nil {
		t.Error("'q' should still return the tea.Quit command")
	}
}

func TestQuitKey_CancelsRunningJob_ProjectsView(t *testing.T) {
	m := newModel()
	m.view = viewProjects
	m.projects = makeProjectSummaries()
	m.jobRunning = true

	var cancelled bool
	m.jobCancel = func() { cancelled = true }

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !cancelled {
		t.Error("'q' in the projects view should cancel the running job before quitting")
	}
	if cmd == nil {
		t.Error("'q' should still return the tea.Quit command")
	}
}

func TestQuitKey_NoOpCancelWhenNoJobRunning(t *testing.T) {
	m := newModel()
	m.view = viewProjects
	m.jobRunning = false
	m.jobCancel = nil

	// Must not panic even though jobCancel is nil.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("'q' should still return the tea.Quit command when no job is running")
	}
}

// visibleWidth measures the visible width of a (possibly ANSI-styled) line.
func visibleWidth(s string) int {
	return len([]rune(stripAnsiCodes(s)))
}
