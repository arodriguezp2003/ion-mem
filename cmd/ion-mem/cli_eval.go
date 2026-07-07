package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ionix/ion-mem/internal/embed"
	"github.com/ionix/ion-mem/internal/eval"
	"github.com/ionix/ion-mem/internal/hybrid"
	"github.com/ionix/ion-mem/internal/store"
)

// evalConfig collects the parsed flags for the `eval` subcommand.
type evalConfig struct {
	golden     string // path to golden queries YAML (required)
	corpus     string // path to corpus YAML (optional; seeds a temp store when present)
	dataDir    string // path to existing store (used when --corpus is absent)
	project    string // project name for query scoping
	k          int    // precision cutoff (default 5)
	embeddings bool   // when true, use hybrid RRF searcher instead of BM25-only
	ollamaURL  string // Ollama base URL for embedding (default http://localhost:11434)
	model      string // embedding model name (default bge-m3)
}

// parseEvalFlags parses the `ion-mem eval` flag set.
// Returns an error when the required --golden flag is missing.
func parseEvalFlags(args []string, homeDir func() (string, error)) (evalConfig, error) {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	golden := fs.String("golden", "", "Path to golden queries YAML file (required).")
	corpus := fs.String("corpus", "", "Path to corpus YAML file; seeds a temp store and evaluates in isolation.")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for an existing store (used when --corpus is absent).")
	project := fs.String("project", "default", "Project name to scope queries.")
	k := fs.Int("k", 5, "Precision cutoff k (default 5).")
	embeddings := fs.Bool("embeddings", false, "Use hybrid RRF searcher (BM25 + vector) instead of BM25-only.")
	ollamaURL := fs.String("ollama-url", "http://localhost:11434", "Ollama base URL for embeddings (used when --embeddings is set).")
	model := fs.String("model", store.DefaultEmbeddingsModel, "Embedding model name (used when --embeddings is set).")

	if err := fs.Parse(args); err != nil {
		return evalConfig{}, fmt.Errorf("ion-mem eval: %w", err)
	}
	if *golden == "" {
		return evalConfig{}, fmt.Errorf("ion-mem eval: --golden is required")
	}
	if *k <= 0 {
		*k = 5
	}

	return evalConfig{
		golden:     *golden,
		corpus:     *corpus,
		dataDir:    *dataDir,
		project:    *project,
		k:          *k,
		embeddings: *embeddings,
		ollamaURL:  *ollamaURL,
		model:      *model,
	}, nil
}

// runEval implements the `ion-mem eval` subcommand.
//
//   - With --corpus: seeds a fresh temp store and evaluates in isolation (self-contained demo).
//   - Without --corpus: runs golden queries against the real store at --data-dir.
//
// Always exits 0; output is informational.
func runEval(args []string, out io.Writer) error {
	cfg, err := parseEvalFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	queries, err := eval.LoadGolden(cfg.golden)
	if err != nil {
		return fmt.Errorf("eval: load golden %q: %w", cfg.golden, err)
	}

	ctx := context.Background()

	var st *store.Store
	if cfg.corpus != "" {
		// Self-contained mode: seed a fresh temp store.
		tmpDir, err := os.MkdirTemp("", "ion-mem-eval-*")
		if err != nil {
			return fmt.Errorf("eval: create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		st, err = store.Open(tmpDir)
		if err != nil {
			return fmt.Errorf("eval: open temp store: %w", err)
		}
		defer st.Close()

		docs, err := eval.LoadCorpus(cfg.corpus)
		if err != nil {
			return fmt.Errorf("eval: load corpus %q: %w", cfg.corpus, err)
		}
		if err := eval.SeedCorpus(ctx, st, docs, cfg.project); err != nil {
			return fmt.Errorf("eval: seed corpus: %w", err)
		}
	} else {
		// Real-store mode.
		st, err = store.Open(cfg.dataDir)
		if err != nil {
			return fmt.Errorf("eval: open store %q: %w", cfg.dataDir, err)
		}
		defer st.Close()
	}

	var report eval.Report

	if cfg.embeddings {
		// Hybrid mode: backfill corpus embeddings (best-effort) then evaluate via RRF.
		client := embed.DefaultClient(cfg.ollamaURL)
		embedder := embed.NewOllamaEmbedder(client, cfg.model)

		if cfg.corpus != "" {
			// Best-effort: embed any observations that were just seeded.
			// MissingEmbeddings returns observations without a vector row.
			missing, _ := st.MissingEmbeddings(ctx, cfg.project, cfg.model, 500)
			for _, obs := range missing {
				text := obs.Title + "\n" + obs.Content
				embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				vec, embedErr := embedder.Embed(embedCtx, text)
				cancel()
				if embedErr != nil {
					// Best-effort: skip un-embeddable docs; they still rank via BM25.
					continue
				}
				_ = st.UpsertEmbedding(ctx, obs.ID, cfg.model, vec)
			}
		}

		searcher := hybrid.NewSearcher(st, embedder)

		searchFn := func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, error) {
			results, _, err := searcher.Search(ctx, params)
			return results, err
		}

		report, err = eval.RunWithSearchFn(ctx, searchFn, queries, cfg.project, cfg.k)
		if err != nil {
			return fmt.Errorf("eval: run with embeddings: %w", err)
		}
	} else {
		// Standard BM25-only path (unchanged for CI).
		report, err = eval.Run(ctx, st, queries, cfg.project, cfg.k)
		if err != nil {
			return fmt.Errorf("eval: run: %w", err)
		}
	}

	writeEvalReport(out, report, cfg.k)
	return nil
}

// writeEvalReport formats the evaluation report as an aligned plain-text table.
func writeEvalReport(out io.Writer, r eval.Report, k int) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "ion-mem eval — search quality report")
	fmt.Fprintln(out)

	// Per-query table.
	fmt.Fprintf(out, "%-5s  %-42s  %5s  %6s  %s\n", "ID", "Query", "MRR", fmt.Sprintf("P@%d", k), "Top Hit")
	fmt.Fprintln(out, strings.Repeat("-", 100))
	for _, qr := range r.PerQuery {
		fmt.Fprintf(out, "%-5s  %-42s  %5.3f  %6.3f  %s\n",
			qr.ID,
			truncate(qr.Query, 42),
			qr.MRR,
			qr.PrecisionK,
			truncate(qr.TopHit, 48),
		)
	}
	fmt.Fprintln(out)

	// Aggregate.
	fmt.Fprintf(out, "MeanMRR:  %.4f\n", r.MeanMRR)
	fmt.Fprintf(out, "MeanP@%d:  %.4f\n", k, r.MeanPrecisionAt5)
	fmt.Fprintln(out)

	// Known gaps.
	if len(r.KnownGaps) > 0 {
		fmt.Fprintln(out, "Known gaps (expect_fail=true — BM25 lexical gaps, embeddings targets)")
		fmt.Fprintln(out, strings.Repeat("-", 100))
		for _, qr := range r.KnownGaps {
			fmt.Fprintf(out, "%-5s  %-42s  %5.3f  %6.3f  %s\n",
				qr.ID,
				truncate(qr.Query, 42),
				qr.MRR,
				qr.PrecisionK,
				truncate(qr.TopHit, 48),
			)
		}
		fmt.Fprintln(out)
	}
}
