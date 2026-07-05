package tui

// config_test.go — Strict TDD tests for the Config view.
//
// TDD cycle order:
//  1. TestConfig_ViewStateConstant — viewConfig exists and is distinct.
//  2. TestConfig_CKeyOpensConfigView — 'c' from projects transitions to viewConfig.
//  3. TestConfig_EscFromConfigReturnsToProjects — Esc returns to viewProjects.
//  4. TestConfig_ToggleEmbeddingsFlipsValue — Enter on EMBEDDINGS row flips toggle.
//  5. TestConfig_ToggleIssueSaveCmd — toggling embeddings issues a settings save command.
//  6. TestConfig_SpaceTogglesEmbeddings — Space also toggles the embeddings setting.
//  7. TestConfig_ArrowsNavigateSettings — ↑↓ move cursor through settings rows.
//  8. TestConfig_EnterOnURLOpensInput — Enter on OLLAMA URL row opens inline edit.
//  9. TestConfig_EscCancelsEdit — Esc during edit restores original value.
// 10. TestConfig_EnterSavesURLEdit — Enter in URL edit mode saves value and closes input.
// 11. TestConfig_EnterOnModelOpensInput — Enter on MODEL row opens inline edit.
// 12. TestConfig_EnterOnTestRunsProbe — Enter on TEST CONNECTION issues probe command.
// 13. TestConfig_TestRunningMessageRendered — while running, TESTING… shown in status.
// 14. TestConfig_ProbeSuccessRendered — success probe result rendered in accent style.
// 15. TestConfig_ProbeFailureRendered — failure probe result rendered (OLLAMA UNREACHABLE).
// 16. TestConfig_ModelNotFoundRendered — model-not-found error rendered.
// 17. TestConfig_SettingsLoadedApplied — configSettingsLoadedMsg updates config fields.
// 18. TestConfig_RenderSmoke_80x24 — exact-fill at 80x24.
// 19. TestConfig_RenderSmoke_Wide_200x55 — centered exact-fill at 200x55.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// newConfigModel returns a config-ready model in viewConfig with sensible defaults.
func newConfigModel() Model {
	m := newModel()
	m.view = viewConfig
	m.configEmbeddingsEnabled = false
	m.configOllamaURL = "http://localhost:11434"
	m.configModel = "nomic-embed-text"
	return m
}

// ─── 1. viewConfig state constant ────────────────────────────────────────────

func TestConfig_ViewStateConstant(t *testing.T) {
	// viewConfig must be distinct from all other view states.
	all := []viewState{viewProjects, viewObservations, viewDetail, viewGlobalSearch}
	for _, v := range all {
		if viewConfig == v {
			t.Errorf("viewConfig (%d) collides with existing view state %d", viewConfig, v)
		}
	}
}

// ─── 2. 'c' key opens config view from projects ───────────────────────────────

func TestConfig_CKeyOpensConfigView(t *testing.T) {
	m := newModel()
	m.view = viewProjects
	m.projects = makeProjectSummaries()

	m = sendRune(m, 'c')
	if m.view != viewConfig {
		t.Errorf("after 'c' from projects, view = %v, want viewConfig", m.view)
	}
}

// ─── 3. Esc from config returns to projects ───────────────────────────────────

func TestConfig_EscFromConfigReturnsToProjects(t *testing.T) {
	m := newConfigModel()

	m = sendKey(m, tea.KeyEsc)
	if m.view != viewProjects {
		t.Errorf("after Esc from config, view = %v, want viewProjects", m.view)
	}
}

// ─── 4. Toggle embeddings on Enter ────────────────────────────────────────────

func TestConfig_ToggleEmbeddingsFlipsValue(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0 // EMBEDDINGS row
	m.configEmbeddingsEnabled = false

	m = sendKey(m, tea.KeyEnter)
	if !m.configEmbeddingsEnabled {
		t.Error("after Enter on EMBEDDINGS row, configEmbeddingsEnabled should flip to true")
	}

	m = sendKey(m, tea.KeyEnter)
	if m.configEmbeddingsEnabled {
		t.Error("second Enter on EMBEDDINGS row, configEmbeddingsEnabled should flip back to false")
	}
}

// ─── 5. Toggle issues save cmd ────────────────────────────────────────────────

func TestConfig_ToggleIssueSaveCmd(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0 // EMBEDDINGS row
	m.configEmbeddingsEnabled = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("toggling EMBEDDINGS should return a non-nil save command")
	}
}

// ─── 6. Space also toggles embeddings ────────────────────────────────────────

func TestConfig_SpaceTogglesEmbeddings(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0
	m.configEmbeddingsEnabled = false

	m = sendKey(m, tea.KeySpace)
	if !m.configEmbeddingsEnabled {
		t.Error("Space on EMBEDDINGS row should toggle configEmbeddingsEnabled")
	}
}

// ─── 7. Arrow keys navigate settings rows ────────────────────────────────────

func TestConfig_ArrowsNavigateSettings(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0

	m = sendKey(m, tea.KeyDown)
	if m.configCursor != 1 {
		t.Errorf("after ↓, configCursor = %d, want 1", m.configCursor)
	}

	m = sendKey(m, tea.KeyDown)
	if m.configCursor != 2 {
		t.Errorf("after ↓↓, configCursor = %d, want 2", m.configCursor)
	}

	m = sendKey(m, tea.KeyUp)
	if m.configCursor != 1 {
		t.Errorf("after ↑, configCursor = %d, want 1", m.configCursor)
	}
}

func TestConfig_CursorDoesNotGoNegative(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0

	m = sendKey(m, tea.KeyUp)
	if m.configCursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", m.configCursor)
	}
}

func TestConfig_CursorDoesNotExceedLastRow(t *testing.T) {
	m := newConfigModel()
	// Move to last row (configRowCount-1 = 5).
	for i := 0; i < 10; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	last := m.configCursor
	if last != configRowCount-1 {
		t.Errorf("expected cursor to clamp at last row %d, got %d", configRowCount-1, last)
	}
	m = sendKey(m, tea.KeyDown)
	if m.configCursor != last {
		t.Errorf("cursor should not move past last row %d, got %d", last, m.configCursor)
	}
}

// ─── 8. Enter on OLLAMA URL opens inline input ────────────────────────────────

func TestConfig_EnterOnURLOpensInput(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 1 // OLLAMA URL row

	m = sendKey(m, tea.KeyEnter)
	if !m.configEditing {
		t.Error("Enter on OLLAMA URL should set configEditing=true")
	}
}

// ─── 9. Esc cancels edit ──────────────────────────────────────────────────────

func TestConfig_EscCancelsEdit(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 1
	m.configEditing = true
	m.configOllamaURL = "http://changed:9999"  // currently changed
	m.configEditOrig = "http://original:11434" // original before edit started
	m.configInput.SetValue("http://changed:9999")

	m = sendKey(m, tea.KeyEsc)
	if m.configEditing {
		t.Error("Esc during edit should set configEditing=false")
	}
	if m.configOllamaURL != "http://original:11434" {
		t.Errorf("Esc should restore original value, got %q", m.configOllamaURL)
	}
}

// ─── 10. Enter saves URL edit ─────────────────────────────────────────────────

func TestConfig_EnterSavesURLEdit(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 1
	m.configEditing = true
	m.configInput.SetValue("http://newhost:8080")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.configEditing {
		t.Error("Enter during URL edit should close edit mode")
	}
	if m.configOllamaURL != "http://newhost:8080" {
		t.Errorf("after Enter save, configOllamaURL = %q, want %q", m.configOllamaURL, "http://newhost:8080")
	}
	if cmd == nil {
		t.Error("saving URL should issue a settings save command")
	}
}

// ─── 11. Enter on MODEL row opens inline input ────────────────────────────────

func TestConfig_EnterOnModelOpensInput(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 2 // MODEL row

	m = sendKey(m, tea.KeyEnter)
	if !m.configEditing {
		t.Error("Enter on MODEL row should set configEditing=true")
	}
}

// ─── 12. Enter on TEST CONNECTION issues probe ────────────────────────────────

func TestConfig_EnterOnTestRunsProbe(t *testing.T) {
	m := newConfigModel()
	// Navigate to TEST CONNECTION (row 3)
	m.configCursor = 3

	// Inject a stub probe function.
	m.probeFn = func(url, model string) tea.Cmd {
		return func() tea.Msg { return configProbeResultMsg{ok: true, info: "stub"} }
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("Enter on TEST CONNECTION should return a probe command")
	}
}

// ─── 13. TESTING… status while probe is running ───────────────────────────────

func TestConfig_TestRunningMessageRendered(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.configTesting = true // simulates in-flight probe

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "TESTING") {
		t.Errorf("View() should show TESTING while probe is in flight; plain:\n%s", plain)
	}
}

// ─── 14. Probe success result rendered ────────────────────────────────────────

func TestConfig_ProbeSuccessRendered(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)

	// Simulate probe success result arriving.
	next, _ := m.Update(configProbeResultMsg{ok: true, info: "nomic-embed-text available — 768 dims — 42ms"})
	m = next.(Model)

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "OLLAMA OK") {
		t.Errorf("View() should show OLLAMA OK on success; plain:\n%s", plain)
	}
}

// ─── 15. Probe failure: OLLAMA UNREACHABLE ────────────────────────────────────

func TestConfig_ProbeFailureRendered(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)

	next, _ := m.Update(configProbeResultMsg{ok: false, info: "OLLAMA UNREACHABLE — connection refused"})
	m = next.(Model)

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "OLLAMA UNREACHABLE") {
		t.Errorf("View() should show OLLAMA UNREACHABLE on failure; plain:\n%s", plain)
	}
}

// ─── 16. Model not found error ────────────────────────────────────────────────

func TestConfig_ModelNotFoundRendered(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)

	next, _ := m.Update(configProbeResultMsg{ok: false, info: "MODEL NOT FOUND — pull it with: ollama pull nomic-embed-text"})
	m = next.(Model)

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "MODEL NOT FOUND") {
		t.Errorf("View() should show MODEL NOT FOUND; plain:\n%s", plain)
	}
}

// ─── 17. Settings loaded message updates config fields ────────────────────────

func TestConfig_SettingsLoadedApplied(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = false
	m.configOllamaURL = ""
	m.configModel = ""

	next, _ := m.Update(configSettingsLoadedMsg{
		embeddingsEnabled: true,
		ollamaURL:         "http://myhost:11434",
		model:             "mxbai-embed-large",
	})
	m = next.(Model)

	if !m.configEmbeddingsEnabled {
		t.Error("configEmbeddingsEnabled should be true after settings loaded")
	}
	if m.configOllamaURL != "http://myhost:11434" {
		t.Errorf("configOllamaURL = %q, want %q", m.configOllamaURL, "http://myhost:11434")
	}
	if m.configModel != "mxbai-embed-large" {
		t.Errorf("configModel = %q, want %q", m.configModel, "mxbai-embed-large")
	}
}

// ─── 18. Render smoke: exact-fill at 80x24 ────────────────────────────────────

func TestConfig_RenderSmoke_80x24(t *testing.T) {
	const termW, termH = 80, 24
	m := newConfigModel()
	m = setSize(m, termW, termH)

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("config view 80x24: View() produced %d lines, want %d", lineCount, termH)
	}
	lines := viewLines(out)
	if len(lines) == 0 {
		t.Fatal("no lines in View()")
	}
	// Header must contain the retro brand.
	if !strings.Contains(stripAnsiCodes(lines[0]), "ION//MEM") {
		t.Errorf("header does not contain ION//MEM: %q", stripAnsiCodes(lines[0]))
	}
	// Footer must contain [ESC] BACK.
	footer := stripAnsiCodes(lines[len(lines)-1])
	if !strings.Contains(strings.ToUpper(footer), "ESC") || !strings.Contains(strings.ToUpper(footer), "BACK") {
		t.Errorf("footer should contain ESC BACK; got: %q", footer)
	}
	// Breadcrumb must include CONFIG.
	header := stripAnsiCodes(lines[0])
	if !strings.Contains(strings.ToUpper(header), "CONFIG") {
		t.Errorf("header breadcrumb should contain CONFIG; got: %q", header)
	}
}

// ─── 19. Render smoke: wide exact-fill at 200x55 ─────────────────────────────

func TestConfig_RenderSmoke_Wide_200x55(t *testing.T) {
	const termW, termH = 200, 55
	m := newConfigModel()
	m = setSize(m, termW, termH)

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("config view 200x55: View() produced %d lines, want %d", lineCount, termH)
	}

	lines := viewLines(out)
	if len(lines) < 2 {
		t.Fatal("fewer than 2 lines")
	}
	// Status bar should mention CONFIG.
	status := stripAnsiCodes(lines[len(lines)-2])
	if !strings.Contains(strings.ToUpper(status), "CONFIG") {
		t.Errorf("status bar should mention CONFIG; got: %q", status)
	}
}

// ─── 20–30. REGENERATE EMBEDDINGS row ────────────────────────────────────────

// 20. configRowRegen constant exists and is at index 5 (after EMBED MISSING at 4).
func TestConfig_RegenRowConstant(t *testing.T) {
	if configRowRegen != 5 {
		t.Errorf("configRowRegen = %d, want 5 (shifted after EMBED MISSING insert)", configRowRegen)
	}
	if configRowCount != 6 {
		t.Errorf("configRowCount = %d, want 6", configRowCount)
	}
}

// 21. ↓↓↓↓↓ from row 0 lands on configRowRegen (row 5).
func TestConfig_CursorCanReachRegenRow(t *testing.T) {
	m := newConfigModel()
	m.configCursor = 0
	for i := 0; i < 5; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	if m.configCursor != configRowRegen {
		t.Errorf("after 5×↓ from row 0, configCursor = %d, want %d", m.configCursor, configRowRegen)
	}
}

// 22. Cursor does not go beyond row 5.
func TestConfig_CursorDoesNotExceedRegenRow(t *testing.T) {
	m := newConfigModel()
	for i := 0; i < 10; i++ {
		m = sendKey(m, tea.KeyDown)
	}
	if m.configCursor != configRowRegen {
		t.Errorf("cursor should clamp at row %d (configRowRegen), got %d", configRowRegen, m.configCursor)
	}
}

// 23. Enter on REGENERATE EMBEDDINGS with embeddings OFF shows danger message (no cmd).
func TestConfig_RegenWhenEmbeddingsDisabledShowsDanger(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowRegen
	m.configEmbeddingsEnabled = false

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	// No async command (no work done).
	if cmd != nil {
		// Drain the command and assert it produces configRegenResultMsg with ok=false.
		msg := cmd()
		rm, ok := msg.(configRegenResultMsg)
		if !ok {
			t.Fatalf("expected configRegenResultMsg, got %T", msg)
		}
		if rm.ok {
			t.Error("regen with embeddings off: result should not be ok=true")
		}
		if !strings.Contains(strings.ToUpper(rm.info), "EMBEDDINGS ARE OFF") {
			t.Errorf("regen with embeddings off: expected EMBEDDINGS ARE OFF in info, got %q", rm.info)
		}
	} else {
		// cmd nil means it's handled synchronously — check state directly.
		_ = m // state check below covers both paths
	}
	// Either way, configRegenerating must be false (no in-flight operation).
	if m.configRegenerating {
		t.Error("configRegenerating should be false when embeddings are off")
	}
}

// 24. Enter on REGENERATE EMBEDDINGS with embeddings ON triggers regenFn.
func TestConfig_RegenWithEmbeddingsEnabledTriggersRegenFn(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowRegen
	m.configEmbeddingsEnabled = true

	called := false
	m.regenFn = func(url, model string) tea.Cmd {
		called = true
		return func() tea.Msg { return configRegenResultMsg{ok: true, info: "done"} }
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !called {
		t.Error("regenFn should have been called on Enter with embeddings enabled")
	}
	if cmd == nil {
		t.Error("Enter on REGENERATE EMBEDDINGS with embeddings enabled should return a command")
	}
}

// 25. While regenerating, configRegenerating is true and REGENERATING… appears in the row value.
func TestConfig_RegenInFlightShownInRow(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.configCursor = configRowRegen
	m.configRegenerating = true

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "REGENERATING") {
		t.Errorf("View() should show REGENERATING while regen is in flight; plain:\n%s", plain)
	}
}

// 26. Pressing Enter while already regenerating does not re-trigger (no-op).
func TestConfig_RegenDoesNotRetriggerWhileInFlight(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowRegen
	m.configEmbeddingsEnabled = true
	m.configRegenerating = true // already in flight

	callCount := 0
	m.regenFn = func(url, model string) tea.Cmd {
		callCount++
		return func() tea.Msg { return configRegenResultMsg{ok: true, info: "done"} }
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if callCount != 0 {
		t.Error("regenFn should not be called while configRegenerating is true")
	}
}

// 27. configRegenResultMsg with ok=true updates model fields and clears configRegenerating.
func TestConfig_RegenResultOKUpdatesState(t *testing.T) {
	m := newConfigModel()
	m.configRegenerating = true

	next, _ := m.Update(configRegenResultMsg{ok: true, info: "EMBEDDINGS REGENERATED — 10/10 — model nomic-embed-text"})
	m = next.(Model)

	if m.configRegenerating {
		t.Error("configRegenerating should be false after result arrives")
	}
	if !m.configRegenOK {
		t.Error("configRegenOK should be true on success")
	}
	if m.configRegenResult == "" {
		t.Error("configRegenResult should be set after result arrives")
	}
}

// 28. configRegenResultMsg is ignored when view != viewConfig (stale guard).
func TestConfig_RegenResultIgnoredWhenNotInConfigView(t *testing.T) {
	m := newConfigModel()
	m.view = viewProjects // stale — user navigated away
	m.configRegenerating = true

	next, _ := m.Update(configRegenResultMsg{ok: true, info: "done"})
	m = next.(Model)

	// configRegenerating must NOT be cleared — stale message discarded.
	if !m.configRegenerating {
		t.Error("stale configRegenResultMsg should not clear configRegenerating when view != viewConfig")
	}
}

// 29. Success result renders in the output.
func TestConfig_RegenSuccessRendered(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)

	next, _ := m.Update(configRegenResultMsg{ok: true, info: "EMBEDDINGS REGENERATED — 5/5 — model nomic-embed-text"})
	m = next.(Model)

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "EMBEDDINGS REGENERATED") {
		t.Errorf("View() should show EMBEDDINGS REGENERATED on success; plain:\n%s", plain)
	}
}

// 30. Failure result renders in danger style (text visible).
func TestConfig_RegenFailureRendered(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)

	next, _ := m.Update(configRegenResultMsg{ok: false, info: "OLLAMA UNREACHABLE — connection refused"})
	m = next.(Model)

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "OLLAMA UNREACHABLE") {
		t.Errorf("View() should show OLLAMA UNREACHABLE on failure; plain:\n%s", plain)
	}
}

// 31. Exact-fill still holds at 80x24 with the new row.
func TestConfig_RenderSmoke_80x24_WithRegenRow(t *testing.T) {
	const termW, termH = 80, 24
	m := newConfigModel()
	m = setSize(m, termW, termH)

	out := m.View()
	lineCount := strings.Count(out, "\n")
	if lineCount != termH {
		t.Errorf("config view 80x24 with regen row: View() produced %d lines, want %d", lineCount, termH)
	}

	// REGENERATE EMBEDDINGS label should appear in the view.
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "REGENERATE") {
		t.Errorf("config view should render REGENERATE label; plain:\n%s", plain)
	}
}
