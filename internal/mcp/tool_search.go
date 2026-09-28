package mcp

import (
	"context"

	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
	"github.com/arodriguezp2003/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSearchTool constructs the ion_search ServerTool.
func buildSearchTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_search",
		mcplib.WithDescription("Search over saved observations. Search mode is configured via the search.mode setting: \"vector\" (default — semantic similarity, no BM25), \"lexical\" (weighted BM25 with recency decay; retries with an OR fallback and sets fuzzy:true when the all-terms query matches nothing), or \"hybrid\" (AND-only BM25 fused with vector search via RRF; never runs the noisy OR fallback). Returns envelope with results array and count; content_preview is a contextual snippet around the match term. mode/effective_mode report the requested vs. actually-used mode; degraded:true (with degraded_reason) means it fell back to lexical because embeddings are disabled or unreachable. Superseded and obsolete observations stay searchable by default — ranked lower, never deleted — each result carries status/superseded_by/status_reason and a superseded row gets a note pointing at its replacement; use include_superseded=false or status to narrow. Zero results returns results:[] (never a Go error)."),
		mcplib.WithString("query", mcplib.Description("Search query (required)."), mcplib.Required()),
		mcplib.WithString("type", mcplib.Description("Filter by observation type.")),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithString("scope", mcplib.Description("Scope filter.")),
		mcplib.WithNumber("limit", mcplib.Description("Max results (default: 10).")),
		mcplib.WithBoolean("all_projects", mcplib.Description("Search across all projects (default: false).")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
		mcplib.WithString("status", mcplib.Description("Filter to an exact status: active, superseded, or obsolete (default: no filter — all statuses).")),
		mcplib.WithBoolean("include_superseded", mcplib.Description("Include superseded and obsolete observations in results, ranked lower (default: true). false returns only active rows.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSearch(s)}
}

// handleSearch is the ToolHandlerFunc for ion_search.
func handleSearch(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		query := req.GetString("query", "")
		obsType := req.GetString("type", "")
		projectArg := req.GetString("project", "")
		scope := req.GetString("scope", "")
		limit := req.GetInt("limit", 10)
		allProjects := req.GetBool("all_projects", false)
		cwdArg := req.GetString("cwd", "")
		statusArg := req.GetString("status", "")
		includeSuperseded := req.GetBool("include_superseded", true)

		// Resolve project (ignored when all_projects=true or project param supplied).
		det, err := s.resolveProject(projectArg, cwdArg)
		if err != nil {
			code := CodeProjectAmbiguous
			if !isAmbiguousProjectError(err) {
				code = CodeInternal
			}
			raw := BuildError(det, code, "error resolving project: "+err.Error())
			return textResult(raw), nil
		}

		params := store.SearchParams{
			Q:                 query,
			Type:              obsType,
			Scope:             scope,
			Limit:             limit,
			Status:            statusArg,
			IncludeSuperseded: &includeSuperseded,
		}
		if !allProjects {
			params.Project = det.Project
		}
		// When projectArg is non-empty, use it directly (already in det.Project via resolveProject).

		// Use hybrid searcher (reads embeddings.enabled from settings).
		// When embeddings are disabled or Ollama is unreachable, falls back to
		// BM25 silently — identical behaviour to the previous implementation.
		searcher := hybrid.NewSearcherFromSettings(ctx, s.store)
		results, meta, err := searcher.Search(ctx, params)
		if err != nil {
			raw := BuildError(det, CodeDBError, "search error: "+err.Error())
			return textResult(raw), nil
		}

		// Build result rows. content_preview uses Snippet when non-empty (contextual
		// excerpt around the match term), falling back to the first 300 bytes of
		// content so that short docs always produce a usable preview.
		rows := make([]map[string]any, 0, len(results))
		for _, r := range results {
			preview := r.Snippet
			if preview == "" {
				preview = r.Observation.Content
				if len(preview) > 300 {
					preview = preview[:300]
				}
			}
			row := map[string]any{
				"id":              r.Observation.ID,
				"sync_id":         r.Observation.SyncID,
				"title":           r.Observation.Title,
				"type":            r.Observation.Type,
				"project":         r.Observation.Project,
				"scope":           r.Observation.Scope,
				"content_preview": preview,
				"score":           r.Score,
				"created_at":      r.Observation.CreatedAt,
				"status":          r.Status,
			}
			if r.Observation.TopicKey != nil {
				row["topic_key"] = *r.Observation.TopicKey
			}
			if r.SupersededBy != nil {
				row["superseded_by"] = *r.SupersededBy
				row["note"] = supersededNote(ctx, s.store, *r.SupersededBy)
			}
			if r.Observation.StatusReason != nil {
				row["status_reason"] = *r.Observation.StatusReason
			}
			rows = append(rows, row)
		}

		// Ensure results is always an array (never null).
		var resultAny any = rows
		if rows == nil {
			resultAny = []any{}
		}

		extras := map[string]any{
			"results":         resultAny,
			"count":           len(results),
			"fuzzy":           meta.Fuzzy,
			"hybrid":          meta.Hybrid,
			"mode":            string(meta.Mode),
			"effective_mode":  string(meta.Effective),
			"degraded":        meta.Degraded,
			"degraded_reason": meta.Reason,
		}
		raw := Build(det, "search complete", extras)
		return textResult(raw), nil
	}
}
