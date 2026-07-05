package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ionix/ion-mem/internal/embed"
	"github.com/ionix/ion-mem/internal/store"
)

// backfillConfig collects the parsed flags for the `backfill-embeddings` subcommand.
type backfillConfig struct {
	dataDir string
	project string
	batch   int
}

// parseBackfillFlags parses the `ion-mem backfill-embeddings` flag set.
func parseBackfillFlags(args []string, homeDir func() (string, error)) (backfillConfig, error) {
	fs := flag.NewFlagSet("backfill-embeddings", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	project := fs.String("project", "", "Project to backfill (all projects when empty).")
	batch := fs.Int("batch", 50, "Number of observations to embed per batch.")

	if err := fs.Parse(args); err != nil {
		return backfillConfig{}, fmt.Errorf("ion-mem backfill-embeddings: %w", err)
	}

	return backfillConfig{
		dataDir: *dataDir,
		project: *project,
		batch:   *batch,
	}, nil
}

// runBackfill implements the `ion-mem backfill-embeddings` subcommand.
//
// It reads embeddings settings from the store, embeds all observations that
// lack a vector row, and prints progress to out. Exits with an error if
// embeddings.enabled is not set to "true".
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

	ctx := context.Background()

	enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false")
	if enabled != "true" {
		return fmt.Errorf("backfill-embeddings: embeddings.enabled is not set to 'true'; " +
			"enable embeddings in the config view first (ion-mem dash → C → EMBEDDINGS)")
	}

	ollamaURL := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
	model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, "nomic-embed-text")

	client := embed.DefaultClient(ollamaURL)
	embedder := embed.NewOllamaEmbedder(client, model)

	project := cfg.project
	batch := cfg.batch
	if batch <= 0 {
		batch = 50
	}

	var totalEmbedded int
	var lastEmbedErr error

	for {
		missing, err := st.MissingEmbeddings(ctx, project, model, batch)
		if err != nil {
			return fmt.Errorf("backfill-embeddings: fetch missing: %w", err)
		}
		if len(missing) == 0 {
			break
		}

		batchSucceeded := 0
		for _, obs := range missing {
			text := obs.Title + "\n" + obs.Content

			embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			vec, embedErr := embedder.Embed(embedCtx, text)
			cancel()

			if embedErr != nil {
				lastEmbedErr = embedErr
				fmt.Fprintf(out, "WARN: embed %d %q: %v\n", obs.ID, obs.Title, embedErr)
				continue
			}

			if err := st.UpsertEmbedding(ctx, obs.ID, model, vec); err != nil {
				fmt.Fprintf(out, "WARN: upsert %d: %v\n", obs.ID, err)
				continue
			}

			totalEmbedded++
			batchSucceeded++
		}

		// Print interim progress.
		have, total, _ := st.EmbeddingCoverage(ctx, project, model)
		fmt.Fprintf(out, "embedded %d/%d …\n", have, total)

		if len(missing) < batch {
			// Last batch — done.
			break
		}

		// Zero-progress guard: a full batch was fetched but nothing succeeded.
		// Without this, the same unembeddable rows would be re-fetched forever.
		if batchSucceeded == 0 {
			fmt.Fprintf(out, "aborted: no progress in last batch — last error: %v\n", lastEmbedErr)
			return fmt.Errorf("backfill-embeddings: aborted: no progress in last batch: %w", lastEmbedErr)
		}
	}

	// Final coverage line.
	have, total, err := st.EmbeddingCoverage(ctx, project, model)
	if err != nil {
		return fmt.Errorf("backfill-embeddings: coverage: %w", err)
	}
	fmt.Fprintf(out, "done: embedded %d/%d observations with model %q\n", have, total, model)
	return nil
}
