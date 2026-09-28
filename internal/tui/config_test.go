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
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arodriguezp2003/ion-mem/internal/store"
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
	// Navigate to TEST CONNECTION.
	m.configCursor = configRowTestConn

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

// ─── Setting-save error surface ──────────────────────────────────────────────

// TestConfig_SaveSettingErrorStoredInModel verifies that when saveConfigSetting
// produces a configSaveSettingMsg with a non-nil err, the error is stored in
// the model so the view can render it.
func TestConfig_SaveSettingErrorStoredInModel(t *testing.T) {
	m := newConfigModel()
	saveErr := fmt.Errorf("disk full")

	next, _ := m.Update(configSaveSettingMsg{err: saveErr})
	m = next.(Model)

	if m.configSaveErr == nil {
		t.Fatal("configSaveErr should be set after configSaveSettingMsg with non-nil err")
	}
	if m.configSaveErr.Error() != "disk full" {
		t.Errorf("configSaveErr = %q, want %q", m.configSaveErr.Error(), "disk full")
	}
}

// TestConfig_SaveSettingErrorRenderedAsDanger verifies that when configSaveErr
// is set, the config view renders a SETTING NOT SAVED danger message.
func TestConfig_SaveSettingErrorRenderedAsDanger(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.configSaveErr = fmt.Errorf("permission denied")

	out := m.View()
	plain := stripAnsiCodes(out)
	if !strings.Contains(strings.ToUpper(plain), "SETTING NOT SAVED") {
		t.Errorf("View() should show SETTING NOT SAVED when configSaveErr is set; plain:\n%s", plain)
	}
}

// TestConfig_SaveSettingSuccessClearsPreviousError verifies that a successful
// save (err=nil) clears any previously stored configSaveErr.
func TestConfig_SaveSettingSuccessClearsPreviousError(t *testing.T) {
	m := newConfigModel()
	m.configSaveErr = fmt.Errorf("old error")

	next, _ := m.Update(configSaveSettingMsg{err: nil})
	m = next.(Model)

	if m.configSaveErr != nil {
		t.Errorf("configSaveErr should be nil after successful save, got %v", m.configSaveErr)
	}
}

// TestConfig_SaveFnFailureProducesErrorMsg verifies that when saveFn is injected
// and returns an error, the resulting configSaveSettingMsg carries that error.
func TestConfig_SaveFnFailureProducesErrorMsg(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowEmbeddings
	m.configEmbeddingsEnabled = false
	injectedErr := fmt.Errorf("injected save failure")
	m.saveFn = func(key, value string) error {
		return injectedErr
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("toggling EMBEDDINGS should return a command")
	}
	msg := cmd()
	saveMsg, ok := msg.(configSaveSettingMsg)
	if !ok {
		t.Fatalf("expected configSaveSettingMsg, got %T", msg)
	}
	if saveMsg.err == nil {
		t.Fatal("configSaveSettingMsg should carry the error from saveFn")
	}
	if saveMsg.err.Error() != "injected save failure" {
		t.Errorf("err = %q, want %q", saveMsg.err.Error(), "injected save failure")
	}
}

// ─── VERBOSE LOG row ──────────────────────────────────────────────────────────

// TestConfig_VerboseLogRowConstant verifies the row sits between MODEL and
// TEST CONNECTION, and that configRowCount grew to accommodate it.
func TestConfig_VerboseLogRowConstant(t *testing.T) {
	if configRowVerboseLog != configRowModel+1 {
		t.Errorf("configRowVerboseLog = %d, want %d (right after MODEL)", configRowVerboseLog, configRowModel+1)
	}
	if configRowTestConn != configRowVerboseLog+1 {
		t.Errorf("configRowTestConn = %d, want %d (right after VERBOSE LOG)", configRowTestConn, configRowVerboseLog+1)
	}
}

// TestConfig_VerboseLogTogglesThroughSaveFn verifies Enter on the VERBOSE LOG
// row flips the local toggle and persists it via saveFn (or st.SetSetting in
// production), and that the rendered value flips between on/off.
func TestConfig_VerboseLogTogglesThroughSaveFn(t *testing.T) {
	m := newConfigModel()
	m = setSize(m, 80, 24)
	m.configCursor = configRowVerboseLog
	m.configVerboseLog = false

	var savedKey, savedVal string
	m.saveFn = func(key, value string) error {
		savedKey, savedVal = key, value
		return nil
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.configVerboseLog {
		t.Fatal("Enter on VERBOSE LOG should flip configVerboseLog to true")
	}
	if cmd == nil {
		t.Fatal("toggling VERBOSE LOG should return a save command")
	}
	_ = cmd()
	if savedKey != store.SettingLogVerbose || savedVal != "true" {
		t.Errorf("saveFn called with (%q, %q), want (%q, %q)", savedKey, savedVal, store.SettingLogVerbose, "true")
	}

	out := m.View()
	if !strings.Contains(stripAnsiCodes(out), "[ON]") {
		t.Errorf("View() should show [ON] once VERBOSE LOG is enabled; out:\n%s", stripAnsiCodes(out))
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.configVerboseLog {
		t.Fatal("second Enter on VERBOSE LOG should flip back to false")
	}
	_ = cmd()
	if savedVal != "false" {
		t.Errorf("saveFn second call value = %q, want %q", savedVal, "false")
	}
}

// TestConfig_VerboseLogLoadedFromSettings verifies configSettingsLoadedMsg
// applies the verboseLog field.
func TestConfig_VerboseLogLoadedFromSettings(t *testing.T) {
	m := newConfigModel()
	m.configVerboseLog = false

	next, _ := m.Update(configSettingsLoadedMsg{verboseLog: true})
	m = next.(Model)

	if !m.configVerboseLog {
		t.Error("configVerboseLog should be true after configSettingsLoadedMsg{verboseLog: true}")
	}
}

// ─── EMBED MISSING / REGENERATE disabled-embeddings guard ────────────────────

// TestConfig_EmbedMissingWhenDisabledShowsDangerSynchronously verifies that,
// unlike the old chained-batch engine, disabling embeddings produces an
// immediate synchronous danger result with no job started and no command
// needed to observe it.
func TestConfig_EmbedMissingWhenDisabledShowsDangerSynchronously(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowEmbedMissing
	m.configEmbeddingsEnabled = false

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should stay false when embeddings are disabled")
	}
	if m.jobResultOK {
		t.Error("jobResultOK should be false when embeddings are disabled")
	}
	if !strings.Contains(strings.ToUpper(m.jobResult), "EMBEDDINGS ARE OFF") {
		t.Errorf("jobResult = %q, want it to mention EMBEDDINGS ARE OFF", m.jobResult)
	}
}

// Triangulate: same guard for REGENERATE.
func TestConfig_RegenWhenDisabledShowsDangerSynchronously(t *testing.T) {
	m := newConfigModel()
	m.configCursor = configRowRegen
	m.configEmbeddingsEnabled = false

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.jobRunning {
		t.Error("jobRunning should stay false when embeddings are disabled")
	}
	if !strings.Contains(strings.ToUpper(m.jobResult), "EMBEDDINGS ARE OFF") {
		t.Errorf("jobResult = %q, want it to mention EMBEDDINGS ARE OFF", m.jobResult)
	}
}
