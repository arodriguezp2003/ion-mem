// `ion-mem status` subcommand — one-shot health snapshot of the local store.
//
// Prints: aggregate stats, recent observations, active sessions (with stale
// warnings for sessions active >24h), last captured prompt, and the DB file
// size. Read-only against the store; exits 0 even when alerts are present
// (caller can grep for "⚠" if they want a non-zero behavior).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// statusConfig collects the parsed flags for the `status` subcommand.
type statusConfig struct {
	dataDir string
	limit   int
}

// parseStatusFlags parses the `ion-mem status` flag set.
func parseStatusFlags(args []string, homeDir func() (string, error)) (statusConfig, error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	limit := fs.Int("limit", 5, "Number of recent items to display per section.")
	if err := fs.Parse(args); err != nil {
		return statusConfig{}, fmt.Errorf("ion-mem status: %w", err)
	}
	if *limit <= 0 {
		return statusConfig{}, fmt.Errorf("ion-mem status: --limit must be positive (got %d)", *limit)
	}
	return statusConfig{dataDir: *dataDir, limit: *limit}, nil
}

// runStatus opens the store and writes the health snapshot to out.
func runStatus(args []string, out io.Writer) error {
	cfg, err := parseStatusFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	stats, err := st.Stats(ctx)
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}

	recentObs, err := st.RecentObservations(ctx, store.RecentObservationsParams{Limit: cfg.limit})
	if err != nil {
		return fmt.Errorf("recent observations: %w", err)
	}

	allSessions, err := st.RecentSessions(ctx, "", 50)
	if err != nil {
		return fmt.Errorf("recent sessions: %w", err)
	}
	activeSessions := filterActive(allSessions)
	staleSessions := filterStale(activeSessions, now, 24*time.Hour)

	recentPrompts, err := st.RecentPrompts(ctx, "", 1)
	if err != nil {
		return fmt.Errorf("recent prompts: %w", err)
	}

	dbPath := filepath.Join(cfg.dataDir, "ion-mem.db")
	dbSize := fileSizeOrZero(dbPath)

	// Embedding coverage: fetch settings and count.
	embEnabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"
	embModel := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)
	var embHave, embTotal int
	if embEnabled {
		embHave, embTotal, _ = st.EmbeddingCoverage(ctx, "", embModel)
	}

	writeStatusReport(out, statusReport{
		dataDir:        cfg.dataDir,
		dbPath:         dbPath,
		dbSize:         dbSize,
		limit:          cfg.limit,
		now:            now,
		stats:          stats,
		recentObs:      recentObs,
		activeSessions: activeSessions,
		staleSessions:  staleSessions,
		recentPrompts:  recentPrompts,
		embeddingReport: embeddingReport{
			enabled:  embEnabled,
			model:    embModel,
			embedded: embHave,
			total:    embTotal,
		},
	})
	return nil
}

// embeddingReport carries embedding coverage data for the status report.
type embeddingReport struct {
	enabled  bool
	model    string
	embedded int
	total    int
}

// statusReport collects everything writeStatusReport needs. Keeping it as a
// struct (rather than passing a long argument list) makes the writer testable
// in isolation against a hand-built fixture.
type statusReport struct {
	dataDir         string
	dbPath          string
	dbSize          int64
	limit           int
	now             time.Time
	stats           store.Stats
	recentObs       []store.Observation
	activeSessions  []store.Session
	staleSessions   []store.Session
	recentPrompts   []store.Prompt
	embeddingReport embeddingReport
}

func writeStatusReport(out io.Writer, r statusReport) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "ion-mem status — health snapshot")
	fmt.Fprintf(out, "Data dir: %s  (%s)\n", r.dataDir, humanBytes(r.dbSize))
	fmt.Fprintln(out)

	// Stats
	fmt.Fprintln(out, "Stats")
	fmt.Fprintf(out, "  observations: %d (non-deleted)\n", r.stats.TotalObservations)
	fmt.Fprintf(out, "  prompts:      %d\n", r.stats.TotalPrompts)
	fmt.Fprintf(out, "  sessions:     %d total  (%d active)\n", r.stats.TotalSessions, len(r.activeSessions))
	// Embedding coverage line.
	if !r.embeddingReport.enabled {
		fmt.Fprintln(out, "  embeddings: disabled")
	} else {
		pct := 0
		if r.embeddingReport.total > 0 {
			pct = r.embeddingReport.embedded * 100 / r.embeddingReport.total
		}
		fmt.Fprintf(out, "  embeddings: %d/%d embedded (%d%%) — model %s\n",
			r.embeddingReport.embedded, r.embeddingReport.total, pct, r.embeddingReport.model)
	}
	if len(r.stats.ByProject) > 0 {
		fmt.Fprintln(out, "  by project:")
		sorted := append([]store.ProjectStats(nil), r.stats.ByProject...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ObservationCount > sorted[j].ObservationCount
		})
		for _, p := range sorted {
			fmt.Fprintf(out, "    %-20s  %d obs  %d prompts\n", p.Project, p.ObservationCount, p.PromptCount)
		}
	}
	fmt.Fprintln(out)

	// Recent observations
	fmt.Fprintf(out, "Recent observations (top %d)\n", r.limit)
	if len(r.recentObs) == 0 {
		fmt.Fprintln(out, "  (none yet — call ion_save from the agent)")
	}
	for _, obs := range r.recentObs {
		age := relativeTime(r.now, obs.CreatedAt)
		fmt.Fprintf(out, "  #%-4d %-14s %-15s %-9s %s\n",
			obs.ID, truncate(obs.Type, 14), truncate(obs.Project, 15), age, truncate(obs.Title, 60))
	}
	fmt.Fprintln(out)

	// Active sessions
	fmt.Fprintln(out, "Active sessions")
	if len(r.activeSessions) == 0 {
		fmt.Fprintln(out, "  (none active — sessions auto-create on first ion_save)")
	}
	for _, s := range r.activeSessions {
		age := relativeTimeFromTime(r.now, s.StartedAt)
		stale := ""
		for _, st := range r.staleSessions {
			if st.ID == s.ID {
				stale = "  ⚠ STALE (>24h)"
				break
			}
		}
		fmt.Fprintf(out, "  %-44s %-15s started %s%s\n", truncate(s.ID, 44), truncate(s.Project, 15), age, stale)
	}
	fmt.Fprintln(out)

	// Last prompt
	fmt.Fprintln(out, "Last captured prompt")
	if len(r.recentPrompts) == 0 {
		fmt.Fprintln(out, "  (no prompts captured — check user-prompt-submit.sh hook)")
	} else {
		p := r.recentPrompts[0]
		age := relativeTime(r.now, p.CreatedAt)
		fmt.Fprintf(out, "  #%d  %s  %s\n", p.ID, age, truncate(p.Content, 80))
	}
	fmt.Fprintln(out)

	// Alerts
	fmt.Fprintln(out, "Alerts")
	alertCount := 0
	if len(r.staleSessions) > 0 {
		fmt.Fprintf(out, "  ⚠ %d session(s) active >24h without close — call `ion-mem session-end --id=<id>` or ion_session_summary.\n", len(r.staleSessions))
		alertCount++
	}
	if r.stats.TotalObservations == 0 {
		fmt.Fprintln(out, "  ⚠ no observations saved yet — Memory Protocol may not be firing. Check SKILL.md is loaded.")
		alertCount++
	}
	if r.stats.TotalPrompts == 0 && r.stats.TotalSessions > 0 {
		fmt.Fprintln(out, "  ⚠ no prompts captured but sessions exist — user-prompt-submit.sh hook may not be writing.")
		alertCount++
	}
	if alertCount == 0 {
		fmt.Fprintln(out, "  ✓ no issues detected")
	}
	fmt.Fprintln(out)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func filterActive(sessions []store.Session) []store.Session {
	out := make([]store.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Status == "active" {
			out = append(out, s)
		}
	}
	return out
}

func filterStale(sessions []store.Session, now time.Time, threshold time.Duration) []store.Session {
	out := make([]store.Session, 0)
	for _, s := range sessions {
		if now.Sub(s.StartedAt) > threshold {
			out = append(out, s)
		}
	}
	return out
}

// relativeTime takes a stored ISO-8601 timestamp string and returns "Xh ago" /
// "Xm ago" / "just now". Falls back to the SQLite default `2006-01-02 15:04:05`
// shape so legacy rows or hand-crafted seeds still render relatively.
func relativeTime(now time.Time, iso string) string {
	if iso == "" {
		return "?"
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05", // SQLite datetime('now') default
	}
	for _, layout := range formats {
		if t, err := time.Parse(layout, iso); err == nil {
			return relativeTimeFromTime(now, t)
		}
	}
	return iso
}

func relativeTimeFromTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}

func fileSizeOrZero(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}
