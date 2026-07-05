package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// evalTestdataPath resolves the eval package's testdata fixtures from this
// source file's location, so the test is independent of the working directory.
func evalTestdataPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "eval", "testdata", name)
}

// TestParseEvalFlags_corpusAndGolden verifies both required fields when
// --corpus is provided.
func TestParseEvalFlags_corpusAndGolden(t *testing.T) {
	cfg, err := parseEvalFlags([]string{
		"--golden=/tmp/g.yaml",
		"--corpus=/tmp/c.yaml",
		"--project=test",
		"--k=10",
		"--data-dir=/tmp/db",
	}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.golden != "/tmp/g.yaml" {
		t.Errorf("golden = %q, want /tmp/g.yaml", cfg.golden)
	}
	if cfg.corpus != "/tmp/c.yaml" {
		t.Errorf("corpus = %q, want /tmp/c.yaml", cfg.corpus)
	}
	if cfg.project != "test" {
		t.Errorf("project = %q, want test", cfg.project)
	}
	if cfg.k != 10 {
		t.Errorf("k = %d, want 10", cfg.k)
	}
	if cfg.dataDir != "/tmp/db" {
		t.Errorf("dataDir = %q, want /tmp/db", cfg.dataDir)
	}
}

// TestParseEvalFlags_missingGolden verifies that omitting --golden is an error.
func TestParseEvalFlags_missingGolden(t *testing.T) {
	_, err := parseEvalFlags([]string{}, fakeHome)
	if err == nil {
		t.Fatal("expected error when --golden is missing")
	}
	if !strings.Contains(err.Error(), "golden") {
		t.Errorf("error %q should mention 'golden'", err.Error())
	}
}

// TestParseEvalFlags_defaults verifies default k=5.
func TestParseEvalFlags_defaults(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.k != 5 {
		t.Errorf("k default = %d, want 5", cfg.k)
	}
	if cfg.project != "default" {
		t.Errorf("project default = %q, want default", cfg.project)
	}
}

// TestRouteCommand_eval_missingGolden verifies that eval without --golden errors.
func TestRouteCommand_eval_missingGolden(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "eval"}, nil)
	if err == nil {
		t.Fatal("expected error when no --golden given")
	}
}

// TestUsage_containsEval verifies that usage() mentions the eval command.
func TestUsage_containsEval(t *testing.T) {
	u := usage()
	if !strings.Contains(u, "eval") {
		t.Error("usage() does not mention eval command")
	}
}

// TestRunEval_withCorpus verifies the self-contained demo path (--corpus provided).
// Uses the real testdata fixtures bundled with the eval package.
func TestRunEval_withCorpus(t *testing.T) {
	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=cli-test",
		"--k=5",
		"--data-dir=" + t.TempDir(),
	}, &sb)
	if err != nil {
		t.Fatalf("runEval with corpus: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "MeanMRR") {
		t.Errorf("output missing MeanMRR; got:\n%s", out)
	}
	if !strings.Contains(out, "MeanP@") {
		t.Errorf("output missing MeanP@; got:\n%s", out)
	}
	if !strings.Contains(out, "Known gaps") {
		t.Errorf("output missing Known gaps section; got:\n%s", out)
	}
}
