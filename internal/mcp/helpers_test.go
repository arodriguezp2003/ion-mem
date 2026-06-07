package mcp_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcptest "github.com/mark3labs/mcp-go/mcptest"
)

// mustStore returns a fresh *store.Store backed by a temp directory.
// It registers t.Cleanup to close the store when the test finishes.
func mustStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("mustStore: store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// mustFakeProject returns a DetectionResult and an Option that injects a
// fake project resolver into a Server.
func mustFakeProject(name string) (project.DetectionResult, mcp.Option) {
	det := project.DetectionResult{
		Project: name,
		Source:  "git_root",
		Path:    "/fake/" + name,
	}
	opt := mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, nil
	})
	return det, opt
}

// mustTestServer creates an mcptest.Server with ion-mem tools registered.
// It returns both the underlying *mcp.Server and the test server.
// t.Cleanup is registered to close the test server.
func mustTestServer(t *testing.T, st *store.Store, opts ...mcp.Option) (*mcp.Server, *mcptest.Server) {
	t.Helper()

	ionSrv := mcp.New(st, opts...)
	tools := ionSrv.ServerTools()

	ts, err := mcptest.NewServer(t, tools...)
	if err != nil {
		t.Fatalf("mustTestServer: %v", err)
	}
	t.Cleanup(ts.Close)
	return ionSrv, ts
}

// mustCall invokes the named tool via the in-process test client.
// Fatals on transport/protocol errors; tool-level errors appear in the result body.
func mustCall(t *testing.T, ts *mcptest.Server, name string, args map[string]any) *mcplib.CallToolResult {
	t.Helper()
	req := mcplib.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := ts.Client().CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("mustCall(%q): %v", name, err)
	}
	return res
}

// mustEnvelope decodes the first text content from a CallToolResult as a
// map[string]any representing the envelope JSON.
func mustEnvelope(t *testing.T, res *mcplib.CallToolResult) map[string]any {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("mustEnvelope: result has no content")
	}
	tc, ok := res.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("mustEnvelope: content[0] is %T, want TextContent", res.Content[0])
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("mustEnvelope: JSON decode: %v\nraw: %s", err, tc.Text)
	}
	return m
}
