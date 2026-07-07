package mcp

import (
	"context"

	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildStatsTool constructs the ion_stats ServerTool.
func buildStatsTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_stats",
		mcplib.WithDescription("Return aggregate counts for the entire store: total sessions, observations, prompts, and per-project breakdowns."),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleStats(s)}
}

// handleStats is the ToolHandlerFunc for ion_stats.
func handleStats(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		det, _ := s.resolveProject("", "")

		stats, err := s.store.Stats(ctx)
		if err != nil {
			raw := BuildError(det, CodeDBError, "error fetching stats: "+err.Error())
			return textResult(raw), nil
		}

		// Serialize ByProject slice; ensure non-null JSON array.
		byProject := make([]map[string]any, 0, len(stats.ByProject))
		for _, ps := range stats.ByProject {
			byProject = append(byProject, map[string]any{
				"project":           ps.Project,
				"observation_count": ps.ObservationCount,
				"prompt_count":      ps.PromptCount,
			})
		}

		// Embedding coverage extras.
		embEnabled := s.store.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"
		embModel := s.store.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)
		var embHave, embTotal int
		if embEnabled {
			embHave, embTotal, _ = s.store.EmbeddingCoverage(ctx, "", embModel)
		}

		raw := Build(det, "stats fetched", map[string]any{
			"stats": map[string]any{
				"total_sessions":     stats.TotalSessions,
				"total_observations": stats.TotalObservations,
				"total_prompts":      stats.TotalPrompts,
				"by_project":         byProject,
				"embeddings": map[string]any{
					"enabled":  embEnabled,
					"model":    embModel,
					"embedded": embHave,
					"total":    embTotal,
				},
			},
		})
		return textResult(raw), nil
	}
}
