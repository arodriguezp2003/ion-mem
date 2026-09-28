package tui

import (
	"strings"
	"testing"
)

// TestConfig_CoverageLineRendered_WhenEmbeddingsEnabled verifies that the config
// view renders an idle coverage line when embeddings are enabled and a coverage
// message has been received.
func TestConfig_CoverageLineRendered_WhenEmbeddingsEnabled(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = true
	m.configModel = "nomic-embed-text"
	m.configCoverageHave = 8
	m.configCoverageTotal = 10

	// Set a terminal size so rendering can work.
	m.width = 80
	m.height = 24

	view := m.viewConfigPage()
	if !strings.Contains(view, "COVERAGE") {
		t.Errorf("config view missing COVERAGE line when embeddings enabled:\n%s", view)
	}
	if !strings.Contains(view, "8/10") {
		t.Errorf("config view missing coverage fraction 8/10:\n%s", view)
	}
}

// TestConfig_CoverageLineNotRendered_WhenEmbeddingsDisabled verifies that when
// embeddings are off no coverage line appears.
func TestConfig_CoverageLineNotRendered_WhenEmbeddingsDisabled(t *testing.T) {
	m := newConfigModel()
	m.configEmbeddingsEnabled = false
	m.configCoverageHave = 5
	m.configCoverageTotal = 10

	m.width = 80
	m.height = 24

	view := m.viewConfigPage()
	if strings.Contains(view, "COVERAGE") {
		t.Errorf("config view must not show COVERAGE line when embeddings are disabled:\n%s", view)
	}
}

// TestConfigCoverageLoadedMsg_UpdatesModelFields verifies that receiving a
// configCoverageLoadedMsg populates configCoverageHave and configCoverageTotal.
func TestConfigCoverageLoadedMsg_UpdatesModelFields(t *testing.T) {
	m := newConfigModel()
	m.view = viewConfig

	updated, _ := m.Update(configCoverageLoadedMsg{have: 42, total: 100})
	m2, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}

	if m2.configCoverageHave != 42 {
		t.Errorf("configCoverageHave = %d, want 42", m2.configCoverageHave)
	}
	if m2.configCoverageTotal != 100 {
		t.Errorf("configCoverageTotal = %d, want 100", m2.configCoverageTotal)
	}
}
