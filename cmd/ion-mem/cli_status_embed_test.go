package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// TestWriteStatusReport_EmbeddingsEnabled_ShowsCoverage verifies that when
// embeddings.enabled is true the status report contains an embeddings coverage line.
func TestWriteStatusReport_EmbeddingsEnabled_ShowsCoverage(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		dbSize:  1024,
		limit:   5,
		now:     time.Now().UTC(),
		stats:   store.Stats{TotalObservations: 10},
		embeddingReport: embeddingReport{
			enabled:  true,
			model:    "nomic-embed-text",
			embedded: 8,
			total:    10,
		},
	})
	out := sb.String()
	if !strings.Contains(out, "embeddings:") {
		t.Errorf("status output missing embeddings line: %s", out)
	}
	if !strings.Contains(out, "8/10") {
		t.Errorf("status output missing coverage fraction 8/10: %s", out)
	}
	if !strings.Contains(out, "nomic-embed-text") {
		t.Errorf("status output missing model name: %s", out)
	}
}

// TestWriteStatusReport_EmbeddingsDisabled_ShowsDisabled verifies that when
// embeddings are disabled the status report shows "embeddings: disabled".
func TestWriteStatusReport_EmbeddingsDisabled_ShowsDisabled(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		dbSize:  1024,
		limit:   5,
		now:     time.Now().UTC(),
		stats:   store.Stats{TotalObservations: 5},
		embeddingReport: embeddingReport{
			enabled: false,
		},
	})
	out := sb.String()
	if !strings.Contains(out, "embeddings: disabled") {
		t.Errorf("expected 'embeddings: disabled' in status, got: %s", out)
	}
}

// TestWriteStatusReport_FullCoverage_ShowsPercent verifies percentage rendering.
func TestWriteStatusReport_FullCoverage_ShowsPercent(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		dbSize:  1024,
		limit:   5,
		now:     time.Now().UTC(),
		stats:   store.Stats{TotalObservations: 3},
		embeddingReport: embeddingReport{
			enabled:  true,
			model:    "nomic-embed-text",
			embedded: 3,
			total:    3,
		},
	})
	out := sb.String()
	if !strings.Contains(out, "100%") {
		t.Errorf("expected 100%% in full coverage status, got: %s", out)
	}
}
