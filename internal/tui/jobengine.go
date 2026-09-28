package tui

// jobengine.go — the background embed job engine backing the config view's
// EMBED MISSING and REGENERATE actions.
//
// Design: a job runs as a real cancellable goroutine via embedjob.Run
// (wrapped by runJobFn / startJobRunner), not as a chain of synchronous
// tea.Cmd batches. The TUI subscribes to the returned Event channel with
// waitForJobEvent and keeps re-issuing that command — regardless of which
// view is active — until the channel closes, per embedjob's "always drain"
// contract. This is what makes the UI stay responsive while a job runs: each
// tea.Cmd only waits for the NEXT event, never for the whole job.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
	"github.com/arodriguezp2003/ion-mem/internal/joblog"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// jobBatchSize is the number of observations fetched per page by the
// underlying embedjob.Run loop.
const jobBatchSize = 25

// waitForJobEvent returns a tea.Cmd that blocks on a single receive from ch
// and reports the result as a jobEventMsg. ok is false once ch is closed.
//
// No stale-channel generation guard is needed here: each waitForJobEvent
// closure captures ch by value (from m.jobEvents at the time it was issued),
// and embedjob.Run guarantees exactly one EventDone followed by a close on
// that same channel — so even a "stale" cmd left over from a superseded job
// can only ever deliver that job's own terminal event, or the no-op close
// signal (ok=false) afterwards. There is no scenario where it observes a
// later job's events or blocks forever.
func waitForJobEvent(ch <-chan embedjob.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return jobEventMsg{ev: ev, ok: ok}
	}
}

// cancelIfJobRunning cancels the running job's context, if any. It must be
// called before returning tea.Quit from any key handler: cli_dash.go defers
// st.Close() immediately after tui.RunWithOptions returns, so if a job
// goroutine is still mid-query when the program exits, it can race a closed
// *sql.DB. Cancelling first makes embedjob.Run observe ctx.Done() and stop
// before the process tears down the store.
func (m Model) cancelIfJobRunning() {
	if m.jobRunning && m.jobCancel != nil {
		m.jobCancel()
	}
}

// startJob resets the job-progress fields and starts a new job of the given
// kind via runJobFn (or startJobRunner in production), returning the batch of
// commands that subscribes to its event channel and kicks off the spinner.
func (m Model) startJob(kind embedjob.Kind) (Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())

	runFn := m.runJobFn
	if runFn == nil {
		runFn = m.startJobRunner
	}
	ch := runFn(ctx, kind)

	m.jobRunning = true
	m.jobKind = kind
	m.jobCancel = cancel
	m.jobEvents = ch
	m.jobStarted = time.Now()
	m.jobDone = 0
	m.jobFailed = 0
	m.jobTotal = 0
	m.jobOK = 0
	m.jobCurrent = ""
	m.jobWarmedUp = false
	m.jobLastErr = ""
	m.jobLogPath = ""
	m.jobResult = ""
	m.jobResultOK = false

	return m, tea.Batch(waitForJobEvent(ch), m.spinner.Tick)
}

// handleJobEvent applies one jobEventMsg to the model and, unless the
// channel has closed, re-issues waitForJobEvent so the caller keeps draining
// it (required by embedjob's channel contract) until it does close.
func (m Model) handleJobEvent(msg jobEventMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		// Channel closed: the job goroutine is fully done. Nothing left to drain.
		return m, nil
	}

	switch msg.ev.Type {
	case embedjob.EventWarmedUp:
		m.jobWarmedUp = true

	case embedjob.EventItem:
		m.jobDone = msg.ev.Done
		m.jobFailed = msg.ev.Failed
		m.jobTotal = msg.ev.Total
		m.jobCurrent = msg.ev.Title
		if msg.ev.Err != nil {
			m.jobLastErr = msg.ev.Err.Error()
		} else {
			m.jobOK++
		}

	case embedjob.EventDone:
		m.jobRunning = false
		if msg.ev.Summary != nil {
			m.jobSummary = msg.ev.Summary
			m.jobDone = msg.ev.Summary.Done
			m.jobFailed = msg.ev.Summary.Failed
			m.jobTotal = msg.ev.Summary.Total
			m.jobLogPath = msg.ev.Summary.LogPath
			if msg.ev.Summary.LastErr != nil {
				m.jobLastErr = msg.ev.Summary.LastErr.Error()
			}
			m.jobResult, m.jobResultOK = buildJobResultText(msg.ev.Summary)
		}
		if m.jobCancel != nil {
			m.jobCancel()
			m.jobCancel = nil
		}
	}

	return m, waitForJobEvent(m.jobEvents)
}

// buildJobResultText renders the terminal result line for a finished job,
// keyed by Summary.Status, and reports whether it should use the OK style.
func buildJobResultText(s *embedjob.Summary) (string, bool) {
	if s == nil {
		return "", false
	}
	switch s.Status {
	case embedjob.StatusComplete:
		return fmt.Sprintf("COMPLETE — %d/%d — %s", s.Done, s.Total, s.Elapsed.Round(time.Second)), true

	case embedjob.StatusPartial:
		return fmt.Sprintf("PARTIAL — %d/%d — %d failed — [R] RETRY — log: %s",
			s.Done, s.Total, s.Failed, s.LogPath), false

	case embedjob.StatusAborted:
		errStr := ""
		if s.LastErr != nil {
			errStr = s.LastErr.Error()
		}
		return fmt.Sprintf("ABORTED — %s — [R] RETRY — log: %s", truncStr(errStr, 60), s.LogPath), false

	case embedjob.StatusStopped:
		return fmt.Sprintf("STOPPED — %d/%d — [R] RESUME", s.Done, s.Total), false

	default:
		return "UNKNOWN JOB STATUS", false
	}
}

// startJobRunner is the production runJobFn: it builds an Ollama embedder
// from the currently loaded settings, opens the embeddings joblog under
// m.dataDir, and delegates the fetch/embed/upsert loop to embedjob.Run. The
// returned channel forwards every event from embedjob.Run and closes the
// joblog only after that inner channel itself closes.
func (m Model) startJobRunner(ctx context.Context, kind embedjob.Kind) <-chan embedjob.Event {
	if m.store == nil {
		out := make(chan embedjob.Event, 2)
		out <- embedjob.Event{Type: embedjob.EventStarted}
		out <- embedjob.Event{
			Type: embedjob.EventDone,
			Summary: &embedjob.Summary{
				Status:  embedjob.StatusAborted,
				LastErr: fmt.Errorf("tui: store unavailable"),
			},
		}
		close(out)
		return out
	}

	client := embed.DefaultClient(m.configOllamaURL)
	embedder := embed.NewOllamaEmbedder(client, m.configModel)

	verbose := joblog.ResolveVerbose(
		m.store.SettingOrDefault(ctx, store.SettingLogVerbose, "false"),
		os.Getenv("ION_MEM_VERBOSE"),
		nil,
	)
	lg, err := joblog.Open(m.dataDir, "embeddings", verbose)
	if err != nil {
		lg = joblog.Nop()
	}

	inner := embedjob.Run(ctx, m.store, embedder, lg, embedjob.Config{
		Kind:    kind,
		Batch:   jobBatchSize,
		Retries: embedjob.DefaultRetries,
		WarmUp:  true,
	})

	out := make(chan embedjob.Event, eventForwardBuffer)
	go func() {
		defer close(out)
		defer lg.Close()
		for ev := range inner {
			out <- ev
		}
	}()
	return out
}

// eventForwardBuffer is the buffer size of the channel startJobRunner uses to
// forward embedjob.Run's events to the TUI.
const eventForwardBuffer = 8

// ─── progress rendering ───────────────────────────────────────────────────────

// minSuccessesForRate is the number of successful items required before the
// rate/eta estimate is shown; below this the estimate is too noisy to trust.
const minSuccessesForRate = 3

// renderJobProgress renders the 2-3 line live progress block shown while a
// job is running: a spinner+bar+counters line, a current-item line, and an
// optional last-error line. Every line is clamped to fit within width.
func (m Model) renderJobProgress(rowIndent string, width int) string {
	var b strings.Builder

	// Each rendered line is prefixed with rowIndent, so the clamp budget for
	// the line's own content must leave room for it.
	innerWidth := width - len(rowIndent)
	if innerWidth < 1 {
		innerWidth = 1
	}

	label := "EMBEDDING"
	if m.jobKind == embedjob.KindRegenerate {
		label = "REGENERATING"
	}

	bar := renderProgressBar(m.jobDone, m.jobTotal, progressBarWidth)
	elapsed := time.Since(m.jobStarted).Round(time.Second)

	line1 := fmt.Sprintf("%s %s  %s  ok %d  fail %d  elapsed %s",
		m.spinner.View(), label, bar, m.jobOK, m.jobFailed, elapsed)

	if m.jobOK >= minSuccessesForRate && elapsed > 0 {
		rate := float64(m.jobOK) / elapsed.Seconds()
		if rate > 0 {
			withRate := line1 + fmt.Sprintf("  ~%.1f/s", rate)
			remaining := m.jobTotal - m.jobDone
			if remaining > 0 {
				eta := time.Duration(float64(remaining)/rate) * time.Second
				withRate += fmt.Sprintf("  eta %s", eta.Round(time.Second))
			}
			line1 = withRate
		}
	}
	b.WriteString(rowIndent + clampLineWidth(line1, innerWidth) + "\n")

	line2 := "warming up model…"
	if m.jobWarmedUp {
		line2 = "→ " + m.jobCurrent
	}
	b.WriteString(rowIndent + clampLineWidth(configTestingStyle.Render(line2), innerWidth) + "\n")

	if m.jobLastErr != "" {
		line3 := "last error: " + m.jobLastErr
		b.WriteString(rowIndent + clampLineWidth(configDangerStyle.Render(line3), innerWidth) + "\n")
	}

	return b.String()
}

// clampLineWidth ensures s never renders wider than width columns. When the
// styled string already fits, it is returned unchanged; otherwise it falls
// back to a plain (unstyled) hard truncation, since truncating a string that
// contains ANSI escapes byte-wise could corrupt them.
func clampLineWidth(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return truncStr(stripAnsiCodes(s), width)
}
