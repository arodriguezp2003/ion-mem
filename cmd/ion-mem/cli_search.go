// `ion-mem search` subcommand — one-shot search outside MCP/TUI.
//
// Usage:
//
//	ion-mem search <query> [--project=...] [--all-projects] [--limit=10]
//	               [--type=...] [--data-dir=...] [--json]
//
// Flags come first; the remaining positional arguments are joined as the query.
// Output: aligned table ID  TYPE  TITLE  SCORE plus a footer with count and
// fuzzy/hybrid chips. --json emits a JSON array instead. Errors go to stderr;
// non-zero exit on store open failure; zero results exits 0 with "no results".
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ionix/ion-mem/internal/hybrid"
	"github.com/ionix/ion-mem/internal/store"
)

// searchConfig collects the parsed flags for the `search` subcommand.
type searchConfig struct {
	query       string
	project     string
	allProjects bool
	limit       int
	obsType     string
	dataDir     string
	jsonOut     bool
}

// parseSearchFlags parses the `ion-mem search` flag set.
// Flags are parsed first; remaining positional arguments are joined as the query.
func parseSearchFlags(args []string, homeDir func() (string, error)) (searchConfig, error) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	project := fs.String("project", "", "Filter to a specific project.")
	allProjects := fs.Bool("all-projects", false, "Search across all projects (overrides --project).")
	limit := fs.Int("limit", 10, "Maximum number of results to return.")
	obsType := fs.String("type", "", "Filter by observation type (e.g. decision, bugfix).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	jsonOut := fs.Bool("json", false, "Emit results as a JSON array instead of a table.")

	if err := fs.Parse(args); err != nil {
		return searchConfig{}, fmt.Errorf("ion-mem search: %w", err)
	}

	// Remaining positional arguments form the query.
	query := strings.Join(fs.Args(), " ")
	query = strings.TrimSpace(query)
	if query == "" {
		return searchConfig{}, fmt.Errorf("ion-mem search: query is required")
	}

	if *limit <= 0 {
		return searchConfig{}, fmt.Errorf("ion-mem search: --limit must be positive (got %d)", *limit)
	}

	return searchConfig{
		query:       query,
		project:     *project,
		allProjects: *allProjects,
		limit:       *limit,
		obsType:     *obsType,
		dataDir:     *dataDir,
		jsonOut:     *jsonOut,
	}, nil
}

// runSearch opens the store, executes a hybrid search, and writes results to out.
// errOut receives error messages; when nil, os.Stderr is used.
func runSearch(args []string, out io.Writer, errOut io.Writer) error {
	cfg, err := parseSearchFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()
	searcher := hybrid.NewSearcherFromSettings(ctx, st)

	params := store.SearchParams{
		Q:     cfg.query,
		Limit: cfg.limit,
	}
	if !cfg.allProjects && cfg.project != "" {
		params.Project = cfg.project
	}
	if cfg.obsType != "" {
		params.Type = cfg.obsType
	}

	results, meta, err := searcher.Search(ctx, params)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		fmt.Fprintln(out, "no results")
		return nil
	}

	if cfg.jsonOut {
		return writeSearchJSON(out, results)
	}
	writeSearchTable(out, results, meta)
	return nil
}

// writeSearchTable writes the aligned table output for search results.
func writeSearchTable(out io.Writer, results []store.SearchResult, meta hybrid.Meta) {
	// Column widths.
	const (
		colID    = 6
		colType  = 14
		colTitle = 50
		colScore = 8
	)

	header := fmt.Sprintf("%-*s  %-*s  %-*s  %*s",
		colID, "ID",
		colType, "TYPE",
		colTitle, "TITLE",
		colScore, "SCORE",
	)
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, strings.Repeat("─", colID+2+colType+2+colTitle+2+colScore))

	for _, r := range results {
		obs := r.Observation
		title := obs.Title
		if len(title) > colTitle {
			title = title[:colTitle-1] + "…"
		}
		obsType := obs.Type
		if len(obsType) > colType {
			obsType = obsType[:colType-1] + "…"
		}
		fmt.Fprintf(out, "%-*d  %-*s  %-*s  %*.4f\n",
			colID, obs.ID,
			colType, obsType,
			colTitle, title,
			colScore, -r.Score, // Score is negative (lower = better); flip for display
		)
	}

	// Footer.
	footer := fmt.Sprintf("\n%d result(s)", len(results))
	chips := ""
	if meta.Fuzzy {
		chips += "  ~FUZZY"
	}
	if meta.Hybrid {
		chips += "  ~HYBRID"
	}
	fmt.Fprintln(out, footer+chips)
}

// searchJSONRow is the JSON representation of a single search result.
type searchJSONRow struct {
	ID      int64   `json:"id"`
	SyncID  string  `json:"sync_id"`
	Type    string  `json:"type"`
	Title   string  `json:"title"`
	Project string  `json:"project"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// writeSearchJSON writes results as a JSON array to out.
func writeSearchJSON(out io.Writer, results []store.SearchResult) error {
	rows := make([]searchJSONRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, searchJSONRow{
			ID:      r.Observation.ID,
			SyncID:  r.Observation.SyncID,
			Type:    r.Observation.Type,
			Title:   r.Observation.Title,
			Project: r.Observation.Project,
			Score:   -r.Score, // flip for user-facing display (higher = better)
			Snippet: r.Snippet,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}
