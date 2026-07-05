package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPreviousView_ObsDetailEscReturnsToObservations verifies the standard path:
// observations → detail → Esc → observations (previousView set to viewObservations).
func TestPreviousView_ObsDetailEscReturnsToObservations(t *testing.T) {
	m := newModel()
	m.view = viewObservations
	m.observations = makeObservations()
	m.obsCursor = 0

	// Enter detail.
	m = sendKey(m, tea.KeyEnter)
	if m.view != viewDetail {
		t.Fatalf("expected viewDetail after Enter, got %v", m.view)
	}
	if m.previousView != viewObservations {
		t.Errorf("previousView = %v, want viewObservations", m.previousView)
	}

	// Esc should return to viewObservations.
	m = sendKey(m, tea.KeyEsc)
	if m.view != viewObservations {
		t.Errorf("after Esc from detail (from obs), view = %v, want viewObservations", m.view)
	}
}

// TestPreviousView_GlobalSearchDetailEscReturnsToGlobalSearch verifies the fix:
// global-search results → detail → Esc → global-search results (not viewObservations).
func TestPreviousView_GlobalSearchDetailEscReturnsToGlobalSearch(t *testing.T) {
	m := newModel()
	m.view = viewGlobalSearch
	m.observations = makeObservations()
	m.globalQuery = "test query"
	m.obsCursor = 0

	// Enter detail from global search.
	m = sendKey(m, tea.KeyEnter)
	if m.view != viewDetail {
		t.Fatalf("expected viewDetail after Enter from globalSearch, got %v", m.view)
	}
	if m.previousView != viewGlobalSearch {
		t.Errorf("previousView = %v, want viewGlobalSearch", m.previousView)
	}

	// Esc must return to viewGlobalSearch, not viewObservations.
	m = sendKey(m, tea.KeyEsc)
	if m.view != viewGlobalSearch {
		t.Errorf("after Esc from detail (from global search), view = %v, want viewGlobalSearch", m.view)
	}
}

// TestPreviousView_GlobalSearchResultsPreservedAfterDetailReturn verifies that
// the global search results (observations slice) and query are intact after
// navigating to detail and back.
func TestPreviousView_GlobalSearchResultsPreservedAfterDetailReturn(t *testing.T) {
	m := newModel()
	m.view = viewGlobalSearch
	obs := makeObservations()
	m.observations = obs
	m.globalQuery = "my search query"
	m.obsCursor = 0

	// Navigate to detail and back.
	m = sendKey(m, tea.KeyEnter)
	m = sendKey(m, tea.KeyEsc)

	if m.view != viewGlobalSearch {
		t.Errorf("view = %v, want viewGlobalSearch", m.view)
	}
	if len(m.observations) != len(obs) {
		t.Errorf("observations count = %d, want %d (results must be preserved)", len(m.observations), len(obs))
	}
	if m.globalQuery != "my search query" {
		t.Errorf("globalQuery = %q, want %q", m.globalQuery, "my search query")
	}
}
