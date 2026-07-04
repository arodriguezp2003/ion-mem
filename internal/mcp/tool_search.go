package mcp

import (
	"context"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSearchTool constructs the ion_search ServerTool.
func buildSearchTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_search",
		mcplib.WithDescription("Full-text search over saved observations using BM25 ranking. Returns envelope with results array and count. Zero results returns results:[] (never a Go error)."),
		mcplib.WithString("query", mcplib.Description("Search query (required)."), mcplib.Required()),
		mcplib.WithString("type", mcplib.Description("Filter by observation type.")),
		mcplib.WithString("project", mcplib.Description("Project override.")),
		mcplib.WithString("scope", mcplib.Description("Scope filter.")),
		mcplib.WithNumber("limit", mcplib.Description("Max results (default: 10).")),
		mcplib.WithBoolean("all_projects", mcplib.Description("Search across all projects (default: false).")),
		mcplib.WithString("cwd", mcplib.Description("Working directory for project detection override.")),
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
			Q:     query,
			Type:  obsType,
			Scope: scope,
			Limit: limit,
		}
		if !allProjects {
			params.Project = det.Project
		}
		// When projectArg is non-empty, use it directly (already in det.Project via resolveProject).

		results, err := s.store.Search(ctx, params)
		if err != nil {
			raw := BuildError(det, CodeDBError, "search error: "+err.Error())
			return textResult(raw), nil
		}

		// Build result rows with content_preview capped at 300 chars.
		rows := make([]map[string]any, 0, len(results))
		for _, r := range results {
			preview := r.Observation.Content
			if len(preview) > 300 {
				preview = preview[:300]
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
			}
			if r.Observation.TopicKey != nil {
				row["topic_key"] = *r.Observation.TopicKey
			}
			rows = append(rows, row)
		}

		// Ensure results is always an array (never null).
		var resultAny any = rows
		if rows == nil {
			resultAny = []any{}
		}

		extras := map[string]any{
			"results": resultAny,
			"count":   len(results),
		}
		raw := Build(det, "search complete", extras)
		return textResult(raw), nil
	}
}
