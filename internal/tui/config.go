package tui

// config.go — Config view: embeddings toggle, Ollama URL, model, verbose log,
// connection test, embed-missing (incremental backfill), regenerate.
//
// Design:
//   - Entry: 'c' from projects view.
//   - Breadcrumb: PROJECTS // CONFIG.
//   - Seven settings rows navigated with ↑↓:
//       0  EMBEDDINGS             [ON] / [OFF] — Enter/Space toggles, persists immediately.
//       1  OLLAMA URL             <url>        — Enter opens inline edit.
//       2  MODEL                  <model>      — Enter opens inline edit.
//       3  VERBOSE LOG            [ON] / [OFF] — Enter/Space toggles, persists immediately.
//       4  TEST CONNECTION                     — Enter runs async probe.
//       5  EMBED MISSING                       — Enter backfills un-embedded observations.
//       6  REGENERATE EMBEDDINGS               — Enter wipes then re-embeds all.
//   - Esc closes edit or returns to projects. A running job is NOT affected by
//     Esc: its event stream keeps being processed regardless of which view is
//     active (see jobengine.go); only the visual progress block is suppressed
//     outside viewConfig.
//   - 's'/'S' cancels the running job (its context is cancelled; the job
//     reports StatusStopped on its next EventDone).
//   - 'r'/'R' retries EMBED MISSING when the last job did not complete.
//   - Probe result shown in status area: OK (accent) or error (danger).
//   - Job (embed/regen) progress shown as a live retro block while running;
//     result line shown when done.
//   - Exact-fill + wide centering consistent with the rest of the TUI.

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── constants ────────────────────────────────────────────────────────────────

// configLabelWidth is the fixed column width for config row labels.
// "REGENERATE EMBEDDINGS" (21 chars) is the longest label — use 22 to leave
// one space of padding.
const configLabelWidth = 22

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
		// Returning to projects does NOT stop a running job: its event stream
		// keeps flowing (see Update's jobEventMsg case), just without the
		// visual progress block.
		m.view = viewProjects
		m.configTesting = false
		return m, nil

	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 'q') || msg.Type == tea.KeyCtrlC:
		m.cancelIfJobRunning()
		return m, tea.Quit

	case msg.Type == tea.KeyUp || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'k'):
		if m.configCursor > 0 {
			m.configCursor--
		}

	case msg.Type == tea.KeyDown || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'j'):
		if m.configCursor < configRowCount-1 {
			m.configCursor++
		}

	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 's' || msg.Runes[0] == 'S'):
		// STOP: cancel the running job's context. The job reports
		// StatusStopped on its next EventDone; jobRunning flips false then.
		if m.jobRunning && m.jobCancel != nil {
			m.jobCancel()
		}
		return m, nil

	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 'r' || msg.Runes[0] == 'R'):
		// RETRY: only meaningful once a job has finished short of Complete.
		if m.jobRunning {
			return m, nil
		}
		if m.jobSummary != nil && m.jobSummary.Status != embedjob.StatusComplete {
			return m.startJob(embedjob.KindEmbedMissing)
		}
		return m, nil

	case msg.Type == tea.KeyEnter || msg.Type == tea.KeySpace:
		return m.handleConfigAction(msg.Type == tea.KeySpace)
	}

	return m, nil
}

// handleConfigAction dispatches the action for the currently selected config row.
// spaceKey is true when the trigger was Space (only relevant for the two toggle rows).
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

	case configRowVerboseLog:
		m.configVerboseLog = !m.configVerboseLog
		val := "false"
		if m.configVerboseLog {
			val = "true"
		}
		return m, m.saveConfigSetting(store.SettingLogVerbose, val)

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
		if !m.configEmbeddingsEnabled {
			m.jobResult = "EMBEDDINGS ARE OFF — enable them first"
			m.jobResultOK = false
			return m, nil
		}
		// Block re-trigger while a job is already running.
		if m.jobRunning {
			return m, nil
		}
		return m.startJob(embedjob.KindEmbedMissing)

	case configRowRegen:
		if spaceKey {
			return m, nil
		}
		if !m.configEmbeddingsEnabled {
			m.jobResult = "EMBEDDINGS ARE OFF — enable them first"
			m.jobResultOK = false
			return m, nil
		}
		if m.jobRunning {
			return m, nil
		}
		return m.startJob(embedjob.KindRegenerate)
	}
	return m, nil
}

// ─── Commands ─────────────────────────────────────────────────────────────────

// fetchConfigSettings loads all settings from the store and, when embeddings
// are enabled, also fetches embedding coverage. Called on view entry.
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
		verbose := st.SettingOrDefault(ctx, store.SettingLogVerbose, "false") == "true"
		return configSettingsLoadedMsg{
			embeddingsEnabled: enabled,
			ollamaURL:         url,
			model:             model,
			verboseLog:        verbose,
		}
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
			label: "VERBOSE LOG",
			value: func() string {
				if m.configVerboseLog {
					return configValueStyle.Foreground(defaultTheme.accent).Render("[ON] ")
				}
				return configValueStyle.Foreground(defaultTheme.dim).Render("[OFF]")
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
				if m.jobRunning && m.jobKind == embedjob.KindEmbedMissing {
					return configTestingStyle.Render("EMBEDDING…")
				}
				return ""
			},
		},
		{
			label: "REGENERATE EMBEDDINGS",
			value: func() string {
				if m.jobRunning && m.jobKind == embedjob.KindRegenerate {
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

	// ── live progress block (visible while a job is running) ─────────────────
	if m.jobRunning {
		content.WriteString("\n" + m.renderJobProgress(rowIndent, cWidth))
	}

	// ── job result (shown when the last job finished) ─────────────────────────
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

	// ── setting-save error ───────────────────────────────────────────────────
	// Shown when the last st.SetSetting call returned an error. Clears on
	// subsequent successful save.
	if m.configSaveErr != nil {
		errLine := configDangerStyle.Render("SETTING NOT SAVED — " + m.configSaveErr.Error())
		content.WriteString("\n" + rowIndent + errLine + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	statusText := "CONFIG // EMBEDDINGS SETTINGS"
	switch {
	case m.jobRunning && m.jobKind == embedjob.KindRegenerate:
		statusText = "CONFIG // REGENERATING EMBEDDINGS…"
	case m.jobRunning:
		statusText = "CONFIG // EMBEDDING MISSING…"
	case m.jobResult != "":
		if m.jobResultOK {
			statusText = "CONFIG // EMBEDDINGS UP TO DATE"
		} else {
			statusText = "CONFIG // EMBEDDING FAILED"
		}
	case m.configTesting:
		statusText = "CONFIG // TESTING CONNECTION…"
	case m.configTestResult != "":
		if m.configTestOK {
			statusText = "CONFIG // CONNECTION OK"
		} else {
			statusText = "CONFIG // CONNECTION FAILED"
		}
	}

	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusText)
	footerLine := m.renderFooterLine(cOffset)

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
