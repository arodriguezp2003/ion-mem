// Package handlers provides MCP tool handler functions for ion-mem.
//
// Each handler file implements one group of ion_* tools. Handlers receive
// a *mcp.Server via closure during tool registration and delegate to
// internal/store for persistence and internal/project for project detection.
//
// All tool responses (except ion_current_project) are built via
// envelope.Build — handlers MUST NOT hand-roll JSON marshaling.
package handlers
