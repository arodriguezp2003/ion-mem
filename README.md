# ion-mem

Persistent memory for AI coding agents — team-grade, agent-agnostic, local-first.

## Status

Local layer shipped (store + MCP + project detection + Claude Code plugin).
Cloud layer (multi-user, projects, RBAC, invites, audit) is the next big slice.

## What is ion-mem?

`ion-mem` provides a structured, queryable memory layer for AI coding agents.
It stores observations, sessions, prompts, and relations in a local SQLite
database with FTS5 full-text indexing, and exposes them through an MCP
(Model Context Protocol) server so agents can persist and recall context
across sessions and post-compaction.

Module path: `github.com/ionix/ion-mem`.

## Build & Test

Requirements: Go 1.25+

```bash
# Build all packages
go build ./...

# Run all tests
go test ./...

# Format check
make fmt

# Vet
make lint
```

## Makefile targets

| Target | Description |
|--------|-------------|
| `build` | Compile all packages |
| `test`  | Run all tests |
| `lint`  | Run `go vet ./...` |
| `fmt`   | Check gofmt formatting (exits non-zero on drift) |
| `help`  | List available targets |
