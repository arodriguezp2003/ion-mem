package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/eval"
	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// evalConfig collects the parsed flags for the `eval` subcommand.
type evalConfig struct {
	golden       string              // path to golden queries YAML (required)
	corpus       string              // path to corpus YAML (optional; seeds a temp store when present)
	dataDir      string              // path to existing store (used when --corpus is absent)
	project      string              // default project for query scoping; "" = no project filter (--all-projects)
	k            int                 // precision/recall cutoff (default 5)
	mode         eval.Mode           // lexical, vector, or hybrid
	vectorWeight float64             // hybrid RRF vector-list weight (--mode hybrid only)
	fusionPolicy hybrid.FusionPolicy // hybrid fusion policy: all (default) or strict (--mode hybrid only)
	embedTimeout time.Duration
	embeddings   bool // legacy flag; true implies mode=hybrid when --mode is unset
	ollamaURL    string
	model        string
	jsonPath     string // "" = no JSON; "-" = stdout; else a file path
}

// parseEvalFlags parses the `ion-mem eval` flag set.
// Returns an error when the required --golden flag is missing, or when
// --mode is set to an unrecognized value.
func parseEvalFlags(args []string, homeDir func() (string, error)) (evalConfig, error) {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	golden := fs.String("golden", "", "Path to golden queries YAML file (required).")
	corpus := fs.String("corpus", "", "Path to corpus YAML file; seeds a temp store and evaluates in isolation.")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for an existing store (used when --corpus is absent).")
	projectFlag := fs.String("project", "default", "Project name to scope queries.")
	allProjects := fs.Bool("all-projects", false, "Do not filter by project (search across all projects). Overrides --project. Equivalent to --project=\"\".")
	k := fs.Int("k", 5, "Precision/recall cutoff k (default 5).")
	modeFlag := fs.String("mode", "", "Search mode: lexical, vector, or hybrid. Default: hybrid if --embeddings is set, else lexical.")
	vectorWeight := fs.Float64("vector-weight", hybrid.DefaultVectorWeight, "Vector-list weight in hybrid RRF fusion (--mode hybrid only).")
	fusionFlag := fs.String("fusion", string(hybrid.FusionAll), "Hybrid fusion policy: all (fuse everything, default) or strict (skip a fuzzy OR-fallback lexical list; --mode hybrid only).")
	embedTimeout := fs.Duration("embed-timeout", eval.DefaultEmbedTimeout, "Query-embedding timeout for --mode vector/hybrid (cold models can be slow).")
	embeddings := fs.Bool("embeddings", false, "Use hybrid RRF searcher (BM25 + vector) instead of BM25-only. Deprecated: use --mode hybrid.")
	ollamaURL := fs.String("ollama-url", "http://localhost:11434", "Ollama base URL for embeddings (used by --mode vector/hybrid).")
	model := fs.String("model", store.DefaultEmbeddingsModel, "Embedding model name (used by --mode vector/hybrid).")
	jsonPath := fs.String("json", "", "Write a JSON report to this path (use \"-\" for stdout). Omit for text-only output.")

	if err := parseFlagsWithHelp(fs, args, os.Stdout); err != nil {
		return evalConfig{}, fmt.Errorf("ion-mem eval: %w", err)
	}
	if *golden == "" {
		return evalConfig{}, fmt.Errorf("ion-mem eval: --golden is required")
	}
	if *k <= 0 {
		*k = 5
	}

	var mode eval.Mode
	switch {
	case *modeFlag != "":
		m, err := eval.ParseMode(*modeFlag)
		if err != nil {
			return evalConfig{}, fmt.Errorf("ion-mem eval: %w", err)
		}
		mode = m
	case *embeddings:
		mode = eval.ModeHybrid
	default:
		mode = eval.ModeLexical
	}

	project := *projectFlag
	if *allProjects {
		project = ""
	}

	fusionPolicy := hybrid.FusionPolicy(*fusionFlag)
	switch fusionPolicy {
	case hybrid.FusionAll, hybrid.FusionStrict:
		// valid
	default:
		return evalConfig{}, fmt.Errorf("ion-mem eval: unknown --fusion value %q (want %q or %q)", *fusionFlag, hybrid.FusionAll, hybrid.FusionStrict)
	}

	return evalConfig{
		golden:       *golden,
		corpus:       *corpus,
		dataDir:      *dataDir,
		project:      project,
		k:            *k,
		mode:         mode,
		vectorWeight: *vectorWeight,
		fusionPolicy: fusionPolicy,
		embedTimeout: *embedTimeout,
		embeddings:   *embeddings,
		ollamaURL:    *ollamaURL,
		model:        *model,
		jsonPath:     *jsonPath,
	}, nil
}

// runEval implements the `ion-mem eval` subcommand.
//
//   - With --corpus: seeds a fresh temp store and evaluates in isolation (self-contained demo).
//   - Without --corpus: runs golden queries against the real store at --data-dir.
//   - --mode vector/hybrid: requires a reachable Ollama server (checked via a
//     Ping before running any query) and errors out clearly when unavailable,
//     rather than silently degrading — hybrid mode's own per-query graceful
//     fallback (see internal/hybrid) only kicks in AFTER this initial check
//     passes, for transient per-query embedding failures.
//
// Always exits 0 once the check above passes; per-query output is informational.
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

	var embedder embed.Embedder
	if cfg.mode == eval.ModeVector || cfg.mode == eval.ModeHybrid {
		client := embed.DefaultClient(cfg.ollamaURL)

		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := client.Ping(pingCtx)
		cancel()
		if pingErr != nil {
			return fmt.Errorf("eval: --mode %s requires embeddings, but Ollama is unavailable at %s: %w", cfg.mode, cfg.ollamaURL, pingErr)
		}

		embedder = embed.NewOllamaEmbedder(client, cfg.model)

		if cfg.corpus != "" {
			if err := backfillEmbeddings(ctx, st, embedder, cfg, os.Stderr); err != nil {
				return err
			}
		}
	}

	search, err := eval.ModeSearchFn(cfg.mode, st, embedder, cfg.vectorWeight, cfg.embedTimeout, cfg.fusionPolicy)
	if err != nil {
		return fmt.Errorf("eval: %w", err)
	}

	report, err := eval.RunWithSearchFn(ctx, search, queries, cfg.project, cfg.k)
	if err != nil {
		return fmt.Errorf("eval: run: %w", err)
	}

	warnings := hybridFallbackWarnings(cfg.mode, report)
	skipped := lexicalSkippedIDs(cfg.mode, report)

	// --json=- means "JSON only, to stdout" (for piping to jq etc.): skip the
	// human-readable table so out carries pure JSON. --json=<path> writes the
	// file in addition to the usual table on out.
	if cfg.jsonPath != "-" {
		writeEvalReport(out, report, cfg, warnings, skipped)
	}

	if cfg.jsonPath != "" {
		if err := writeEvalJSON(out, cfg, report, warnings, skipped); err != nil {
			return fmt.Errorf("eval: write json: %w", err)
		}
	}

	return nil
}

// backfillEmbeddings embeds any observations missing a vector row for
// cfg.model (best-effort per document — an un-embeddable doc still ranks via
// BM25 in hybrid mode) and upserts each resulting vector into st, logging any
// upsert failure to stderr. It is the --corpus seeding path's bridge into
// vector/hybrid mode: without it, a freshly-seeded temp store would have no
// embeddings at all to search.
func backfillEmbeddings(ctx context.Context, st *store.Store, embedder embed.Embedder, cfg evalConfig, stderr io.Writer) error {
	missing, _ := st.MissingEmbeddings(ctx, cfg.project, cfg.model, 500)
	return backfillEmbeddingsWithUpsert(ctx, missing, embedder, cfg, stderr, st.UpsertEmbedding)
}

// backfillEmbeddingsWithUpsert is backfillEmbeddings with the upsert call
// injected, so the upsert-failure-handling logic below is unit-testable
// without a real store (see cli_eval_backfill_test.go).
//
// Each upsert failure is logged to stderr immediately (never silently
// swallowed) and counted; if EVERY attempted upsert failed, that indicates a
// systemic problem (e.g. a DB error), not merely a handful of un-embeddable
// docs, so the whole eval run is aborted with an error rather than silently
// proceeding to report a misleading vector/hybrid result against an empty
// index. A doc that fails to EMBED (as opposed to failing to upsert) never
// counts as an upsert attempt at all — that path already has its own
// best-effort skip, unrelated to this guard.
func backfillEmbeddingsWithUpsert(
	ctx context.Context,
	missing []store.Observation,
	embedder embed.Embedder,
	cfg evalConfig,
	stderr io.Writer,
	upsert func(ctx context.Context, obsID int64, model string, vec []float32) error,
) error {
	var upsertAttempts, upsertFailures int
	for _, obs := range missing {
		text := obs.Title + "\n" + obs.Content
		embedCtx, cancel := context.WithTimeout(ctx, cfg.embedTimeout)
		vec, embedErr := embedder.Embed(embedCtx, text)
		cancel()
		if embedErr != nil {
			// Best-effort: skip un-embeddable docs; hybrid mode still
			// ranks them via BM25, vector mode simply won't surface them.
			continue
		}

		upsertAttempts++
		if err := upsert(ctx, obs.ID, cfg.model, vec); err != nil {
			upsertFailures++
			fmt.Fprintf(stderr, "eval: upsert embedding for %q: %v\n", obs.Title, err)
			continue
		}
	}

	if upsertAttempts > 0 && upsertFailures == upsertAttempts {
		return fmt.Errorf("eval: all %d embedding upserts failed; refusing to run a misleading --mode %s eval", upsertAttempts, cfg.mode)
	}
	return nil
}

// hybridFallbackWarnings returns the IDs (across PerQuery and KnownGaps) of
// queries that silently fell back to BM25 when --mode hybrid was requested,
// i.e. QueryResult.Hybrid is false even though the whole run passed the
// upfront Ollama availability check in runEval. For any other mode this is
// always nil: ModeLexical never sets Hybrid, and ModeVector has no fallback
// path (its errors are hard failures, not silent degradation).
//
// QueryResult.LexicalSkipped is explicitly excluded: under --fusion strict,
// a fuzzy-fallback lexical list is DELIBERATELY dropped (see
// hybrid.FusionStrict), which also sets Hybrid=false — that is expected
// policy behavior, not a silent failure, so it must not be reported as a
// warning here (see lexicalSkippedIDs for that, reported separately).
func hybridFallbackWarnings(mode eval.Mode, r eval.Report) []string {
	if mode != eval.ModeHybrid {
		return nil
	}
	var ids []string
	for _, qr := range r.PerQuery {
		if !qr.Hybrid && !qr.LexicalSkipped {
			ids = append(ids, qr.ID)
		}
	}
	for _, qr := range r.KnownGaps {
		if !qr.Hybrid && !qr.LexicalSkipped {
			ids = append(ids, qr.ID)
		}
	}
	return ids
}

// lexicalSkippedIDs returns the IDs (across PerQuery and KnownGaps) of
// queries where --fusion strict skipped a fuzzy-fallback lexical list (see
// hybrid.FusionStrict). Always nil outside --mode hybrid --fusion strict.
func lexicalSkippedIDs(mode eval.Mode, r eval.Report) []string {
	if mode != eval.ModeHybrid {
		return nil
	}
	var ids []string
	for _, qr := range r.PerQuery {
		if qr.LexicalSkipped {
			ids = append(ids, qr.ID)
		}
	}
	for _, qr := range r.KnownGaps {
		if qr.LexicalSkipped {
			ids = append(ids, qr.ID)
		}
	}
	return ids
}

// writeEvalReport formats the evaluation report as an aligned plain-text table.
func writeEvalReport(out io.Writer, r eval.Report, cfg evalConfig, warnings, skipped []string) {
	fmt.Fprintln(out)
	if cfg.mode == eval.ModeHybrid {
		fmt.Fprintf(out, "ion-mem eval — search quality report (mode=%s, fusion=%s, k=%d)\n", cfg.mode, cfg.fusionPolicy, cfg.k)
	} else {
		fmt.Fprintf(out, "ion-mem eval — search quality report (mode=%s, k=%d)\n", cfg.mode, cfg.k)
	}
	fmt.Fprintln(out)

	writeEvalTable(out, r.PerQuery, cfg.k)
	fmt.Fprintln(out)

	// Aggregate.
	fmt.Fprintf(out, "MeanMRR:          %.4f\n", r.MeanMRR)
	fmt.Fprintf(out, "MeanHit@1:        %.4f\n", r.MeanHitAt1)
	fmt.Fprintf(out, "MeanP@%d:         %.4f\n", cfg.k, r.MeanPrecisionAt5)
	fmt.Fprintf(out, "MeanRecall@%d:    %.4f\n", cfg.k, r.MeanRecallAtK)
	fmt.Fprintf(out, "MeanRecall@10:    %.4f\n", r.MeanRecallAt10)
	fmt.Fprintf(out, "MeanNDCG@10:      %.4f\n", r.MeanNDCGAt10)
	fmt.Fprintf(out, "Latency p50/p95/mean: %s / %s / %s\n", r.Latency.P50, r.Latency.P95, r.Latency.Mean)
	if r.Errors > 0 {
		total := len(r.PerQuery) + len(r.KnownGaps)
		fmt.Fprintf(out, "Errors:           %d/%d (%s)\n", r.Errors, total, strings.Join(r.ErrorIDs, ", "))
	}
	if cfg.mode == eval.ModeHybrid {
		total := len(r.PerQuery) + len(r.KnownGaps)
		fmt.Fprintf(out, "HybridRan:        %d/%d\n", r.HybridRan, total)
		if cfg.fusionPolicy == hybrid.FusionStrict {
			fmt.Fprintf(out, "LexicalSkipped:   %d/%d (fuzzy fallback dropped under --fusion strict)\n", r.LexicalSkipped, total)
		}
	}
	fmt.Fprintln(out)

	if len(warnings) > 0 {
		fmt.Fprintf(out, "WARNING: %d hybrid quer%s silently fell back to BM25 (embed/vector error): %s\n",
			len(warnings), pluralY(len(warnings)), strings.Join(warnings, ", "))
		fmt.Fprintln(out)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(out, "NOTE: %d quer%s skipped a fuzzy-fallback lexical list under --fusion strict (vector ranking used instead): %s\n",
			len(skipped), pluralY(len(skipped)), strings.Join(skipped, ", "))
		fmt.Fprintln(out)
	}
	writeQueryErrorWarnings(out, r)

	// Known gaps.
	if len(r.KnownGaps) > 0 {
		fmt.Fprintln(out, "Known gaps (expect_fail=true — BM25 lexical gaps, embeddings targets)")
		fmt.Fprintln(out, strings.Repeat("-", 100))
		writeEvalTable(out, r.KnownGaps, cfg.k)
		fmt.Fprintln(out)
	}
}

// writeQueryErrorWarnings prints one "WARN: query <id> failed: <err>" line
// per query (across PerQuery and KnownGaps) whose SearchFn call errored (see
// eval.QueryResult.Err). These are per-query failures RunWithSearchFn
// tolerated and kept running past — see eval.Report.Errors/ErrorIDs for the
// summary counts also surfaced in the JSON aggregate.
func writeQueryErrorWarnings(out io.Writer, r eval.Report) {
	var printed bool
	for _, qr := range r.PerQuery {
		if qr.Err != "" {
			fmt.Fprintf(out, "WARN: query %s failed: %s\n", qr.ID, qr.Err)
			printed = true
		}
	}
	for _, qr := range r.KnownGaps {
		if qr.Err != "" {
			fmt.Fprintf(out, "WARN: query %s failed: %s\n", qr.ID, qr.Err)
			printed = true
		}
	}
	if printed {
		fmt.Fprintln(out)
	}
}

// pluralY returns "ies" for n!=1 and "y" for n==1, e.g. "1 quer"+pluralY(1) = "1 query".
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// writeEvalTable renders one aligned per-query table.
func writeEvalTable(out io.Writer, rows []eval.QueryResult, k int) {
	fmt.Fprintf(out, "%-5s  %-32s  %5s  %5s  %6s  %6s  %7s  %8s  %-6s  %s\n",
		"ID", "Query", "MRR", "Hit1", fmt.Sprintf("P@%d", k), "Rec@10", "NDCG@10", "Latency", "Hybrid", "Top Hit")
	fmt.Fprintln(out, strings.Repeat("-", 110))
	for _, qr := range rows {
		hybridCol := "-"
		switch {
		case qr.LexicalSkipped:
			hybridCol = "skip"
		case qr.Hybrid:
			hybridCol = "yes"
		}
		topHitCol := qr.TopHit
		if qr.Err != "" {
			topHitCol = "ERR: " + qr.Err
		}
		fmt.Fprintf(out, "%-5s  %-32s  %5.3f  %5.3f  %6.3f  %6.3f  %7.3f  %8s  %-6s  %s\n",
			qr.ID,
			truncate(qr.Query, 32),
			qr.MRR,
			qr.HitAt1,
			qr.PrecisionK,
			qr.RecallAt10,
			qr.NDCGAt10,
			qr.Latency.Round(time.Millisecond),
			hybridCol,
			truncate(topHitCol, 40),
		)
	}
}

// ─── JSON report ───────────────────────────────────────────────────────────

type jsonLatencyMS struct {
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Mean float64 `json:"mean"`
}

type jsonAggregate struct {
	MeanMRR                float64       `json:"mean_mrr"`
	HitAt1                 float64       `json:"hit_at_1"`
	MeanPAtK               float64       `json:"mean_p_at_k"`
	MeanRecallAtK          float64       `json:"mean_recall_at_k"`
	MeanRecallAt10         float64       `json:"mean_recall_at_10"`
	MeanNDCGAt10           float64       `json:"mean_ndcg_at_10"`
	LatencyMS              jsonLatencyMS `json:"latency_ms"`
	HybridRan              int           `json:"hybrid_ran"`
	Queries                int           `json:"queries"`
	KnownGaps              int           `json:"known_gaps"`
	HybridFallbackWarnings []string      `json:"hybrid_fallback_warnings,omitempty"`
	// LexicalSkipped counts queries where --fusion strict dropped a
	// fuzzy-fallback lexical list (see hybrid.FusionStrict). 0 outside
	// --mode hybrid --fusion strict.
	LexicalSkipped    int      `json:"lexical_skipped"`
	LexicalSkippedIDs []string `json:"lexical_skipped_ids,omitempty"`
	// Errors counts queries whose SearchFn call errored (see
	// eval.QueryResult.Err); ErrorIDs lists their GoldenQuery.ID values. A
	// per-query error does not abort the run — see eval.RunWithSearchFn.
	Errors   int      `json:"errors"`
	ErrorIDs []string `json:"error_ids,omitempty"`
}

type jsonTopResult struct {
	Rank    int     `json:"rank"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Project string  `json:"project"`
}

type jsonQueryResult struct {
	ID             string          `json:"id"`
	Query          string          `json:"query"`
	Project        string          `json:"project"`
	Category       string          `json:"category,omitempty"`
	Expected       []string        `json:"expected"`
	ExpectFail     bool            `json:"expect_fail"`
	MRR            float64         `json:"mrr"`
	Hit1           float64         `json:"hit1"`
	PAtK           float64         `json:"p_at_k"`
	RecallAtK      float64         `json:"recall_at_k"`
	RecallAt10     float64         `json:"recall_at_10"`
	NDCGAt10       float64         `json:"ndcg_at_10"`
	LatencyMS      float64         `json:"latency_ms"`
	Hybrid         bool            `json:"hybrid"`
	Fuzzy          bool            `json:"fuzzy"`
	LexicalSkipped bool            `json:"lexical_skipped"`
	Err            string          `json:"err,omitempty"`
	Top            []jsonTopResult `json:"top"`
}

type jsonReport struct {
	Mode         string            `json:"mode"`
	VectorWeight float64           `json:"vector_weight,omitempty"`
	Fusion       string            `json:"fusion,omitempty"`
	Model        string            `json:"model,omitempty"`
	K            int               `json:"k"`
	Project      string            `json:"project"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Aggregate    jsonAggregate     `json:"aggregate"`
	PerQuery     []jsonQueryResult `json:"per_query"`
}

// toJSONQueryResult converts one eval.QueryResult into its JSON DTO.
func toJSONQueryResult(qr eval.QueryResult) jsonQueryResult {
	top := make([]jsonTopResult, 0, len(qr.Top))
	for _, t := range qr.Top {
		top = append(top, jsonTopResult{Rank: t.Rank, Title: t.Title, Score: t.Score, Project: t.Project})
	}
	return jsonQueryResult{
		ID:             qr.ID,
		Query:          qr.Query,
		Project:        qr.Project,
		Category:       qr.Category,
		Expected:       qr.Expected,
		ExpectFail:     qr.ExpectFail,
		MRR:            qr.MRR,
		Hit1:           qr.HitAt1,
		PAtK:           qr.PrecisionK,
		RecallAtK:      qr.RecallAtK,
		RecallAt10:     qr.RecallAt10,
		NDCGAt10:       qr.NDCGAt10,
		LatencyMS:      durationMS(qr.Latency),
		Hybrid:         qr.Hybrid,
		Fuzzy:          qr.Fuzzy,
		LexicalSkipped: qr.LexicalSkipped,
		Err:            qr.Err,
		Top:            top,
	}
}

// buildJSONReport assembles the full JSON DTO from an eval.Report.
func buildJSONReport(cfg evalConfig, r eval.Report, warnings, skipped []string) jsonReport {
	perQuery := make([]jsonQueryResult, 0, len(r.PerQuery)+len(r.KnownGaps))
	for _, qr := range r.PerQuery {
		perQuery = append(perQuery, toJSONQueryResult(qr))
	}
	for _, qr := range r.KnownGaps {
		perQuery = append(perQuery, toJSONQueryResult(qr))
	}

	var vectorWeight float64
	var fusion string
	if cfg.mode == eval.ModeHybrid {
		vectorWeight = cfg.vectorWeight
		fusion = string(cfg.fusionPolicy)
	}
	var model string
	if cfg.mode == eval.ModeVector || cfg.mode == eval.ModeHybrid {
		model = cfg.model
	}

	return jsonReport{
		Mode:         string(cfg.mode),
		VectorWeight: vectorWeight,
		Fusion:       fusion,
		Model:        model,
		K:            cfg.k,
		Project:      cfg.project,
		GeneratedAt:  time.Now().UTC(),
		Aggregate: jsonAggregate{
			MeanMRR:        r.MeanMRR,
			HitAt1:         r.MeanHitAt1,
			MeanPAtK:       r.MeanPrecisionAt5,
			MeanRecallAtK:  r.MeanRecallAtK,
			MeanRecallAt10: r.MeanRecallAt10,
			MeanNDCGAt10:   r.MeanNDCGAt10,
			LatencyMS: jsonLatencyMS{
				P50:  durationMS(r.Latency.P50),
				P95:  durationMS(r.Latency.P95),
				Mean: durationMS(r.Latency.Mean),
			},
			HybridRan:              r.HybridRan,
			Queries:                len(r.PerQuery),
			KnownGaps:              len(r.KnownGaps),
			HybridFallbackWarnings: warnings,
			LexicalSkipped:         r.LexicalSkipped,
			LexicalSkippedIDs:      skipped,
			Errors:                 r.Errors,
			ErrorIDs:               r.ErrorIDs,
		},
		PerQuery: perQuery,
	}
}

// writeEvalJSON marshals the report as indented JSON to cfg.jsonPath, or to
// out (runEval's output writer) when cfg.jsonPath is "-".
func writeEvalJSON(out io.Writer, cfg evalConfig, r eval.Report, warnings, skipped []string) error {
	data, err := json.MarshalIndent(buildJSONReport(cfg, r, warnings, skipped), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')

	if cfg.jsonPath == "-" {
		_, err := out.Write(data)
		return err
	}
	return os.WriteFile(cfg.jsonPath, data, 0o644)
}

// durationMS converts a time.Duration to milliseconds as a float64, matching
// the fractional precision typically wanted for latency reporting (e.g.
// 1.5ms rather than being truncated to 1 or rounded up to 2).
func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
