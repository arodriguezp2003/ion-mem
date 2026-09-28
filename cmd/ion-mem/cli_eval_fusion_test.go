package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
)

// ─── flag parsing ──────────────────────────────────────────────────────────

func TestParseEvalFlags_fusion_defaultsToAll(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.fusionPolicy != hybrid.FusionAll {
		t.Errorf("cfg.fusionPolicy = %q, want %q (production default; no behavior change without --fusion)", cfg.fusionPolicy, hybrid.FusionAll)
	}
}

func TestParseEvalFlags_fusion_strict(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--fusion=strict"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.fusionPolicy != hybrid.FusionStrict {
		t.Errorf("cfg.fusionPolicy = %q, want %q", cfg.fusionPolicy, hybrid.FusionStrict)
	}
}

func TestParseEvalFlags_fusion_explicitAll(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--fusion=all"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.fusionPolicy != hybrid.FusionAll {
		t.Errorf("cfg.fusionPolicy = %q, want %q", cfg.fusionPolicy, hybrid.FusionAll)
	}
}

func TestParseEvalFlags_fusion_invalidValueErrors(t *testing.T) {
	_, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--fusion=loose"}, fakeHome)
	if err == nil {
		t.Fatal("expected error for invalid --fusion value")
	}
	if !strings.Contains(err.Error(), "loose") {
		t.Errorf("error %q should mention the invalid --fusion value", err.Error())
	}
}

// ─── end-to-end ────────────────────────────────────────────────────────────

// TestRunEval_ModeHybrid_FusionStrict_SkipsFuzzyQuery runs the bundled golden
// set (which includes Q08/Q09 — queries documented in golden.yaml as forcing
// BM25's OR fuzzy fallback via one nonexistent term) under --mode=hybrid
// --fusion=strict, and verifies via JSON output that at least one fuzzy
// query has lexical_skipped=true and hybrid=false (vector-only ranking),
// while the top-level "fusion" field reports "strict".
func TestRunEval_ModeHybrid_FusionStrict_SkipsFuzzyQuery(t *testing.T) {
	vec := []float32{0.3, 0.4, 0.1, 0.2}
	srv := fakeOllamaServer(vec)
	defer srv.Close()

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=fusion-strict-e2e",
		"--data-dir=" + t.TempDir(),
		"--mode=hybrid",
		"--fusion=strict",
		"--ollama-url=" + srv.URL,
		"--model=fake-model",
		"--json=-",
	}, &sb)
	if err != nil {
		t.Fatalf("runEval: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(sb.String()), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, sb.String())
	}

	if parsed["fusion"] != "strict" {
		t.Errorf("fusion = %v, want strict", parsed["fusion"])
	}

	agg := parsed["aggregate"].(map[string]any)
	skippedCount, _ := agg["lexical_skipped"].(float64)
	if skippedCount == 0 {
		t.Fatalf("aggregate.lexical_skipped = %v, want > 0 (Q08/Q09 in golden.yaml force the fuzzy OR fallback): %+v", agg["lexical_skipped"], agg)
	}

	perQuery := parsed["per_query"].([]any)
	var foundSkipped bool
	for _, raw := range perQuery {
		qr := raw.(map[string]any)
		if qr["lexical_skipped"] == true {
			foundSkipped = true
			if qr["hybrid"] != false {
				t.Errorf("query %v: lexical_skipped=true should imply hybrid=false, got hybrid=%v", qr["id"], qr["hybrid"])
			}
		}
	}
	if !foundSkipped {
		t.Error("expected at least one per_query entry with lexical_skipped=true")
	}
}

// TestRunEval_ModeHybrid_FusionAll_DefaultBehaviorUnchanged verifies that
// running with the default fusion policy (--fusion omitted) never sets
// lexical_skipped, preserving current production behavior byte-for-byte in
// spirit (same fields, always false/0).
func TestRunEval_ModeHybrid_FusionAll_DefaultBehaviorUnchanged(t *testing.T) {
	vec := []float32{0.3, 0.4, 0.1, 0.2}
	srv := fakeOllamaServer(vec)
	defer srv.Close()

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=fusion-all-e2e",
		"--data-dir=" + t.TempDir(),
		"--mode=hybrid",
		"--ollama-url=" + srv.URL,
		"--model=fake-model",
		"--json=-",
	}, &sb)
	if err != nil {
		t.Fatalf("runEval: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(sb.String()), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, sb.String())
	}
	if parsed["fusion"] != "all" {
		t.Errorf("fusion = %v, want all (default)", parsed["fusion"])
	}
	agg := parsed["aggregate"].(map[string]any)
	if agg["lexical_skipped"] != float64(0) {
		t.Errorf("aggregate.lexical_skipped = %v, want 0 under default FusionAll", agg["lexical_skipped"])
	}
	for _, raw := range parsed["per_query"].([]any) {
		qr := raw.(map[string]any)
		if qr["lexical_skipped"] == true {
			t.Errorf("query %v: lexical_skipped should always be false under FusionAll", qr["id"])
		}
	}
}
