package tui

// config.go — Config view: embeddings toggle, Ollama URL, model, connection test,
// embed-missing (incremental backfill), regenerate.
//
// Design:
//   - Entry: 'c' from projects view.
//   - Breadcrumb: PROJECTS // CONFIG.
//   - Six settings rows navigated with ↑↓:
//       0  EMBEDDINGS             [ON] / [OFF] — Enter/Space toggles, persists immediately.
//       1  OLLAMA URL             <url>        — Enter opens inline edit.
//       2  MODEL                  <model>      — Enter opens inline edit.
//       3  TEST CONNECTION                     — Enter runs async probe.
//       4  EMBED MISSING                       — Enter backfills un-embedded observations.
//       5  REGENERATE EMBEDDINGS               — Enter wipes then re-embeds all.
//   - Esc closes edit or returns to projects.
//   - Probe result shown in status area: OK (accent) or error (danger).
//   - Job (embed/regen) progress shown as a retro bar while running; result when done.
//   - Exact-fill + wide centering consistent with the rest of the TUI.
//
// Batch engine concurrency note:
//   A job (EMBED MISSING or REGENERATE) processes ONE batch per tea.Cmd. Progress
//   messages keep flowing even if the user navigates away from the config view —
//   only the visual progress bar is suppressed in other views. This keeps the engine
//   simple (no goroutines) and avoids stale-result races.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ionix/ion-mem/internal/embed"
	"github.com/ionix/ion-mem/internal/store"
)

// ─── constants ────────────────────────────────────────────────────────────────

// configLabelWidth is the fixed column width for config row labels.
// "REGENERATE EMBEDDINGS" (21 chars) is the longest label — use 22 to leave
// one space of padding.
const configLabelWidth = 22

// embedBatchSize is the number of observations fetched per chained batch.
// 25 keeps each tea.Cmd short enough for a live progress bar.
const embedBatchSize = 25

// progressBarWidth is the number of block-character columns in the progress bar
// fill region (excluding label and fraction text).
const progressBarWidth = 24

// ─── styles (config-specific) ────────────────────────────────────────────────

var (
	configLabelStyle = lipgloss.NewStyle().Foreground(defaultTheme.dim).Width(configLabelWidth)
	configValueStyle = lipgloss.NewStyle().Bold(true)
	configSelStyle   = lipgloss.NewStyle().
				Bold(true).
				Background(defaultTheme.accent).
				Foreground(lipgloss.AdaptiveColor{Dark: "#1A0407", Light: "#FFFFFF"})
	configOKStyle      = lipgloss.NewStyle().Foreground(defaultTheme.accent).Bold(true)
	configDangerStyle  = lipgloss.NewStyle().Foreground(defaultTheme.danger).Bold(true)
	configTestingStyle = lipgloss.NewStyle().Foreground(defaultTheme.dim)

	// Progress bar block styles.
	barFilledStyle = lipgloss.NewStyle().Foreground(defaultTheme.accent)
	barEmptyStyle  = lipgloss.NewStyle().Foreground(defaultTheme.muted)
)

// ─── Key handler ─────────────────────────────────────────────────────────────

func (m Model) handleKeyConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If inline edit is open, handle it first.
	if m.configEditing {
		switch {
		case msg.Type == tea.KeyEsc:
			// Cancel: restore original value.
			switch m.configCursor {
			case configRowOllamaURL:
				m.configOllamaURL = m.configEditOrig
			case configRowModel:
				m.configModel = m.configEditOrig
			}
			m.configEditing = false
			m.configInput.Blur()
			return m, nil

		case msg.Type == tea.KeyEnter:
			// Save: persist the edited value.
			val := strings.TrimSpace(m.configInput.Value())
			var key string
			switch m.configCursor {
			case configRowOllamaURL:
				if val == "" {
					val = m.configEditOrig
				}
				m.configOllamaURL = val
				key = store.SettingOllamaURL
			case configRowModel:
				if val == "" {
					val = m.configEditOrig
				}
				m.configModel = val
				key = store.SettingEmbeddingsModel
			}
			m.configEditing = false
			m.configInput.Blur()
			return m, m.saveConfigSetting(key, val)

		default:
			var cmd tea.Cmd
			m.configInput, cmd = m.configInput.Update(msg)
			return m, cmd
		}
	}

	switch {
	case msg.Type == tea.KeyEsc:
		m.view = viewProjects
		m.configTesting = false
		return m, nil

	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 'q') || msg.Type == tea.KeyCtrlC:
		return m, tea.Quit

	case msg.Type == tea.KeyUp || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'k'):
		if m.configCursor > 0 {
			m.configCursor--
		}

	case msg.Type == tea.KeyDown || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'j'):
		if m.configCursor < configRowCount-1 {
			m.configCursor++
		}

	case msg.Type == tea.KeyEnter || msg.Type == tea.KeySpace:
		return m.handleConfigAction(msg.Type == tea.KeySpace)
	}

	return m, nil
}

// handleConfigAction dispatches the action for the currently selected config row.
// spaceKey is true when the trigger was Space (only relevant for EMBEDDINGS toggle).
func (m Model) handleConfigAction(spaceKey bool) (tea.Model, tea.Cmd) {
	switch m.configCursor {
	case configRowEmbeddings:
		m.configEmbeddingsEnabled = !m.configEmbeddingsEnabled
		val := "false"
		if m.configEmbeddingsEnabled {
			val = "true"
		}
		return m, m.saveConfigSetting(store.SettingEmbeddingsEnabled, val)

	case configRowOllamaURL:
		if spaceKey {
			return m, nil
		}
		m.configEditOrig = m.configOllamaURL
		m.configEditing = true
		m.configInput.Reset()
		m.configInput.SetValue(m.configOllamaURL)
		m.configInput.Focus()
		return m, textinput.Blink

	case configRowModel:
		if spaceKey {
			return m, nil
		}
		m.configEditOrig = m.configModel
		m.configEditing = true
		m.configInput.Reset()
		m.configInput.SetValue(m.configModel)
		m.configInput.Focus()
		return m, textinput.Blink

	case configRowTestConn:
		if spaceKey {
			return m, nil
		}
		m.configTesting = true
		m.configTestResult = ""
		probeFn := m.probeFn
		if probeFn == nil {
			probeFn = defaultProbeFn
		}
		return m, probeFn(m.configOllamaURL, m.configModel)

	case configRowEmbedMissing:
		if spaceKey {
			return m, nil
		}
		// Embeddings must be enabled.
		if !m.configEmbeddingsEnabled {
			return m, func() tea.Msg {
				return embedJobBatchMsg{
					done:     0,
					total:    0,
					finished: true,
					lastErr:  fmt.Errorf("EMBEDDINGS ARE OFF — enable them first"),
				}
			}
		}
		// Block re-trigger while a job is already running.
		if m.jobRunning {
			return m, nil
		}
		// Start the EMBED MISSING job.
		m.jobRunning = true
		m.jobDone = 0
		m.jobTotal = 0
		m.jobKind = jobKindEmbed
		m.jobResult = ""
		batchFn := m.embedBatchFn
		if batchFn == nil {
			batchFn = m.makeDefaultEmbedBatchFn()
		}
		return m, batchFn(m.configOllamaURL, m.configModel, 0, embedBatchSize)

	case configRowRegen:
		if spaceKey {
			return m, nil
		}
		// If embeddings are disabled, show a danger message immediately without
		// starting any async work.
		if !m.configEmbeddingsEnabled {
			return m, func() tea.Msg {
				return configRegenResultMsg{
					ok:   false,
					info: "EMBEDDINGS ARE OFF — enable them first",
				}
			}
		}
		// Block re-trigger while a job is already running (new engine) or the
		// legacy configRegenerating flag is set (backwards compat with old stubs).
		if m.jobRunning || m.configRegenerating {
			return m, nil
		}
		// Legacy single-shot path ONLY when a test explicitly stubs regenFn.
		if m.regenFn != nil {
			m.configRegenerating = true
			m.configRegenResult = ""
			return m, m.regenFn(m.configOllamaURL, m.configModel)
		}
		// Production path: chained-batch engine with live progress bar,
		// same as EMBED MISSING (falls back to the default batch fn when
		// no test injection is present).
		batchFn := m.regenBatchFn
		if batchFn == nil {
			batchFn = m.makeDefaultRegenBatchFn()
		}
		m.jobRunning = true
		m.jobDone = 0
		m.jobTotal = 0
		m.jobKind = jobKindRegen
		m.jobResult = ""
		m.configRegenerating = true
		m.configRegenResult = ""
		return m, batchFn(m.configOllamaURL, m.configModel, 0, embedBatchSize)
	}
	return m, nil
}

// ─── Commands ─────────────────────────────────────────────────────────────────

// fetchConfigSettings loads all three settings from the store and, when
// embeddings are enabled, also fetches embedding coverage. Called on view entry.
func (m Model) fetchConfigSettings() tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	settingsCmd := func() tea.Msg {
		ctx := context.Background()
		enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"
		url := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
		model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)
		return configSettingsLoadedMsg{embeddingsEnabled: enabled, ollamaURL: url, model: model}
	}
	coverageCmd := func() tea.Msg {
		ctx := context.Background()
		enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"
		if !enabled {
			return configCoverageLoadedMsg{}
		}
		model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)
		have, total, _ := st.EmbeddingCoverage(ctx, "", model)
		return configCoverageLoadedMsg{have: have, total: total}
	}
	return tea.Batch(settingsCmd, coverageCmd)
}

// saveConfigSetting persists a single key/value pair. Returns a command that
// produces a configSaveSettingMsg carrying any error so the view can surface it.
func (m Model) saveConfigSetting(key, value string) tea.Cmd {
	if key == "" {
		return func() tea.Msg { return configSaveSettingMsg{} }
	}
	// Allow test injection via saveFn.
	if m.saveFn != nil {
		fn := m.saveFn
		return func() tea.Msg {
			return configSaveSettingMsg{err: fn(key, value)}
		}
	}
	if m.store == nil {
		return func() tea.Msg { return configSaveSettingMsg{} }
	}
	st := m.store
	return func() tea.Msg {
		err := st.SetSetting(context.Background(), key, value)
		return configSaveSettingMsg{err: err}
	}
}

// ─── Embed-missing pure helper ────────────────────────────────────────────────

// embedMissingAll embeds all observations that do not yet have an embedding row
// for embedder.Model(). Unlike regenerateAll it does NOT call DeleteAllEmbeddings
// first — it is a pure incremental backfill.
//
// Returns (done, total, err):
//   - done  — number of observations successfully embedded in this run.
//   - total — total non-deleted observations across all projects.
//   - err   — nil on full success; non-nil on systemic failure (zero-progress guard).
func embedMissingAll(ctx context.Context, st *store.Store, embedder embed.Embedder) (done, total int, err error) {
	return embedMissingAllWithBatch(ctx, st, embedder, embedBatchSize)
}

// embedMissingAllWithBatch is the testable core of embedMissingAll.
func embedMissingAllWithBatch(ctx context.Context, st *store.Store, embedder embed.Embedder, batch int) (done, total int, err error) {
	model := embedder.Model()
	var lastEmbedErr error

	for {
		missing, fetchErr := st.MissingEmbeddings(ctx, "", model, batch)
		if fetchErr != nil {
			return done, total, fmt.Errorf("embed-missing: fetch: %w", fetchErr)
		}
		if len(missing) == 0 {
			break
		}

		batchSucceeded := 0
		for _, obs := range missing {
			text := obs.Title + "\n" + obs.Content

			embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			vec, embedErr := embedder.Embed(embedCtx, text)
			cancel()

			if embedErr != nil {
				lastEmbedErr = embedErr
				continue
			}
			if upsertErr := st.UpsertEmbedding(ctx, obs.ID, model, vec); upsertErr != nil {
				continue
			}
			done++
			batchSucceeded++
		}

		if len(missing) < batch {
			break
		}
		// Zero-progress guard: full batch fetched but nothing succeeded.
		if len(missing) == batch && batchSucceeded == 0 {
			_, total, _ = st.EmbeddingCoverage(ctx, "", model)
			if lastEmbedErr != nil {
				return done, total, fmt.Errorf("embed-missing: no progress in last batch: %w", lastEmbedErr)
			}
			return done, total, nil
		}
	}

	_, total, err = st.EmbeddingCoverage(ctx, "", model)
	if err != nil {
		return done, 0, fmt.Errorf("embed-missing: coverage: %w", err)
	}
	return done, total, lastEmbedErr
}

// ─── Chained batch engine: EMBED MISSING ─────────────────────────────────────

// makeDefaultEmbedBatchFn returns the production EMBED MISSING batch function.
// Each call processes one batch of embedBatchSize observations and returns an
// embedJobBatchMsg. The engine in Update issues the next cmd on partial results.
func (m Model) makeDefaultEmbedBatchFn() func(url, modelName string, offset, batch int) tea.Cmd {
	st := m.store
	return func(baseURL, modelName string, offset, batch int) tea.Cmd {
		return func() tea.Msg {
			if st == nil {
				return embedJobBatchMsg{finished: true, lastErr: fmt.Errorf("store unavailable")}
			}
			ctx := context.Background()
			client := embed.DefaultClient(baseURL)
			embedder := embed.NewOllamaEmbedder(client, modelName)
			model := embedder.Model()

			missing, err := st.MissingEmbeddings(ctx, "", model, batch)
			if err != nil {
				return embedJobBatchMsg{finished: true, lastErr: err}
			}
			if len(missing) == 0 {
				// Nothing left to embed — finished.
				_, total, _ := st.EmbeddingCoverage(ctx, "", model)
				return embedJobBatchMsg{done: 0, total: total, finished: true}
			}

			batchDone := 0
			var lastErr error
			for _, obs := range missing {
				text := obs.Title + "\n" + obs.Content
				embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				vec, embedErr := embedder.Embed(embedCtx, text)
				cancel()
				if embedErr != nil {
					lastErr = embedErr
					continue
				}
				if err := st.UpsertEmbedding(ctx, obs.ID, model, vec); err != nil {
					continue
				}
				batchDone++
			}

			have, total, _ := st.EmbeddingCoverage(ctx, "", model)

			// Zero-progress guard: full batch but nothing embedded.
			if len(missing) == batch && batchDone == 0 {
				return embedJobBatchMsg{done: have, total: total, lastErr: lastErr, aborted: true}
			}
			// Partial batch means no more rows are missing → finished.
			// Full batch means there may be more → issue next batch.
			finished := len(missing) < batch
			return embedJobBatchMsg{done: have, total: total, lastErr: lastErr, finished: finished}
		}
	}
}

// ─── Chained batch engine: REGENERATE ────────────────────────────────────────

// makeDefaultRegenBatchFn returns the production REGENERATE batch function for
// the chained engine. The first batch deletes all embeddings before fetching
// the first page; subsequent batches continue the loop.
//
// Note: in the chained engine the deletion happens ONCE at job start (triggered
// by a dedicated "delete-then-first-batch" cmd). For simplicity the first call
// (offset==0) performs the deletion; subsequent calls skip it.
func (m Model) makeDefaultRegenBatchFn() func(url, modelName string, offset, batch int) tea.Cmd {
	st := m.store
	return func(baseURL, modelName string, offset, batch int) tea.Cmd {
		return func() tea.Msg {
			if st == nil {
				return embedJobBatchMsg{finished: true, lastErr: fmt.Errorf("store unavailable")}
			}
			ctx := context.Background()
			client := embed.DefaultClient(baseURL)
			embedder := embed.NewOllamaEmbedder(client, modelName)
			model := embedder.Model()

			// First batch: wipe all embeddings.
			if offset == 0 {
				if _, err := st.DeleteAllEmbeddings(ctx); err != nil {
					return embedJobBatchMsg{finished: true, lastErr: err}
				}
			}

			missing, err := st.MissingEmbeddings(ctx, "", model, batch)
			if err != nil {
				return embedJobBatchMsg{finished: true, lastErr: err}
			}
			if len(missing) == 0 {
				_, total, _ := st.EmbeddingCoverage(ctx, "", model)
				return embedJobBatchMsg{done: 0, total: total, finished: true}
			}

			batchDone := 0
			var lastErr error
			for _, obs := range missing {
				text := obs.Title + "\n" + obs.Content
				embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				vec, embedErr := embedder.Embed(embedCtx, text)
				cancel()
				if embedErr != nil {
					lastErr = embedErr
					continue
				}
				if err := st.UpsertEmbedding(ctx, obs.ID, model, vec); err != nil {
					continue
				}
				batchDone++
			}

			have, total, _ := st.EmbeddingCoverage(ctx, "", model)

			if len(missing) == batch && batchDone == 0 {
				return embedJobBatchMsg{done: have, total: total, lastErr: lastErr, aborted: true}
			}
			finished := len(missing) < batch
			return embedJobBatchMsg{done: have, total: total, lastErr: lastErr, finished: finished}
		}
	}
}

// ─── Legacy single-shot regen (kept for existing unit tests) ─────────────────

// regenerateAll deletes all embeddings and re-embeds every observation using
// embedder. It is extracted as a pure function (no TUI coupling) so that tests
// can inject a fake Embedder and a real temporary store without starting Ollama.
//
// Returns (done, total, err):
//   - done  — number of observations successfully embedded.
//   - total — total number of non-deleted observations across all projects.
//   - err   — first fatal error that stopped the loop (nil on full success or
//     partial completion where individual embed failures were skipped).
func regenerateAll(ctx context.Context, st *store.Store, embedder embed.Embedder) (done, total int, err error) {
	return regenerateAllWithBatch(ctx, st, embedder, 50)
}

// regenerateAllWithBatch is the testable core of regenerateAll. The batch
// parameter controls how many observations are fetched per iteration; callers
// that need the default production behaviour should use regenerateAll instead.
//
// Zero-progress guard: if a full batch is fetched (len(missing) == batch) but
// every embed attempt in that batch fails (batchSucceeded == 0), the loop
// terminates immediately and returns a non-nil error to prevent an infinite
// loop where the same unembeddable rows are re-fetched on every iteration.
func regenerateAllWithBatch(ctx context.Context, st *store.Store, embedder embed.Embedder, batch int) (done, total int, err error) {
	model := embedder.Model()

	if _, err := st.DeleteAllEmbeddings(ctx); err != nil {
		return 0, 0, fmt.Errorf("regenerate: clear embeddings: %w", err)
	}

	var lastEmbedErr error

	for {
		missing, fetchErr := st.MissingEmbeddings(ctx, "", model, batch)
		if fetchErr != nil {
			return done, total, fmt.Errorf("regenerate: fetch missing: %w", fetchErr)
		}
		if len(missing) == 0 {
			break
		}

		batchSucceeded := 0
		for _, obs := range missing {
			text := obs.Title + "\n" + obs.Content

			embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			vec, embedErr := embedder.Embed(embedCtx, text)
			cancel()

			if embedErr != nil {
				lastEmbedErr = embedErr
				// Skip individual embed failures; caller reports partial.
				continue
			}

			if upsertErr := st.UpsertEmbedding(ctx, obs.ID, model, vec); upsertErr != nil {
				// Skip upsert failures similarly.
				continue
			}
			done++
			batchSucceeded++
		}

		fullBatch := len(missing) == batch
		if len(missing) < batch {
			break
		}
		if fullBatch && batchSucceeded == 0 {
			// Zero-progress guard: a full batch was fetched but nothing was
			// successfully embedded. Without this break the loop would refetch
			// the same rows forever. Surface the last embed error so callers
			// can distinguish a systemic failure from a clean empty-store exit.
			_, total, _ = st.EmbeddingCoverage(ctx, "", model)
			if lastEmbedErr != nil {
				return done, total, fmt.Errorf("regenerate: no progress in last batch: %w", lastEmbedErr)
			}
			return done, total, nil
		}
	}

	// Final total count.
	_, total, err = st.EmbeddingCoverage(ctx, "", model)
	if err != nil {
		return done, 0, fmt.Errorf("regenerate: coverage: %w", err)
	}

	return done, total, nil
}

// makeDefaultRegenFn returns the legacy (single-shot blocking) REGENERATE
// EMBEDDINGS function. Used only when regenBatchFn is nil (existing unit tests
// that stub regenFn directly). Production now uses makeDefaultRegenBatchFn.
func (m Model) makeDefaultRegenFn() func(url, model string) tea.Cmd {
	st := m.store
	return func(baseURL, modelName string) tea.Cmd {
		return func() tea.Msg {
			if st == nil {
				return configRegenResultMsg{
					ok:   false,
					info: "REGENERATE: store unavailable",
				}
			}
			ctx := context.Background()
			client := embed.DefaultClient(baseURL)
			embedder := embed.NewOllamaEmbedder(client, modelName)

			done, total, err := regenerateAll(ctx, st, embedder)
			if err != nil {
				return configRegenResultMsg{
					ok:   false,
					info: fmt.Sprintf("OLLAMA UNREACHABLE — %v", err),
				}
			}
			if done < total {
				return configRegenResultMsg{
					ok:   false,
					info: fmt.Sprintf("PARTIAL — %d/%d embedded — some failures", done, total),
				}
			}
			return configRegenResultMsg{
				ok:   true,
				info: fmt.Sprintf("EMBEDDINGS REGENERATED — %d/%d — model %s", done, total, modelName),
			}
		}
	}
}

// defaultProbeFn is the production probe function. It constructs an embed.Client
// using the current settings and runs Ping → HasModel → ProbeEmbed.
func defaultProbeFn(baseURL, model string) tea.Cmd {
	return func() tea.Msg {
		c := embed.DefaultClient(baseURL)
		ctx := context.Background()

		if err := c.Ping(ctx); err != nil {
			return configProbeResultMsg{ok: false, info: "OLLAMA UNREACHABLE — " + err.Error()}
		}

		ok, err := c.HasModel(ctx, model)
		if err != nil {
			return configProbeResultMsg{ok: false, info: "OLLAMA UNREACHABLE — " + err.Error()}
		}
		if !ok {
			return configProbeResultMsg{
				ok:   false,
				info: fmt.Sprintf("MODEL NOT FOUND — pull it with: ollama pull %s", model),
			}
		}

		dims, elapsed, err := c.ProbeEmbed(ctx, model)
		if err != nil {
			return configProbeResultMsg{ok: false, info: "PROBE FAILED — " + err.Error()}
		}

		info := fmt.Sprintf("OLLAMA OK — %s available — %d dims — %dms",
			model, dims, elapsed.Milliseconds())
		return configProbeResultMsg{ok: true, info: info}
	}
}

// ─── Progress bar ─────────────────────────────────────────────────────────────

// renderProgressBar renders a retro BBS-style progress bar.
// Format: `▓▓▓▓▓▓▓░░░░░░░░░  57/142  (40%)`
// width controls the number of block character columns in the fill region.
// The function is pure (no side effects) and safe to test in isolation.
func renderProgressBar(done, total, width int) string {
	if width < 4 {
		width = 4
	}
	var pct int
	if total > 0 {
		pct = done * 100 / total
	}
	filled := 0
	if total > 0 {
		filled = done * width / total
	}
	if filled > width {
		filled = width
	}
	empty := width - filled

	var bar strings.Builder
	bar.WriteString(barFilledStyle.Render(strings.Repeat("▓", filled)))
	bar.WriteString(barEmptyStyle.Render(strings.Repeat("░", empty)))
	bar.WriteString(fmt.Sprintf("  %d/%d  (%d%%)", done, total, pct))
	return bar.String()
}

// ─── View ─────────────────────────────────────────────────────────────────────

// viewConfigPage renders the config view with exact-fill and wide centering.
func (m Model) viewConfigPage() string {
	// ── chrome ──────────────────────────────────────────────────────────────
	header := m.renderHeader("Projects // Config")
	separator := m.renderSeparator()

	cOffset := contentOffset(m.width)
	cWidth := effectiveWidth(m.width)
	if cWidth < 40 {
		cWidth = 40
	}
	rowIndent := strings.Repeat(" ", cOffset+leftPad)

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	rows := []struct {
		label string
		value func() string
	}{
		{
			label: "EMBEDDINGS",
			value: func() string {
				if m.configEmbeddingsEnabled {
					return configValueStyle.Foreground(defaultTheme.accent).Render("[ON] ")
				}
				return configValueStyle.Foreground(defaultTheme.dim).Render("[OFF]")
			},
		},
		{
			label: "OLLAMA URL",
			value: func() string {
				if m.configEditing && m.configCursor == configRowOllamaURL {
					return m.configInput.View()
				}
				return configValueStyle.Render(m.configOllamaURL)
			},
		},
		{
			label: "MODEL",
			value: func() string {
				if m.configEditing && m.configCursor == configRowModel {
					return m.configInput.View()
				}
				return configValueStyle.Render(m.configModel)
			},
		},
		{
			label: "TEST CONNECTION",
			value: func() string {
				// While the probe is in flight the row itself shows TESTING…;
				// a finished result is rendered in the trailing result block below.
				if m.configTesting {
					return configTestingStyle.Render("TESTING…")
				}
				return ""
			},
		},
		{
			label: "EMBED MISSING",
			value: func() string {
				if m.jobRunning && m.jobKind == jobKindEmbed {
					return configTestingStyle.Render("EMBEDDING…")
				}
				return ""
			},
		},
		{
			label: "REGENERATE EMBEDDINGS",
			value: func() string {
				// While regen is in flight the row itself shows REGENERATING…;
				// a finished result is rendered in the trailing result block below.
				if m.configRegenerating || (m.jobRunning && m.jobKind == jobKindRegen) {
					return configTestingStyle.Render("REGENERATING…")
				}
				return ""
			},
		},
	}

	for i, row := range rows {
		label := configLabelStyle.Render(row.label)
		val := row.value()

		var line string
		if i == m.configCursor {
			// Selected row: render label+value with selection highlight on the label.
			selLabel := configSelStyle.Render(fmt.Sprintf("%-*s", configLabelWidth, row.label))
			// During edit the row shows label highlighted + live input (same layout).
			line = rowIndent + selLabel + " " + val
		} else {
			line = rowIndent + label + " " + val
		}
		content.WriteString(line + "\n")
	}

	// ── idle coverage line (shown when no job is running and embeddings are on) ──
	if !m.jobRunning && m.configEmbeddingsEnabled {
		pct := 0
		if m.configCoverageTotal > 0 {
			pct = m.configCoverageHave * 100 / m.configCoverageTotal
		}
		covLine := fmt.Sprintf("COVERAGE  %d/%d (%d%%)", m.configCoverageHave, m.configCoverageTotal, pct)
		content.WriteString("\n" + rowIndent + configTestingStyle.Render(covLine) + "\n")
	}

	// ── progress bar (visible while job is running) ───────────────────────────
	if m.jobRunning {
		label := "EMBEDDING  "
		if m.jobKind == jobKindRegen {
			label = "REGENERATING "
		}
		bar := renderProgressBar(m.jobDone, m.jobTotal, progressBarWidth)
		content.WriteString("\n" + rowIndent + configTestingStyle.Render(label) + bar + "\n")
	}

	// ── job result (shown when job finished) ──────────────────────────────────
	if !m.jobRunning && m.jobResult != "" {
		var resultLine string
		if m.jobResultOK {
			resultLine = configOKStyle.Render(m.jobResult)
		} else {
			resultLine = configDangerStyle.Render(m.jobResult)
		}
		content.WriteString("\n" + rowIndent + resultLine + "\n")
	}

	// ── test result ───────────────────────────────────────────────────────────
	// While a probe is in flight the TEST CONNECTION row itself shows TESTING…;
	// this trailing block only renders a finished result.
	if !m.configTesting && m.configTestResult != "" {
		var resultLine string
		if m.configTestOK {
			resultLine = configOKStyle.Render("OLLAMA OK — " + strings.TrimPrefix(m.configTestResult, "OLLAMA OK — "))
		} else {
			resultLine = configDangerStyle.Render(m.configTestResult)
		}
		content.WriteString("\n" + rowIndent + resultLine + "\n")
	}

	// ── regen result (legacy path: configRegenResult set by old regenFn) ──────
	// Rendered only when the new jobResult is empty (prevents double-rendering
	// when the new engine is used — it sets jobResult directly).
	if !m.configRegenerating && m.configRegenResult != "" && m.jobResult == "" {
		var resultLine string
		if m.configRegenOK {
			resultLine = configOKStyle.Render(m.configRegenResult)
		} else {
			resultLine = configDangerStyle.Render(m.configRegenResult)
		}
		content.WriteString("\n" + rowIndent + resultLine + "\n")
	}

	// ── setting-save error ───────────────────────────────────────────────────
	// Shown when the last st.SetSetting call returned an error. Clears on
	// subsequent successful save.
	if m.configSaveErr != nil {
		errLine := configDangerStyle.Render("SETTING NOT SAVED — " + m.configSaveErr.Error())
		content.WriteString("\n" + rowIndent + errLine + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	statusText := "CONFIG // EMBEDDINGS SETTINGS"
	if m.jobRunning {
		if m.jobKind == jobKindRegen {
			statusText = "CONFIG // REGENERATING EMBEDDINGS…"
		} else {
			statusText = "CONFIG // EMBEDDING MISSING…"
		}
	} else if m.configRegenerating {
		statusText = "CONFIG // REGENERATING EMBEDDINGS…"
	} else if m.jobResult != "" {
		if m.jobResultOK {
			statusText = "CONFIG // EMBEDDINGS UP TO DATE"
		} else {
			statusText = "CONFIG // EMBEDDING FAILED"
		}
	} else if m.configRegenResult != "" {
		if m.configRegenOK {
			statusText = "CONFIG // EMBEDDINGS REGENERATED"
		} else {
			statusText = "CONFIG // REGENERATION FAILED"
		}
	} else if m.configTesting {
		statusText = "CONFIG // TESTING CONNECTION…"
	} else if m.configTestResult != "" {
		if m.configTestOK {
			statusText = "CONFIG // CONNECTION OK"
		} else {
			statusText = "CONFIG // CONNECTION FAILED"
		}
	}

	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusText)
	footerLine := strings.Repeat(" ", cOffset+leftPad) + m.renderFooter()

	// ── compose full-height layout ───────────────────────────────────────────
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}

// ─── ensure time import is used ──────────────────────────────────────────────

var _ = time.Second // referenced via embed.DefaultClient timeout
