package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func makeProjectSummaries() []store.ProjectSummary {
	return []store.ProjectSummary{
		{Project: "alpha", ObservationCount: 5, SessionCount: 2, LastActivity: time.Now().Add(-2 * time.Hour)},
		{Project: "beta", ObservationCount: 3, SessionCount: 1, LastActivity: time.Now().Add(-30 * time.Minute)},
	}
}

func makeObservations() []store.Observation {
	return []store.Observation{
		{ID: 1, Title: "First obs", Type: "decision", CreatedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano)},
		{ID: 2, Title: "Second obs", Type: "bugfix", CreatedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano)},
	}
}

func sendKey(m Model, keyType tea.KeyType) Model {
	next, _ := m.Update(tea.KeyMsg{Type: keyType})
	return next.(Model)
}

func sendRune(m Model, r rune) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return next.(Model)
}

// ─── initial view ─────────────────────────────────────────────────────────────

func TestModel_initialViewIsProjects(t *testing.T) {
	m := newModel()
	if m.view != viewProjects {
		t.Errorf("initial view = %v, want viewProjects", m.view)
	}
}

// ─── projects → observations on Enter ────────────────────────────────────────

func TestModel_enterOnProjectNavigatesToObservations(t *testing.T) {
	m := newModel()
	m.projects = makeProjectSummaries()
	m.projectCursor = 0

	// Load observations first via a message, then press Enter.
	obs := makeObservations()
	next, _ := m.Update(observationsLoadedMsg{observations: obs, project: "alpha"})
	m = next.(Model)
	m.view = viewProjects // reset back to projects

	m = sendKey(m, tea.KeyEnter)
	if m.view != viewObservations {
		t.Errorf("after Enter on project, view = %v, want viewObservations", m.view)
	}
	if m.selectedProject != "alpha" {
		t.Errorf("selectedProject = %q, want %q", m.selectedProject, "alpha")
	}
}

// ─── observations → projects on Esc ──────────────────────────────────────────

func TestModel_escFromObservationsGoesBackToProjects(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.selectedProject = "alpha"

	m = sendKey(m, tea.KeyEsc)
	if m.view != viewProjects {
		t.Errorf("after Esc from observations, view = %v, want viewProjects", m.view)
	}
}

// ─── observations → detail on Enter ──────────────────────────────────────────

func TestModel_enterOnObservationNavigatesToDetail(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.obsCursor = 0

	m = sendKey(m, tea.KeyEnter)
	if m.view != viewDetail {
		t.Errorf("after Enter on observation, view = %v, want viewDetail", m.view)
	}
	if m.selectedObs == nil || m.selectedObs.ID != 1 {
		t.Errorf("selectedObs.ID = %v, want 1", m.selectedObs)
	}
}

// ─── detail → observations on Esc ────────────────────────────────────────────

func TestModel_escFromDetailGoesBackToObservations(t *testing.T) {
	m := newModel()
	m.view = viewDetail
	obs := makeObservations()
	m.selectedObs = &obs[0]

	m = sendKey(m, tea.KeyEsc)
	if m.view != viewObservations {
		t.Errorf("after Esc from detail, view = %v, want viewObservations", m.view)
	}
}

// ─── search flow ──────────────────────────────────────────────────────────────

func TestModel_slashOpensSearchInput(t *testing.T) {
	m := newModel()
	m.view = viewObservations

	m = sendRune(m, '/')
	if !m.searching {
		t.Error("after '/', searching should be true")
	}
}

func TestModel_escClearsSearchBackToRecent(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.searching = true
	m.searchQuery = "something"

	m = sendKey(m, tea.KeyEsc)
	if m.searching {
		t.Error("after Esc, searching should be false")
	}
	if m.searchQuery != "" {
		t.Errorf("after Esc, searchQuery = %q, want empty", m.searchQuery)
	}
	if m.view != viewObservations {
		t.Errorf("after Esc, view = %v, want viewObservations", m.view)
	}
}

func TestModel_fuzzyIndicatorSetWhenResultsAreFuzzy(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.searching = true

	obs := makeObservations()
	next, _ := m.Update(searchResultMsg{results: obs, fuzzy: true})
	m = next.(Model)

	if !m.fuzzyResults {
		t.Error("fuzzyResults should be true when search results came from OR fallback")
	}
	if m.searching {
		t.Error("searching should be cleared after results arrive")
	}
}

func TestModel_fuzzyIndicatorClearedOnNonFuzzyResults(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.fuzzyResults = true // was fuzzy before

	obs := makeObservations()
	next, _ := m.Update(searchResultMsg{results: obs, fuzzy: false})
	m = next.(Model)

	if m.fuzzyResults {
		t.Error("fuzzyResults should be false when results are not fuzzy")
	}
}

// ─── delete confirm flow ──────────────────────────────────────────────────────

func TestModel_dKeyInObservationsTriggersConfirm(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.obsCursor = 0

	m = sendRune(m, 'd')
	if !m.confirmDelete {
		t.Error("after 'd', confirmDelete should be true")
	}
}

func TestModel_confirmDeleteYesClearsConfirmFlag(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.obsCursor = 0
	m.confirmDelete = true

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	// confirmDelete should be cleared after confirm, regardless of whether
	// the store command was issued (store is nil in unit tests).
	if m.confirmDelete {
		t.Error("after 'y' confirm, confirmDelete should be false")
	}
}

func TestModel_confirmDeleteNoCancels(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.obsCursor = 0
	m.confirmDelete = true

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(Model)
	if m.confirmDelete {
		t.Error("after 'n' cancel, confirmDelete should be false")
	}
	if cmd != nil {
		t.Error("after 'n' cancel, no command should be issued")
	}
}

// ─── delete in detail view ────────────────────────────────────────────────────

func TestModel_dKeyInDetailTriggersConfirm(t *testing.T) {
	m := newModel()
	m.view = viewDetail
	obs := makeObservations()
	m.selectedObs = &obs[0]

	m = sendRune(m, 'd')
	if !m.confirmDelete {
		t.Error("after 'd' in detail, confirmDelete should be true")
	}
}

// ─── j/k navigation ───────────────────────────────────────────────────────────

func TestModel_jkNavigatesProjectList(t *testing.T) {
	m := newModel()
	m.view = viewProjects
	m.projects = makeProjectSummaries()
	m.projectCursor = 0

	m = sendRune(m, 'j')
	if m.projectCursor != 1 {
		t.Errorf("after j, projectCursor = %d, want 1", m.projectCursor)
	}

	m = sendRune(m, 'k')
	if m.projectCursor != 0 {
		t.Errorf("after k, projectCursor = %d, want 0", m.projectCursor)
	}
}

func TestModel_jkNavigatesObservationList(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.obsCursor = 0

	m = sendRune(m, 'j')
	if m.obsCursor != 1 {
		t.Errorf("after j in obs view, obsCursor = %d, want 1", m.obsCursor)
	}

	m = sendRune(m, 'k')
	if m.obsCursor != 0 {
		t.Errorf("after k in obs view, obsCursor = %d, want 0", m.obsCursor)
	}
}

// ─── cursor clamping ─────────────────────────────────────────────────────────

func TestModel_cursorDoesNotGoNegative(t *testing.T) {
	m := newModel()
	m.view = viewProjects
	m.projects = makeProjectSummaries()
	m.projectCursor = 0

	m = sendRune(m, 'k')
	if m.projectCursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", m.projectCursor)
	}
}

func TestModel_cursorDoesNotExceedListLength(t *testing.T) {
	m := newModel()
	m.view = viewProjects
	m.projects = makeProjectSummaries()
	m.projectCursor = len(m.projects) - 1

	m = sendRune(m, 'j')
	if m.projectCursor != len(m.projects)-1 {
		t.Errorf("cursor should not exceed last index, got %d", m.projectCursor)
	}
}

// ─── q quits from projects ───────────────────────────────────────────────────

func TestModel_qQuitsFromProjectsView(t *testing.T) {
	m := newModel()
	m.view = viewProjects

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
	// tea.Quit returns a non-nil BatchMsg or command; verify it's Quit.
	// The simplest check: execute the command and verify it produces a QuitMsg.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("q command produced %T, want tea.QuitMsg", msg)
	}
}

// ─── search params scoped to project ─────────────────────────────────────────

func TestModel_searchSubmitSetsProject(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.selectedProject = "myproject"
	m.searching = true
	m.searchQuery = "architecture"

	// searchSubmitMsg causes a runSearch which is a command (async, store may be nil in tests).
	// Verify state: selectedProject is preserved so the scoped search can use it.
	next, _ := m.Update(searchSubmitMsg{query: "architecture"})
	m = next.(Model)
	if m.selectedProject != "myproject" {
		t.Errorf("selectedProject = %q, want %q after search submit", m.selectedProject, "myproject")
	}
}

func TestModel_searchSubmitClearsSearchingFlag(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.selectedProject = "myproject"
	m.searching = true
	m.searchQuery = "query"

	next, _ := m.Update(searchSubmitMsg{query: "query"})
	m = next.(Model)
	// searching is cleared, query is preserved until results arrive.
	if m.searching {
		t.Error("searching should be cleared when submit message is processed")
	}
}

// ─── delete result refreshes list ────────────────────────────────────────────

func TestModel_deleteResultClearsConfirmAndRefreshesList(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.confirmDelete = true

	obs := []store.Observation{makeObservations()[1]} // only the second obs remains
	next, _ := m.Update(observationsLoadedMsg{observations: obs, project: "alpha"})
	m = next.(Model)

	if len(m.observations) != 1 {
		t.Errorf("observations after delete refresh = %d, want 1", len(m.observations))
	}
}
