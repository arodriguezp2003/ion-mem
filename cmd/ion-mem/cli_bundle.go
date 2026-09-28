// cli_bundle.go — export-project / import-project subcommands: share one
// project's memory as a portable .ionmem.zip file between teammates on the
// same repo. See internal/bundle and store.ExportProject/ImportProject.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/project"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// resolveDefaultProject detects the project for the current working
// directory the same way the MCP server does (internal/project.Detect).
// Returns ("", nil) when detection is ambiguous (multiple git children) —
// callers must treat that as "could not detect, ask the user for --project".
func resolveDefaultProject() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve default project: getwd: %w", err)
	}
	name, err := project.Detect(cwd)
	if err != nil {
		if errors.Is(err, project.ErrAmbiguousProject) {
			return "", nil
		}
		return "", fmt.Errorf("resolve default project: %w", err)
	}
	return name, nil
}

// ─── export-project ──────────────────────────────────────────────────────────

type exportProjectConfig struct {
	project        string
	out            string
	dataDir        string
	withPrompts    bool
	includeDeleted bool
	allowSecrets   bool
}

func parseExportProjectFlags(args []string, homeDir func() (string, error)) (exportProjectConfig, error) {
	fs := flag.NewFlagSet("export-project", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	projectFlag := fs.String("project", "", "Project to export (default: detected from cwd).")
	out := fs.String("out", "", "Output file path (default: <cwd>/<project>-<YYYYMMDD>.ionmem.zip).")
	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	withPrompts := fs.Bool("with-prompts", false, "Include user prompts in the bundle (excluded by default).")
	includeDeleted := fs.Bool("include-deleted", false, "Include soft-deleted observations.")
	allowSecrets := fs.Bool("allow-secrets", false, "Export even if the secret scanner finds matches.")

	if err := parseFlagsWithHelp(fs, args, os.Stdout); err != nil {
		return exportProjectConfig{}, fmt.Errorf("ion-mem export-project: %w", err)
	}

	return exportProjectConfig{
		project:        *projectFlag,
		out:            *out,
		dataDir:        *dataDir,
		withPrompts:    *withPrompts,
		includeDeleted: *includeDeleted,
		allowSecrets:   *allowSecrets,
	}, nil
}

// runExportProject opens the store, builds a bundle for one project, scans
// it for secrets (refusing to write unless --allow-secrets), and writes the
// zip to disk. Prints a manifest summary to out.
func runExportProject(args []string, out io.Writer) error {
	cfg, err := parseExportProjectFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	projectName := cfg.project
	if projectName == "" {
		projectName, err = resolveDefaultProject()
		if err != nil {
			return fmt.Errorf("export-project: %w", err)
		}
		if projectName == "" {
			return fmt.Errorf("export-project: could not detect a project from the current directory (ambiguous) — pass --project")
		}
	}

	outPath := cfg.out
	if outPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("export-project: getwd: %w", err)
		}
		ts := time.Now().UTC().Format("20060102")
		outPath = filepath.Join(cwd, projectName+"-"+ts+".ionmem.zip")
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()
	b, err := st.ExportProject(ctx, projectName, store.ExportProjectOptions{
		IncludeDeleted: cfg.includeDeleted,
		IncludePrompts: cfg.withPrompts,
	})
	if err != nil {
		return fmt.Errorf("export-project: %w", err)
	}

	findings := bundle.ScanSecrets(b)
	if len(findings) > 0 && !cfg.allowSecrets {
		fmt.Fprintf(out, "Refusing to export: %d potential secret(s) found:\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(out, "  [%s] %s (%s)\n", f.SyncID, f.Title, f.Pattern)
		}
		fmt.Fprintln(out, "Re-run with --allow-secrets to export anyway.")
		return fmt.Errorf("export-project: refusing to export: %d potential secret(s) found", len(findings))
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("export-project: create %s: %w", outPath, err)
	}
	defer f.Close()

	if err := bundle.Write(f, b); err != nil {
		return fmt.Errorf("export-project: %w", err)
	}

	fmt.Fprintf(out, "Exported to: %s\n", outPath)
	fmt.Fprintf(out, "  project:      %s\n", b.Manifest.Project)
	fmt.Fprintf(out, "  observations: %d\n", b.Manifest.Counts.Observations)
	fmt.Fprintf(out, "  revisions:    %d\n", b.Manifest.Counts.Revisions)
	if b.Manifest.IncludesPrompts {
		fmt.Fprintf(out, "  prompts:      %d\n", b.Manifest.Counts.Prompts)
	} else {
		fmt.Fprintln(out, "  prompts:      not included (use --with-prompts)")
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "  WARNING: %d potential secret(s) included (--allow-secrets was set)\n", len(findings))
	}
	return nil
}

// ─── import-project ──────────────────────────────────────────────────────────

type importProjectConfig struct {
	file         string
	project      string
	apply        bool
	preferBundle bool
	withPrompts  bool
	dataDir      string
}

func parseImportProjectFlags(args []string, homeDir func() (string, error)) (importProjectConfig, error) {
	fs := flag.NewFlagSet("import-project", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	projectFlag := fs.String("project", "", "Target project (default: detected from cwd).")
	apply := fs.Bool("apply", false, "Execute the import (default: dry-run only).")
	preferBundle := fs.Bool("prefer-bundle", false, "On conflict, prefer the bundle's version over the local row.")
	withPrompts := fs.Bool("with-prompts", false, "Also import prompts (only if the bundle includes them).")
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
	if err := parseFlagsWithHelp(fs, flagArgs, os.Stdout); err != nil {
		return importProjectConfig{}, fmt.Errorf("ion-mem import-project: %w", err)
	}
	if len(positional) < 1 {
		return importProjectConfig{}, fmt.Errorf("ion-mem import-project: <file> is required")
	}

	return importProjectConfig{
		file:         positional[0],
		project:      *projectFlag,
		apply:        *apply,
		preferBundle: *preferBundle,
		withPrompts:  *withPrompts,
		dataDir:      *dataDir,
	}, nil
}

// runImportProject reads a bundle file, resolves the target project, and
// either prints a dry-run report (default) or backs up the store and applies
// the import (--apply). Never overwrites or deletes a teammate's existing
// rows unless --prefer-bundle is set.
func runImportProject(args []string, out io.Writer) error {
	cfg, err := parseImportProjectFlags(args, os.UserHomeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	f, err := os.Open(cfg.file)
	if err != nil {
		return fmt.Errorf("import-project: open %s: %w", cfg.file, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("import-project: stat %s: %w", cfg.file, err)
	}

	b, err := bundle.Read(f, info.Size())
	if err != nil {
		return fmt.Errorf("import-project: %w", err)
	}

	targetProject := cfg.project
	if targetProject == "" {
		targetProject, err = resolveDefaultProject()
		if err != nil {
			return fmt.Errorf("import-project: %w", err)
		}
		if targetProject == "" {
			return fmt.Errorf("import-project: could not detect a project from the current directory (ambiguous) — pass --project")
		}
	}

	if targetProject != b.Manifest.Project {
		fmt.Fprintf(out, "WARNING: bundle project %q differs from target project %q — importing into %q anyway.\n",
			b.Manifest.Project, targetProject, targetProject)
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	if !cfg.apply {
		report, err := st.ImportProject(ctx, b, store.ImportOptions{
			TargetProject:  targetProject,
			DryRun:         true,
			PreferBundle:   cfg.preferBundle,
			IncludePrompts: cfg.withPrompts,
		})
		if err != nil {
			return fmt.Errorf("import-project: %w", err)
		}
		fmt.Fprintln(out, "[DRY-RUN] no changes were written.")
		writeImportReport(out, report)
		fmt.Fprintln(out, "Re-run with --apply to execute.")
		return nil
	}

	backupsDir := filepath.Join(cfg.dataDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return fmt.Errorf("import-project: mkdir backups: %w", err)
	}
	ts := time.Now().UTC().Format("20060102-150405")
	backupDest := filepath.Join(backupsDir, "ion-mem-pre-import-"+ts+".db")
	if err := st.Backup(ctx, backupDest); err != nil {
		return fmt.Errorf("import-project: backup: %w", err)
	}
	fmt.Fprintf(out, "Backup written to: %s\n", backupDest)

	report, err := st.ImportProject(ctx, b, store.ImportOptions{
		TargetProject:  targetProject,
		PreferBundle:   cfg.preferBundle,
		IncludePrompts: cfg.withPrompts,
	})
	if err != nil {
		return fmt.Errorf("import-project: %w", err)
	}
	writeImportReport(out, report)
	fmt.Fprintf(out, "Hint: run `ion-mem backfill-embeddings --project %s` to embed the imported observations.\n", targetProject)
	return nil
}

// writeImportReport prints the inserted/skipped/updated/conflict/unresolved
// summary shared by both the dry-run and --apply paths.
func writeImportReport(out io.Writer, r store.ImportReport) {
	fmt.Fprintf(out, "Inserted: %d  Skipped: %d  Updated: %d\n", r.Inserted, r.Skipped, r.Updated)
	if len(r.Conflicts) > 0 {
		fmt.Fprintf(out, "Conflicts (%d):\n", len(r.Conflicts))
		for _, c := range r.Conflicts {
			fmt.Fprintf(out, "  [%s] %s — %s\n", c.SyncID, c.Title, c.Reason)
		}
	}
	if len(r.UnresolvedSupersedes) > 0 {
		fmt.Fprintf(out, "Unresolved supersedes (%d): %s\n", len(r.UnresolvedSupersedes), strings.Join(r.UnresolvedSupersedes, ", "))
	}
	// A downgraded row landed in the store with its content but WITHOUT the
	// status the bundle asked for, so the local copy now differs from the
	// sender's. Printing it is the only way the operator learns that.
	if len(r.StatusDowngrades) > 0 {
		fmt.Fprintf(out, "Status downgrades (%d) — imported as active instead:\n", len(r.StatusDowngrades))
		for _, d := range r.StatusDowngrades {
			fmt.Fprintf(out, "  [%s] wanted %s — %s\n", d.SyncID, d.Wanted, d.Reason)
		}
	}
	if r.Prompts > 0 {
		fmt.Fprintf(out, "Prompts imported: %d\n", r.Prompts)
	}
	fmt.Fprintf(out, "Session: %s\n", r.SessionID)
}
