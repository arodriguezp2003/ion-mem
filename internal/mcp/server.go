package mcp

import (
	"context"
	"sync"

	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// agentTools is the set of tool names available under the "agent" profile.
// After slice 1: 3 tools. After slice 2: 10 tools. After slice 3: 14 tools.
// The full set of 14 is listed here so profile filtering is stable across slices;
// unimplemented tools simply have no registered handler yet.
var agentTools = map[string]struct{}{
	"ion_current_project":   {},
	"ion_save":              {},
	"ion_search":            {},
	"ion_context":           {},
	"ion_get_observation":   {},
	"ion_session_start":     {},
	"ion_session_end":       {},
	"ion_session_summary":   {},
	"ion_save_prompt":       {},
	"ion_suggest_topic_key": {},
	"ion_update":            {},
	"ion_delete":            {},
	"ion_timeline":          {},
	"ion_stats":             {},
}

// Server is the central context carrier for the MCP stdio server.
// It owns the store reference, the project resolver, session tracking,
// and the single-slot prompt buffer.
type Server struct {
	store       *store.Store
	detect      func(cwd string) (project.DetectionResult, error)
	defaultProj string
	profile     string

	// Project cache: first resolve result for process lifetime.
	cacheMu       sync.Mutex
	cachedProject *project.DetectionResult

	// Session tracking: maps project → last used session ID.
	sessionMu      sync.Mutex
	sessionsByProj map[string]string

	// Single-slot prompt buffer: maps session ID → last prompt content.
	promptMu         sync.Mutex
	promptsBySession map[string]string
}

// Option configures a Server.
type Option func(*Server)

// WithProfile sets the tool profile. Valid values: "agent" (default), "all".
func WithProfile(profile string) Option {
	return func(s *Server) {
		s.profile = profile
	}
}

// WithDefaultProject sets a static project override (loaded from ION_MEM_PROJECT or --project flag).
// Callers MUST NOT call os.Getenv directly — use configuredDefaultProject() and pass the result here.
func WithDefaultProject(proj string) Option {
	return func(s *Server) {
		s.defaultProj = proj
	}
}

// WithDetectFunc replaces the default project.DetectFull detection function.
// Primarily used in tests to inject deterministic project detection.
func WithDetectFunc(fn func(cwd string) (project.DetectionResult, error)) Option {
	return func(s *Server) {
		s.detect = fn
	}
}

// New creates a new Server wrapping the given Store with the supplied options.
func New(st *store.Store, opts ...Option) *Server {
	s := &Server{
		store:            st,
		detect:           project.DetectFull,
		profile:          "agent",
		sessionsByProj:   make(map[string]string),
		promptsBySession: make(map[string]string),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// allowsTool returns true if the named tool is enabled for this server's profile.
// In MVP, "agent" and "all" both allow the full agentTools set.
func (s *Server) allowsTool(name string) bool {
	_, ok := agentTools[name]
	return ok
}

// toolHandler is the mcp-go tool handler signature.
type toolHandler = func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error)

// ServerTools returns the slice of mcpserver.ServerTool entries for all tools
// that pass the profile filter. Used by tests and by Serve to build the MCPServer.
func (s *Server) ServerTools() []mcpserver.ServerTool {
	var tools []mcpserver.ServerTool

	candidates := []mcpserver.ServerTool{
		buildCurrentProjectTool(s),
		buildSaveTool(s),
		buildSearchTool(s),
		buildContextTool(s),
		buildGetObservationTool(s),
		buildSessionStartTool(s),
		buildSessionEndTool(s),
		buildSessionSummaryTool(s),
		buildSavePromptTool(s),
		buildSuggestTopicKeyTool(s),
	}

	for _, st := range candidates {
		if s.allowsTool(st.Tool.GetName()) {
			tools = append(tools, st)
		}
	}
	return tools
}

// Serve starts the MCP stdio loop and blocks until ctx is cancelled or an error occurs.
func (s *Server) Serve(ctx context.Context) error {
	srv := mcpserver.NewMCPServer("ion-mem", "0.1.0")
	for _, t := range s.ServerTools() {
		srv.AddTool(t.Tool, t.Handler)
	}
	stdio := mcpserver.NewStdioServer(srv)
	return stdio.Listen(ctx, nil, nil)
}

// textResult wraps a JSON payload as a successful mcp CallToolResult with one TextContent entry.
func textResult(raw []byte) *mcplib.CallToolResult {
	return &mcplib.CallToolResult{
		Content: []mcplib.Content{
			mcplib.TextContent{Type: "text", Text: string(raw)},
		},
	}
}

// RecordPromptForTest exposes recordPrompt for external test packages.
// It allows handler tests (in package handlers_test) to pre-seed the prompt buffer.
func (s *Server) RecordPromptForTest(sessionID, content string) {
	s.recordPrompt(sessionID, content)
}
