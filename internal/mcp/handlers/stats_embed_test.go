package handlers_test

import (
	"testing"
)

// TestIonStats_EmbeddingsFieldPresent verifies that ion_stats includes an
// "embeddings" object in extras with enabled, model, embedded, and total fields.
func TestIonStats_EmbeddingsFieldPresent(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_stats", map[string]any{})
	env := decodeText(t, res)

	stats, ok := env["stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected stats object, got %v", env["stats"])
	}

	embeddings, ok := stats["embeddings"].(map[string]any)
	if !ok {
		t.Fatalf("stats.embeddings must be an object, got %v", stats["embeddings"])
	}

	// "enabled" must be a bool.
	if _, ok := embeddings["enabled"].(bool); !ok {
		t.Errorf("embeddings.enabled must be bool, got %T: %v", embeddings["enabled"], embeddings["enabled"])
	}

	// "model" must be a string.
	if _, ok := embeddings["model"].(string); !ok {
		t.Errorf("embeddings.model must be string, got %T: %v", embeddings["model"], embeddings["model"])
	}

	// "embedded" and "total" must be numeric.
	if _, ok := embeddings["embedded"].(float64); !ok {
		t.Errorf("embeddings.embedded must be float64, got %T: %v", embeddings["embedded"], embeddings["embedded"])
	}
	if _, ok := embeddings["total"].(float64); !ok {
		t.Errorf("embeddings.total must be float64, got %T: %v", embeddings["total"], embeddings["total"])
	}
}

// TestIonStats_EmbeddingsDisabledByDefault verifies that when no settings are
// written, embeddings.enabled is false (the default).
func TestIonStats_EmbeddingsDisabledByDefault(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, fakeProject("ion-mem"))

	res := callTool(t, ts, "ion_stats", map[string]any{})
	env := decodeText(t, res)

	stats := env["stats"].(map[string]any)
	embeddings := stats["embeddings"].(map[string]any)

	if enabled, _ := embeddings["enabled"].(bool); enabled {
		t.Errorf("embeddings.enabled should be false by default, got true")
	}
}
