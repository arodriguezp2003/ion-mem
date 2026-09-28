package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
	"github.com/arodriguezp2003/ion-mem/internal/joblog"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// backfillConfig collects the parsed flags for the `backfill-embeddings` subcommand.
type backfillConfig struct {
	dataDir string
	project string
	batch   int

	// verbose is nil unless --verbose was explicitly passed on the command
	// line, so joblog.ResolveVerbose can distinguish "not passed" (fall
	// through to the ION_MEM_VERBOSE env var and the log.verbose setting)
	// from an explicit --verbose or --verbose=false.
	verbose *bool
}

// parseBackfillFlags parses the `ion-mem backfill-embeddings` flag set.
func parseBackfillFlags(args []string, homeDir func() (string, error)) (backfillConfig, error) {
	fs := flag.NewFlagSet("backfill-embeddings", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	project := fs.String("project", "", "Project to backfill (all projects when empty).")
	batch := fs.Int("batch", 50, "Number of observations to embed per batch.")
	verbose := fs.Bool("verbose", false,
		"Enable DEBUG-level job logging (overrides the log.verbose setting and ION_MEM_VERBOSE).")

	if err := parseFlagsWithHelp(fs, args, os.Stdout); err != nil {
		return backfillConfig{}, fmt.Errorf("ion-mem backfill-embeddings: %w", err)
	}

	cfg := backfillConfig{
		dataDir: *dataDir,
		project: *project,
		batch:   *batch,
	}

	// fs.Visit only calls back for flags that were actually set on the
	// command line, which lets us keep cfg.verbose nil when --verbose was
	// never passed.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "verbose" {
			v := *verbose
			cfg.verbose = &v
		}
	})

	return cfg, nil
}

// progressEvery controls how many successful embeds elapse between interim
// "embedded X/Y" progress lines.
const progressEvery = 10

// backfillSleep overrides embedjob's retry-backoff sleep for tests only. It
// is nil in production, which makes embedjob.Run fall back to its own
// default (a real context-aware timer). Production deliberately keeps
// embedjob's default backoff ({1s, 3s}) rather than a short one: Ollama can
// take a while to cold-load a model like bge-m3 into memory, and the real
// wait between attempts gives it a chance to finish loading instead of
// hammering it immediately after the first failure.
var backfillSleep func(ctx context.Context, d time.Duration) error

// runBackfill implements the `ion-mem backfill-embeddings` subcommand.
//
// It reads embeddings settings from the store and delegates the actual
// fetch → embed → upsert loop to internal/embedjob, which owns retry,
// backoff, the consecutive-failure abort guard, and a warm-up probe. This
// function's job is to translate embedjob.Event values into CLI output and
// an exit status, and to wire Ctrl-C/SIGTERM into the job's context so it
// stops cleanly instead of leaving a partial batch mid-flight.
//
// Job progress and failures are additionally recorded to a joblog (see
// internal/joblog) at <data-dir>/logs/embeddings.log; a failure to open that
// log file is non-fatal (falls back to joblog.Nop()) since job logging must
// never prevent the backfill itself from running.
func runBackfill(args []string, out io.Writer) error {
	cfg, err := parseBackfillFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("backfill-embeddings: open store: %w", err)
	}
	defer st.Close()

	ctx, cancel := signalNotifyContext(context.Background())
	defer cancel()

	enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false")
	if enabled != "true" {
		return fmt.Errorf("backfill-embeddings: embeddings.enabled is not set to 'true'; " +
			"enable embeddings in the config view first (ion-mem dash → C → EMBEDDINGS)")
	}

	ollamaURL := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
	model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)

	verbose := joblog.ResolveVerbose(
		st.SettingOrDefault(ctx, store.SettingLogVerbose, "false"),
		os.Getenv("ION_MEM_VERBOSE"),
		cfg.verbose,
	)

	lg, err := joblog.Open(cfg.dataDir, "embeddings", verbose)
	if err != nil {
		fmt.Fprintf(out, "WARN: joblog.Open: %v\n", err)
		lg = joblog.Nop()
	}
	defer lg.Close()

	fmt.Fprintf(out, "log: %s\n", lg.Path())

	client := embed.DefaultClient(ollamaURL)
	embedder := embed.NewOllamaEmbedder(client, model)

	events := embedjob.Run(ctx, st, embedder, lg, embedjob.Config{
		Kind:    embedjob.KindEmbedMissing,
		Project: cfg.project,
		Batch:   cfg.batch,
		Retries: embedjob.DefaultRetries,
		WarmUp:  true,
		Sleep:   backfillSleep,
	})

	var summary *embedjob.Summary
	successCount := 0

	for ev := range events {
		switch ev.Type {
		case embedjob.EventWarmedUp:
			fmt.Fprintf(out, "warm-up: %s ready in %s\n", model, ev.Duration)
		case embedjob.EventItem:
			if ev.Err != nil {
				fmt.Fprintf(out, "WARN: embed %d %s: %v (attempts=%d)\n", ev.ID, ev.Title, ev.Err, ev.Attempt)
				continue
			}
			successCount++
			if successCount%progressEvery == 0 {
				fmt.Fprintf(out, "embedded %d/%d …\n", ev.Done, ev.Total)
			}
		case embedjob.EventDone:
			summary = ev.Summary
		}
	}

	if summary == nil {
		return fmt.Errorf("backfill-embeddings: job produced no summary")
	}

	fmt.Fprintf(out, "embedded %d/%d …\n", summary.Done, summary.Total)

	switch summary.Status {
	case embedjob.StatusComplete:
		fmt.Fprintf(out, "done: embedded %d/%d observations with model %q\n", summary.Done, summary.Total, model)
		return nil
	case embedjob.StatusPartial:
		fmt.Fprintf(out, "partial: %d/%d embedded, %d failed — see %s\n",
			summary.Done, summary.Total, summary.Failed, summary.LogPath)
		return fmt.Errorf("backfill-embeddings: partial: %d/%d embedded, %d failed",
			summary.Done, summary.Total, summary.Failed)
	case embedjob.StatusAborted:
		fmt.Fprintf(out, "aborted: %v — see %s\n", summary.LastErr, summary.LogPath)
		return fmt.Errorf("backfill-embeddings: aborted: %w", summary.LastErr)
	case embedjob.StatusStopped:
		fmt.Fprintf(out, "stopped: %d/%d\n", summary.Done, summary.Total)
		// Returning context.Canceled here is intentional, not an oversight:
		// main.go treats errors.Is(err, context.Canceled) as a graceful
		// shutdown and exits 0 without printing an "ion-mem: <err>" line, so
		// Ctrl-C/SIGTERM produces the "stopped:" line above and a clean exit.
		if errors.Is(summary.LastErr, context.Canceled) {
			return summary.LastErr
		}
		return nil
	default:
		return fmt.Errorf("backfill-embeddings: unexpected job status %v", summary.Status)
	}
}
