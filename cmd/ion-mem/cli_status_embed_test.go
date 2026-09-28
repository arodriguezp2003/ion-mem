package main

import (
	"strings"
	"testing"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/store"
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

// TestWriteStatusReport_OllamaReachable_ShowsPresentLine verifies the extra
// Ollama health line reusing `ion-mem doctor`'s check (see ollamaHealth in
// cli_doctor.go) when Ollama is reachable and the model is present.
func TestWriteStatusReport_OllamaReachable_ShowsPresentLine(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		now:     time.Now().UTC(),
		stats:   store.Stats{TotalObservations: 10},
		embeddingReport: embeddingReport{
			enabled: true, model: "bge-m3", embedded: 8, total: 10,
			ollamaReachable: true, ollamaModelPresent: true,
		},
	})
	out := sb.String()
	if !strings.Contains(out, "ollama: reachable (bge-m3 present)") {
		t.Errorf("status output missing reachable/present line: %s", out)
	}
}

// TestWriteStatusReport_OllamaUnreachable_ShowsDegradeWarning verifies the
// unreachable variant of the same line.
func TestWriteStatusReport_OllamaUnreachable_ShowsDegradeWarning(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		now:     time.Now().UTC(),
		stats:   store.Stats{TotalObservations: 10},
		embeddingReport: embeddingReport{
			enabled: true, model: "bge-m3", embedded: 8, total: 10,
			ollamaReachable: false,
		},
	})
	out := sb.String()
	if !strings.Contains(out, "ollama: unreachable — search will degrade to lexical") {
		t.Errorf("status output missing unreachable warning line: %s", out)
	}
}

// TestWriteStatusReport_ShowsPerStatusCounts verifies the
// "status: N active · N superseded · N obsolete" line reports
// Stats.StatusCounts, including a zero for a status with no rows.
func TestWriteStatusReport_ShowsPerStatusCounts(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		now:     time.Now().UTC(),
		stats: store.Stats{
			TotalObservations: 15,
			StatusCounts: map[string]int64{
				store.StatusActive:     10,
				store.StatusSuperseded: 4,
				store.StatusObsolete:   1,
			},
		},
	})
	out := sb.String()
	if !strings.Contains(out, "status:") {
		t.Fatalf("status output missing 'status:' line: %s", out)
	}
	if !strings.Contains(out, "10 active · 4 superseded · 1 obsolete") {
		t.Errorf("status output missing per-status counts: %s", out)
	}
}

// TestWriteStatusReport_ShowsZeroStatusCounts verifies missing statuses
// render as 0 rather than being omitted from the line.
func TestWriteStatusReport_ShowsZeroStatusCounts(t *testing.T) {
	var sb strings.Builder
	writeStatusReport(&sb, statusReport{
		dataDir: "/tmp",
		dbPath:  "/tmp/ion-mem.db",
		now:     time.Now().UTC(),
		stats: store.Stats{
			TotalObservations: 3,
			StatusCounts:      map[string]int64{store.StatusActive: 3},
		},
	})
	out := sb.String()
	if !strings.Contains(out, "3 active · 0 superseded · 0 obsolete") {
		t.Errorf("status output missing zero-filled statuses: %s", out)
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
