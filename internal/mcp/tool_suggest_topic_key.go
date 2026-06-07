package mcp

import (
	"context"
	"regexp"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// buildSuggestTopicKeyTool constructs the ion_suggest_topic_key ServerTool.
func buildSuggestTopicKeyTool(s *Server) mcpserver.ServerTool {
	tool := mcplib.NewTool("ion_suggest_topic_key",
		mcplib.WithDescription("Generate a suggested topic_key in family/specific-description format. Pure function — no store calls. Returns envelope + topic_key."),
		mcplib.WithString("title", mcplib.Description("Observation title to slugify (required)."), mcplib.Required()),
		mcplib.WithString("type", mcplib.Description("Observation type used to derive the key family prefix.")),
	)
	return mcpserver.ServerTool{Tool: tool, Handler: handleSuggestTopicKey(s)}
}

// handleSuggestTopicKey is the ToolHandlerFunc for ion_suggest_topic_key.
// Pure function: MUST NOT call any store method.
func handleSuggestTopicKey(s *Server) toolHandler {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title, _ := req.RequireString("title")
		obsType := req.GetString("type", "")

		slug := slugifyTitle(title)
		var key string
		if obsType != "" {
			family := typeToFamily(obsType)
			key = family + "/" + slug
		} else {
			key = slug
		}

		// Use a zero-value DetectionResult — this tool is project-independent.
		det, _ := s.resolveProject("", "")

		raw := Build(det, "topic key suggested", map[string]any{
			"topic_key": key,
		})
		return textResult(raw), nil
	}
}

// typeToFamily maps an observation type to its topic-key family prefix.
// Unknown types default to "learning".
func typeToFamily(obsType string) string {
	switch strings.ToLower(obsType) {
	case "architecture":
		return "architecture"
	case "bugfix", "bug":
		return "bug"
	case "decision":
		return "decision"
	case "pattern":
		return "pattern"
	case "config":
		return "config"
	case "discovery":
		return "discovery"
	case "learning":
		return "learning"
	default:
		return "learning"
	}
}

var (
	// nonAlphanumRe matches any character that is not a lowercase letter, digit, or hyphen.
	nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)
	// multiHyphenRe collapses consecutive hyphens into one.
	multiHyphenRe = regexp.MustCompile(`-{2,}`)
)

// slugifyTitle converts a title to lowercase kebab-case, keeping only [a-z0-9-].
func slugifyTitle(title string) string {
	s := strings.ToLower(title)
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = multiHyphenRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
