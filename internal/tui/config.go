package tui

// config.go — Config view: embeddings toggle, Ollama URL, model, connection test.
//
// Design:
//   - Entry: 'c' from projects view.
//   - Breadcrumb: PROJECTS // CONFIG.
//   - Four settings rows navigated with ↑↓:
//       0  EMBEDDINGS      [ON] / [OFF]    — Enter/Space toggles, persists immediately.
//       1  OLLAMA URL      <url>           — Enter opens inline edit.
//       2  MODEL           <model>         — Enter opens inline edit.
//       3  TEST CONNECTION                 — Enter runs async probe.
//   - Esc closes edit or returns to projects.
//   - Probe result shown in status area: OK (accent) or error (danger).
//   - Exact-fill + wide centering consistent with the rest of the TUI.

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

// ─── styles (config-specific) ────────────────────────────────────────────────

var (
	configLabelStyle = lipgloss.NewStyle().Foreground(defaultTheme.dim).Width(16)
	configValueStyle = lipgloss.NewStyle().Bold(true)
	configSelStyle   = lipgloss.NewStyle().
				Bold(true).
				Background(defaultTheme.accent).
				Foreground(lipgloss.AdaptiveColor{Dark: "#1A0407", Light: "#FFFFFF"})
	configOKStyle      = lipgloss.NewStyle().Foreground(defaultTheme.accent).Bold(true)
	configDangerStyle  = lipgloss.NewStyle().Foreground(defaultTheme.danger).Bold(true)
	configTestingStyle = lipgloss.NewStyle().Foreground(defaultTheme.dim)
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
	}
	return m, nil
}

// ─── Commands ─────────────────────────────────────────────────────────────────

// fetchConfigSettings loads all three settings from the store. Called on view
// entry so the config view always reflects the persisted state.
func (m Model) fetchConfigSettings() tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	return func() tea.Msg {
		ctx := context.Background()
		enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"
		url := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
		model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, "nomic-embed-text")
		return configSettingsLoadedMsg{embeddingsEnabled: enabled, ollamaURL: url, model: model}
	}
}

// saveConfigSetting persists a single key/value pair. Returns a fire-and-forget
// command; the configSaveSettingMsg it produces is handled by Update silently.
func (m Model) saveConfigSetting(key, value string) tea.Cmd {
	if m.store == nil || key == "" {
		return func() tea.Msg { return configSaveSettingMsg{} }
	}
	st := m.store
	return func() tea.Msg {
		_ = st.SetSetting(context.Background(), key, value)
		return configSaveSettingMsg{}
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
				if m.configTesting {
					return configTestingStyle.Render("TESTING…")
				}
				return ""
			},
		},
	}

	for i, row := range rows {
		label := configLabelStyle.Render(row.label)
		val := row.value()

		// Determine the input width for editing rows.
		if m.configEditing && (i == configRowOllamaURL || i == configRowModel) && m.configCursor == i {
			// The text input renders at a fixed width — handled by the input itself.
		}

		var line string
		if i == m.configCursor {
			// Selected row: render label+value with selection highlight on the label.
			selLabel := configSelStyle.Render(fmt.Sprintf("%-16s", row.label))
			if m.configEditing {
				// During edit the row shows label highlighted + live input.
				line = rowIndent + selLabel + " " + val
			} else {
				line = rowIndent + selLabel + " " + val
			}
		} else {
			line = rowIndent + label + " " + val
		}
		content.WriteString(line + "\n")
	}

	// ── test result ───────────────────────────────────────────────────────────
	if m.configTesting {
		content.WriteString("\n" + rowIndent + configTestingStyle.Render("TESTING…") + "\n")
	} else if m.configTestResult != "" {
		var resultLine string
		if m.configTestOK {
			resultLine = configOKStyle.Render("OLLAMA OK — " + strings.TrimPrefix(m.configTestResult, "OLLAMA OK — "))
		} else {
			resultLine = configDangerStyle.Render(m.configTestResult)
		}
		content.WriteString("\n" + rowIndent + resultLine + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	statusText := "CONFIG // EMBEDDINGS SETTINGS"
	if m.configTesting {
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
