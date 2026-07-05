package eval_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/eval"
	"github.com/ionix/ion-mem/internal/store"
)

func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLoadCorpus_roundtrip(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "eval", "testdata", "corpus.yaml")
	// Use absolute path.
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	docs, err := eval.LoadCorpus(abs)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("LoadCorpus returned 0 docs")
	}
	// Spot-check first doc has required fields.
	for _, d := range docs {
		if d.Title == "" {
			t.Errorf("doc missing title: %+v", d)
		}
		if d.Content == "" {
			t.Errorf("doc %q missing content", d.Title)
		}
		if d.Type == "" {
			t.Errorf("doc %q missing type", d.Title)
		}
	}
}

func TestLoadGolden_roundtrip(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "eval", "testdata", "golden.yaml")
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	queries, err := eval.LoadGolden(abs)
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}
	if len(queries) == 0 {
		t.Fatal("LoadGolden returned 0 queries")
	}
	for _, q := range queries {
		if q.ID == "" {
			t.Errorf("query missing ID: %+v", q)
		}
		if q.Query == "" {
			t.Errorf("query %q missing query text", q.ID)
		}
		if !q.ExpectFail && len(q.Expected) == 0 {
			t.Errorf("query %q is not expect_fail but has no expected titles", q.ID)
		}
	}
}

func TestLoadCorpus_fileNotFound(t *testing.T) {
	_, err := eval.LoadCorpus("/nonexistent/path/corpus.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadGolden_fileNotFound(t *testing.T) {
	_, err := eval.LoadGolden("/nonexistent/path/golden.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSeedCorpus_insertsAllDocs(t *testing.T) {
	st := mustOpenStore(t)
	ctx := context.Background()

	docs := []eval.CorpusDoc{
		{Title: "doc alpha", Content: "alpha content", Type: "decision", AgeDays: 0},
		{Title: "doc beta", Content: "beta content", Type: "pattern", AgeDays: 5},
		{Title: "doc gamma", Content: "gamma content", Type: "architecture", TopicKey: "test/gamma", AgeDays: 30},
	}

	if err := eval.SeedCorpus(ctx, st, docs, "eval-test"); err != nil {
		t.Fatalf("SeedCorpus: %v", err)
	}

	// Verify all docs are findable via search.
	for _, d := range docs {
		results, _, err := st.SearchWithFallback(ctx, store.SearchParams{
			Q:       d.Title,
			Project: "eval-test",
		})
		if err != nil {
			t.Fatalf("SearchWithFallback %q: %v", d.Title, err)
		}
		if len(results) == 0 {
			t.Errorf("doc %q not found after SeedCorpus", d.Title)
		}
	}
}

func TestLoadCorpus_invalidYAML(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\tnot valid yaml: [unclosed"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := eval.LoadCorpus(bad)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
