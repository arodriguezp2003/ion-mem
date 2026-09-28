// Package handlers is intentionally kept thin in slice 1.
// The handler implementations live in the parent mcp package as build* functions
// to avoid circular imports (handlers would need to import mcp.Server,
// but mcp imports handlers — a cycle).
//
// This file exists to satisfy the package declaration requirement.
package handlers
