// cli_lifecycle.go — backup, export, prune, config, project subcommands.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── backup ───────────────────────────────────────────────────────────────────

type backupConfig struct {
	out     string
	dataDir string
}

func parseBackupFlags(args []string, homeDir func() (string, error)) (backupConfig, error) {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "Destination file path (default: <data-dir>/backups/ion-mem-<timestamp>.db).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	if err := fs.Parse(args); err != nil {
		return backupConfig{}, fmt.Errorf("ion-mem backup: %w", err)
	}
	return backupConfig{out: *out, dataDir: *dataDir}, nil
}

func runBackup(args []string, out io.Writer) error {
	cfg, err := parseBackupFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	dest := cfg.out
	if dest == "" {
		backupsDir := filepath.Join(cfg.dataDir, "backups")
		if err := os.MkdirAll(backupsDir, 0o700); err != nil {
			return fmt.Errorf("backup: mkdir backups: %w", err)
		}
		ts := time.Now().UTC().Format("20060102-150405")
		dest = filepath.Join(backupsDir, "ion-mem-"+ts+".db")
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if err := st.Backup(context.Background(), dest); err != nil {
		return err
	}

	info, _ := os.Stat(dest)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	fmt.Fprintf(out, "Backup written to: %s (%d bytes)\n", dest, size)
	return nil
}

// ─── export ───────────────────────────────────────────────────────────────────

type exportConfig struct {
	out     string
	dataDir string
}

func parseExportFlags(args []string, homeDir func() (string, error)) (exportConfig, error) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "Output directory (default: <data-dir>/export-<timestamp>/).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	if err := fs.Parse(args); err != nil {
		return exportConfig{}, fmt.Errorf("ion-mem export: %w", err)
	}
	return exportConfig{out: *out, dataDir: *dataDir}, nil
}

func runExport(args []string, out io.Writer) error {
	cfg, err := parseExportFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	outDir := cfg.out
	if outDir == "" {
		ts := time.Now().UTC().Format("20060102-150405")
		outDir = filepath.Join(cfg.dataDir, "export-"+ts)
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	manifest, err := st.Export(context.Background(), outDir)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Exported to: %s\n", outDir)
	fmt.Fprintf(out, "  observations: %d\n", manifest.Counts["observations"])
	fmt.Fprintf(out, "  prompts:      %d\n", manifest.Counts["prompts"])
	fmt.Fprintf(out, "  sessions:     %d\n", manifest.Counts["sessions"])
	fmt.Fprintf(out, "  revisions:    %d\n", manifest.Counts["revisions"])
	fmt.Fprintf(out, "  settings:     %d\n", manifest.Counts["settings"])
	fmt.Fprintf(out, "  embeddings:   not exported (regenerable)\n")
	return nil
}

// ─── prune ────────────────────────────────────────────────────────────────────

type pruneConfig struct {
	apply       bool
	promptDays  int
	deletedDays int
	dataDir     string
}

func parsePruneFlags(args []string, homeDir func() (string, error)) (pruneConfig, error) {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "Execute deletions (default: dry-run only).")
	promptDays := fs.Int("prompt-days", 90, "Delete user prompts older than N days.")
	deletedDays := fs.Int("deleted-days", 30, "Hard-delete soft-deleted observations older than N days.")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	if err := fs.Parse(args); err != nil {
		return pruneConfig{}, fmt.Errorf("ion-mem prune: %w", err)
	}
	return pruneConfig{
		apply:       *apply,
		promptDays:  *promptDays,
		deletedDays: *deletedDays,
		dataDir:     *dataDir,
	}, nil
}

func runPrune(args []string, out io.Writer) error {
	cfg, err := parsePruneFlags(args, os.UserHomeDir)
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

	promptCutoff := now.AddDate(0, 0, -cfg.promptDays).UTC().Format(time.RFC3339)
	deletedCutoff := now.AddDate(0, 0, -cfg.deletedDays).UTC().Format(time.RFC3339)

	// Read settings-overridable defaults.
	if v := st.SettingOrDefault(ctx, "retention.prompt_days", ""); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			promptCutoff = now.AddDate(0, 0, -n).UTC().Format(time.RFC3339)
		}
	}
	if v := st.SettingOrDefault(ctx, "retention.deleted_days", ""); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			deletedCutoff = now.AddDate(0, 0, -n).UTC().Format(time.RFC3339)
		}
	}

	promptCount, err := st.CountPrunablePrompts(ctx, promptCutoff)
	if err != nil {
		return fmt.Errorf("count prunable prompts: %w", err)
	}
	obsCount, err := st.CountPrunableDeletedObs(ctx, deletedCutoff)
	if err != nil {
		return fmt.Errorf("count prunable deleted obs: %w", err)
	}

	if !cfg.apply {
		fmt.Fprintf(out, "[DRY-RUN] would delete:\n")
		fmt.Fprintf(out, "  prompts older than %d days:          %d\n", cfg.promptDays, promptCount)
		fmt.Fprintf(out, "  soft-deleted obs older than %d days: %d\n", cfg.deletedDays, obsCount)
		fmt.Fprintf(out, "Run with --apply to execute.\n")
		return nil
	}

	// Safety check: require a fresh backup (within 24h) before destructive ops.
	lastAt := st.SettingOrDefault(ctx, "backup.last_at", "")
	if lastAt == "" {
		return fmt.Errorf("no fresh backup — run 'ion-mem backup' first")
	}
	t, parseErr := time.Parse(time.RFC3339, lastAt)
	if parseErr != nil || now.Sub(t) > 24*time.Hour {
		return fmt.Errorf("no fresh backup — run 'ion-mem backup' first (last backup: %s)", lastAt)
	}

	deletedPrompts, err := st.PrunePrompts(ctx, promptCutoff)
	if err != nil {
		return fmt.Errorf("prune prompts: %w", err)
	}
	deletedObs, err := st.PruneDeletedObs(ctx, deletedCutoff)
	if err != nil {
		return fmt.Errorf("prune deleted obs: %w", err)
	}

	fmt.Fprintf(out, "Pruned:\n")
	fmt.Fprintf(out, "  prompts deleted:              %d\n", deletedPrompts)
	fmt.Fprintf(out, "  soft-deleted obs hard-deleted: %d\n", deletedObs)
	return nil
}

// ─── config ───────────────────────────────────────────────────────────────────

// knownConfigKeys is the closed set of valid config keys.
var knownConfigKeys = []string{
	"embeddings.enabled",
	"embeddings.ollama_url",
	"embeddings.model",
	"retention.prompt_days",
	"retention.deleted_days",
}

// knownConfigDefaults documents defaults shown by config list.
var knownConfigDefaults = map[string]string{
	"embeddings.enabled":     "false",
	"embeddings.ollama_url":  "http://localhost:11434",
	"embeddings.model":       store.DefaultEmbeddingsModel,
	"retention.prompt_days":  "90",
	"retention.deleted_days": "30",
}

type configCmd struct {
	subcmd  string
	key     string
	value   string
	dataDir string
}

func parseConfigFlags(args []string, homeDir func() (string, error)) (configCmd, error) {
	// Drain positional arguments then any --data-dir flag.
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	// Separate positional args from flags.
	var positional []string
	var flagArgs []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
		} else {
			positional = append(positional, a)
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return configCmd{}, fmt.Errorf("ion-mem config: %w", err)
	}

	if len(positional) == 0 {
		return configCmd{}, fmt.Errorf("ion-mem config: subcommand required (get <key>, set <key> <value>, list)")
	}

	cmd := configCmd{subcmd: positional[0], dataDir: *dataDir}
	switch cmd.subcmd {
	case "get":
		if len(positional) < 2 {
			return configCmd{}, fmt.Errorf("ion-mem config get: <key> required")
		}
		cmd.key = positional[1]
	case "set":
		if len(positional) < 3 {
			return configCmd{}, fmt.Errorf("ion-mem config set: <key> <value> required")
		}
		cmd.key = positional[1]
		cmd.value = positional[2]
	case "list":
		// no extra args
	default:
		return configCmd{}, fmt.Errorf("ion-mem config: unknown subcommand %q", cmd.subcmd)
	}
	return cmd, nil
}

func isKnownConfigKey(key string) bool {
	for _, k := range knownConfigKeys {
		if k == key {
			return true
		}
	}
	return false
}

func runConfig(args []string, out io.Writer) error {
	cmd, err := parseConfigFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	st, err := store.Open(cmd.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	switch cmd.subcmd {
	case "get":
		val, ok, err := st.GetSetting(ctx, cmd.key)
		if err != nil {
			return fmt.Errorf("config get: %w", err)
		}
		if !ok {
			def := knownConfigDefaults[cmd.key]
			fmt.Fprintf(out, "%s = %s (default)\n", cmd.key, def)
		} else {
			fmt.Fprintf(out, "%s = %s\n", cmd.key, val)
		}

	case "set":
		if !isKnownConfigKey(cmd.key) {
			valid := strings.Join(knownConfigKeys, ", ")
			return fmt.Errorf("ion-mem config set: unknown key %q — valid keys: %s", cmd.key, valid)
		}
		if err := st.SetSetting(ctx, cmd.key, cmd.value); err != nil {
			return fmt.Errorf("config set: %w", err)
		}
		fmt.Fprintf(out, "Set %s = %s\n", cmd.key, cmd.value)

	case "list":
		for _, key := range knownConfigKeys {
			val, ok, err := st.GetSetting(ctx, key)
			if err != nil {
				return fmt.Errorf("config list: %w", err)
			}
			if ok {
				fmt.Fprintf(out, "%s = %s\n", key, val)
			} else {
				fmt.Fprintf(out, "%s = %s (default)\n", key, knownConfigDefaults[key])
			}
		}
	}
	return nil
}

// ─── project ──────────────────────────────────────────────────────────────────

type projectRenameConfig struct {
	oldName string
	newName string
	apply   bool
	dataDir string
}

func parseProjectRenameFlags(args []string, homeDir func() (string, error)) (projectRenameConfig, error) {
	fs := flag.NewFlagSet("project rename", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "Execute rename (default: dry-run).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")

	var positional []string
	var flagArgs []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
		} else {
			positional = append(positional, a)
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return projectRenameConfig{}, fmt.Errorf("ion-mem project rename: %w", err)
	}
	if len(positional) < 2 {
		return projectRenameConfig{}, fmt.Errorf("ion-mem project rename: <old> <new> required")
	}
	return projectRenameConfig{
		oldName: positional[0],
		newName: positional[1],
		apply:   *apply,
		dataDir: *dataDir,
	}, nil
}

func runProject(args []string, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	if len(args) == 0 {
		return fmt.Errorf("ion-mem project: subcommand required (rename <old> <new>)")
	}
	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "rename":
		return runProjectRename(rest, out)
	default:
		return fmt.Errorf("ion-mem project: unknown subcommand %q", subcmd)
	}
}

func runProjectRename(args []string, out io.Writer) error {
	cfg, err := parseProjectRenameFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	if !cfg.apply {
		// Dry-run: count rows per table without modifying anything.
		tables := []struct {
			name  string
			table string
		}{
			{"observations", "observations"},
			{"sessions", "sessions"},
			{"prompts", "user_prompts"},
		}
		total := 0
		fmt.Fprintf(out, "[DRY-RUN] would rename project %q to %q:\n", cfg.oldName, cfg.newName)
		for _, tbl := range tables {
			var count int
			st.DB().QueryRowContext(ctx,
				fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE project=?", tbl.table), //nolint:gosec
				cfg.oldName,
			).Scan(&count)
			fmt.Fprintf(out, "  %s: %d rows\n", tbl.name, count)
			total += count
		}
		if total == 0 {
			return fmt.Errorf("project %q not found", cfg.oldName)
		}
		fmt.Fprintf(out, "Run with --apply to execute.\n")
		return nil
	}

	rows, err := st.RenameProject(ctx, cfg.oldName, cfg.newName)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Renamed project %q to %q (%d rows updated)\n", cfg.oldName, cfg.newName, rows)
	return nil
}
