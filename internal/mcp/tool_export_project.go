package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildExportProjectTool constructs the ion_export_project ServerTool.
func buildExportProjectTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_export_project",
		mcplib.WithDescription("Export one project's memory to a portable .ionmem.zip file, to hand to a teammate working on the same repo. Prompts are excluded by default — pass with_prompts=true to include them. Never modifies or deletes anything locally (read-only against the store). Scans the export for secret-shaped content (API keys, tokens, private keys, passwords) first and refuses to write the file — returning the findings instead — unless allow_secrets=true."),
		mcplib.WithString("project", mcplib.Description("Project to export (default: detected from cwd).")),
		mcplib.WithString("out", mcplib.Description("Output file path (default: <project-path>/<project>-<YYYYMMDD>.ionmem.zip).")),
		mcplib.WithBoolean("with_prompts", mcplib.Description("Include user prompts in the bundle. Default: false (excluded).")),
		mcplib.WithBoolean("include_deleted", mcplib.Description("Include soft-deleted observations. Default: false.")),
		mcplib.WithBoolean("allow_secrets", mcplib.Description("Write the bundle even if the secret scanner finds matches. Default: false (refuse).")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleExportProject(s)}
}

// resolveBundlePath resolves a possibly-relative bundle file path (an
// ion_export_project "out" or ion_import_project "file" argument) against
// cwd. An MCP agent's own process cwd is not a safe implicit base for a
// relative path someone else typed — it may not match the directory the
// caller actually means — so a relative path with no cwd to resolve it
// against is refused with a clear message rather than silently resolved
// against whatever directory the server happens to be running in.
func resolveBundlePath(path, cwd string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	if cwd == "" {
		return "", fmt.Errorf("path %q is relative and no cwd was provided to resolve it against — pass an absolute path or supply cwd", path)
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd %q must be absolute to resolve relative path %q", cwd, path)
	}
	return filepath.Join(cwd, path), nil
}

// handleExportProject is the ToolHandlerFunc for ion_export_project.
func handleExportProject(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		projectArg := req.GetString("project", "")
		cwdArg := req.GetString("cwd", "")
		outArg := req.GetString("out", "")
		withPrompts := req.GetBool("with_prompts", false)
		includeDeleted := req.GetBool("include_deleted", false)
		allowSecrets := req.GetBool("allow_secrets", false)

		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		// A caller-supplied "out" must be absolute or resolvable against
		// "cwd" — resolved up front so a bad path fails before any export
		// work happens. An empty "out" (auto-generate a default) is
		// unaffected and handled further down.
		if outArg != "" {
			resolved, pathErr := resolveBundlePath(outArg, cwdArg)
			if pathErr != nil {
				raw := BuildError(det, CodeInvalidArgument, pathErr.Error())
				return textResult(raw), nil
			}
			outArg = resolved
		}

		b, err := s.store.ExportProject(ctx, det.Project, store.ExportProjectOptions{
			IncludeDeleted: includeDeleted,
			IncludePrompts: withPrompts,
		})
		if err != nil {
			raw := BuildError(det, CodeDBError, "error exporting project: "+err.Error())
			return textResult(raw), nil
		}

		findings := bundle.ScanSecrets(b)

		if len(findings) > 0 && !allowSecrets {
			raw := BuildError(det, CodeInvalidArgument,
				fmt.Sprintf("refusing to export: %d potential secret(s) found (%s) — retry with allow_secrets=true to export anyway",
					len(findings), formatFindingsSummary(findings)))
			return textResult(raw), nil
		}

		outPath := outArg
		if outPath == "" {
			base := det.Path
			if base == "" {
				if wd, wdErr := os.Getwd(); wdErr == nil {
					base = wd
				}
			}
			ts := time.Now().UTC().Format("20060102")
			outPath = filepath.Join(base, det.Project+"-"+ts+".ionmem.zip")
		}

		f, err := os.Create(outPath)
		if err != nil {
			raw := BuildError(det, CodeInternal, "error creating output file: "+err.Error())
			return textResult(raw), nil
		}
		defer f.Close()

		if err := bundle.Write(f, b); err != nil {
			raw := BuildError(det, CodeInternal, "error writing bundle: "+err.Error())
			return textResult(raw), nil
		}

		findingsPayload := make([]map[string]any, 0, len(findings))
		for _, finding := range findings {
			findingsPayload = append(findingsPayload, map[string]any{
				"sync_id": finding.SyncID,
				"title":   finding.Title,
				"pattern": finding.Pattern,
			})
		}

		raw := Build(det, "project exported", map[string]any{
			"out": outPath,
			"manifest": map[string]any{
				"format_version":   b.Manifest.FormatVersion,
				"project":          b.Manifest.Project,
				"exported_at":      b.Manifest.ExportedAt,
				"includes_prompts": b.Manifest.IncludesPrompts,
				"counts": map[string]any{
					"observations": b.Manifest.Counts.Observations,
					"revisions":    b.Manifest.Counts.Revisions,
					"prompts":      b.Manifest.Counts.Prompts,
				},
			},
			"findings": findingsPayload,
		})
		return textResult(raw), nil
	}
}

// formatFindingsSummary renders a short "sync_id:pattern, ..." summary for
// the refusal message, capped so a huge bundle doesn't blow up the envelope.
func formatFindingsSummary(findings []bundle.Finding) string {
	const maxShown = 5
	parts := make([]string, 0, len(findings))
	for i, f := range findings {
		if i >= maxShown {
			parts = append(parts, fmt.Sprintf("and %d more", len(findings)-maxShown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s:%s", f.SyncID, f.Pattern))
	}
	return strings.Join(parts, ", ")
}
