# ion-mem

Persistent memory for AI coding agents, internal Ionix fork of engram.

## Status

scaffold / work in progress

## What is ion-mem?

`ion-mem` provides a structured, queryable memory layer for AI coding agents.
It stores observations, sessions, prompts, and relations in a local SQLite
database with FTS5 full-text indexing, and exposes them through an MCP
(Model Context Protocol) server so agents can persist and recall context
across sessions.

## Fork Relationship

`ion-mem` is an internal Ionix fork of
[engram](https://github.com/Gentleman-Programming/engram).
It extends engram with Ionix-specific features while tracking upstream
improvements. Module path: `github.com/ionix/ion-mem`.

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
