// Package mcp implements the Model Context Protocol (MCP) stdio server for ion-mem.
//
// It exposes 14 ion_* tools to any MCP-compatible agent (Claude Code, OpenCode, etc.),
// wrapping internal/store and internal/project behind a stable envelope contract.
// All tool names are prefixed with ion_ (NOT mem_*). The project-override environment
// variable is ION_MEM_PROJECT.
//
// The Server struct is the central context carrier. Create one with New, then call
// Serve to start the stdio loop:
//
//	srv := mcp.New(store, mcp.WithProfile("agent"))
//	if err := srv.Serve(ctx); err != nil {
//	    log.Fatal(err)
//	}
package mcp
