package mcp

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildImportProjectTool constructs the ion_import_project ServerTool.
func buildImportProjectTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_import_project",
		mcplib.WithDescription("Import a portable .ionmem.zip bundle (see ion_export_project) produced by a teammate on the same repo. Dry-run by default — pass apply=true to actually write. Never deletes anything and never overwrites an existing local row unless prefer_bundle=true (default: local rows always win on conflict). Prompts are excluded by default — pass with_prompts=true to also import them (only takes effect if the bundle itself includes prompts). Before applying, creates a backup of the local store."),
		mcplib.WithString("file", mcplib.Description("Path to the .ionmem.zip bundle file (required)."), mcplib.Required()),
		mcplib.WithString("project", mcplib.Description("Target project (default: detected from cwd).")),
		mcplib.WithBoolean("apply", mcplib.Description("Execute the import. Default: false (dry-run only, reports what would happen).")),
		mcplib.WithBoolean("prefer_bundle", mcplib.Description("On conflict, prefer the bundle's version over the local row. Default: false (local wins).")),
		mcplib.WithBoolean("with_prompts", mcplib.Description("Also import prompts, if the bundle includes them. Default: false.")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleImportProject(s)}
}

// handleImportProject is the ToolHandlerFunc for ion_import_project.
func handleImportProject(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		file, err := req.RequireString("file")
		if err != nil {
			det, _ := s.resolveProject("", "")
			raw := BuildError(det, CodeInvalidArgument, "file is required")
			return textResult(raw), nil
		}
		projectArg := req.GetString("project", "")
		cwdArg := req.GetString("cwd", "")
		apply := req.GetBool("apply", false)
		preferBundle := req.GetBool("prefer_bundle", false)
		withPrompts := req.GetBool("with_prompts", false)

		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		file, pathErr := resolveBundlePath(file, cwdArg)
		if pathErr != nil {
			raw := BuildError(det, CodeInvalidArgument, pathErr.Error())
			return textResult(raw), nil
		}

		f, err := os.Open(file)
		if err != nil {
			raw := BuildError(det, CodeInvalidArgument, "error opening bundle file: "+err.Error())
			return textResult(raw), nil
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			raw := BuildError(det, CodeInternal, "error reading bundle file: "+err.Error())
			return textResult(raw), nil
		}

		b, err := bundle.Read(f, info.Size())
		if err != nil {
			raw := BuildError(det, CodeInvalidArgument, "error parsing bundle: "+err.Error())
			return textResult(raw), nil
		}

		projectMismatch := b.Manifest.Project != det.Project

		var backupPath string
		if apply {
			if s.dataDir == "" {
				raw := BuildError(det, CodeInternal, "cannot apply import: server has no data directory configured for a pre-apply backup")
				return textResult(raw), nil
			}
			backupsDir := filepath.Join(s.dataDir, "backups")
			if err := os.MkdirAll(backupsDir, 0o700); err != nil {
				raw := BuildError(det, CodeInternal, "error creating backups directory: "+err.Error())
				return textResult(raw), nil
			}
			ts := time.Now().UTC().Format("20060102-150405")
			backupPath = filepath.Join(backupsDir, "ion-mem-pre-import-"+ts+".db")
			if err := s.store.Backup(ctx, backupPath); err != nil {
				raw := BuildError(det, CodeInternal, "error creating pre-apply backup: "+err.Error())
				return textResult(raw), nil
			}
		}

		report, err := s.store.ImportProject(ctx, b, store.ImportOptions{
			TargetProject:  det.Project,
			DryRun:         !apply,
			PreferBundle:   preferBundle,
			IncludePrompts: withPrompts,
		})
		if err != nil {
			raw := BuildError(det, CodeDBError, "error importing bundle: "+err.Error())
			return textResult(raw), nil
		}

		conflicts := make([]map[string]any, 0, len(report.Conflicts))
		for _, c := range report.Conflicts {
			conflicts = append(conflicts, map[string]any{
				"sync_id": c.SyncID,
				"title":   c.Title,
				"reason":  c.Reason,
			})
		}

		// StatusDowngrades are rows that WERE imported but had their bundled
		// status (superseded/obsolete) rejected by the store's invariants and
		// were left active instead. Silently dropping them from the envelope
		// would hide a real difference between the bundle and what now sits in
		// the local store, so they travel with the rest of the report.
		downgrades := make([]map[string]any, 0, len(report.StatusDowngrades))
		for _, d := range report.StatusDowngrades {
			downgrades = append(downgrades, map[string]any{
				"sync_id": d.SyncID,
				"wanted":  d.Wanted,
				"reason":  d.Reason,
			})
		}

		extras := map[string]any{
			"applied":               apply,
			"bundle_project":        b.Manifest.Project,
			"project_mismatch":      projectMismatch,
			"inserted":              report.Inserted,
			"skipped":               report.Skipped,
			"updated":               report.Updated,
			"conflicts":             conflicts,
			"unresolved_supersedes": report.UnresolvedSupersedes,
			"status_downgrades":     downgrades,
			"prompts_imported":      report.Prompts,
			"session_id":            report.SessionID,
		}
		if backupPath != "" {
			extras["backup_path"] = backupPath
		}

		resultMsg := "dry-run: no changes written"
		if apply {
			resultMsg = "project imported"
		}
		raw := Build(det, resultMsg, extras)
		return textResult(raw), nil
	}
}
