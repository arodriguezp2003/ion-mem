package handlers_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/store"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcptest "github.com/mark3labs/mcp-go/mcptest"
)

// contextBG returns context.Background() — tiny helper to keep test lines short.
func contextBG(_ *testing.T) context.Context {
	return context.Background()
}

// mustStore returns a fresh *store.Store backed by a temp directory.
func mustStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("mustStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// mustTestServer creates an in-process mcptest.Server with ion-mem tools registered.
func mustTestServer(t *testing.T, st *store.Store, opts ...mcp.Option) (*mcp.Server, *mcptest.Server) {
	t.Helper()
	ionSrv := mcp.New(st, opts...)
	ts, err := mcptest.NewServer(t, ionSrv.ServerTools()...)
	if err != nil {
		t.Fatalf("mustTestServer: %v", err)
	}
	t.Cleanup(ts.Close)
	return ionSrv, ts
}

// callTool invokes a tool on the test server. Fatals on protocol errors.
func callTool(t *testing.T, ts *mcptest.Server, name string, args map[string]any) *mcplib.CallToolResult {
	t.Helper()
	req := mcplib.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := ts.Client().CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("callTool(%q): %v", name, err)
	}
	return res
}

// decodeText decodes the first TextContent from a CallToolResult as JSON.
func decodeText(t *testing.T, res *mcplib.CallToolResult) map[string]any {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("decodeText: no content")
	}
	tc, ok := res.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("decodeText: content[0] is %T, want TextContent", res.Content[0])
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("decodeText: %v\nraw: %s", err, tc.Text)
	}
	return m
}
