# Proposal: MCP Server MVP for ion-mem

## Intent

Expose ion-mem's local memory layer (`internal/store` + `internal/project`) to AI coding agents over the Model Context Protocol so a Claude Code session can drive `mem_save`/`mem_search`/`mem_context`/session lifecycle without ever leaving the editor. This is the first "user-facing" surface of ion-mem: until MCP lands, the store and project packages are reachable only by Go tests. After MCP, an agent gets persistent memory across sessions and compactions, with the same wire shape (`mem_*` tools, project envelope) as upstream engram — which keeps the future Claude Code plugin a drop-in swap.

## Scope

### In Scope

- MCP stdio server (`internal/mcp/server.go`) on `github.com/mark3labs/mcp-go v0.44.0`
- 14 core tools: `mem_save`, `mem_update`, `mem_delete`, `mem_search`, `mem_context`, `mem_get_observation`, `mem_timeline`, `mem_session_start`, `mem_session_end`, `mem_session_summary`, `mem_save_prompt`, `mem_current_project`, `mem_suggest_topic_key`, `mem_stats`
- Standard response envelope: `{project, project_source, project_path, result, ...}` on every tool except `mem_current_project`
- Per-call project resolution via `project.DetectFull(cwd)`, with `ION_MEM_PROJECT` env override and `--project` flag override
- Implicit session bootstrap: if a tool lacks `session_id`, MCP creates and reuses a process-lifetime session per project
- Prompt context capture: `mem_save_prompt` populates `user_prompts` table AND a single-slot per-session in-memory latest-prompt cache; `mem_save` opts into attaching that latest prompt via `capture_prompt: true` (default)
- Tool profiles: `--tools=agent` (10–12 daily-driver tools) and `--tools=all` (everything including `mem_stats`)
- Handler unit tests + `server_test.go` integration tests using in-process MCP client
- Strict TDD throughout (red → green → refactor per handler)

### Out of Scope

- `mem_judge` / `mem_compare` (conflict surfacing) — needs `memory_relations` table; future `mcp-conflict-surfacing`
- `mem_doctor`, `mem_merge_projects` — admin tools; future `mcp-admin`
- `mem_capture_passive` — `## Key Learnings` extractor; future change
- `ion-mem mcp` CLI subcommand wiring in `cmd/ion-mem/main.go` — separate `cli-mvp` change
- HTTP REST API (`engram serve` equivalent) — separate `local-api-mvp` change
- Cloud sync, setup installer, TUI — separate changes
- In-process write queue (rely on `SetMaxOpenConns(1)` for MVP serialization)

## Capabilities

### New Capabilities

- `mcp-server`: stdio MCP server lifecycle, tool registration, profile resolution, project resolution per call, implicit session and prompt-context plumbing, standard response envelope
- `mcp-tools`: behavioral spec for each of the 14 tool handlers — input schema, store delegation, envelope shape, error semantics, idempotency rules

### Modified Capabilities

- None. `local-store` and `project-detection` are consumed unchanged; no spec deltas.

## Approach

Wrap each `Store` method in a thin MCP handler that (1) resolves project via `project.DetectFull(cwd)`, (2) acquires/creates the session for that project, (3) calls the store, (4) wraps the result in the standard envelope. Keep the server itself stateful via a `*Server` struct that holds the `*store.Store`, the project resolver, and a map of `project → current session ID + latest prompt`. Pass the server into tool registration closures so handlers stay testable without package-level singletons.

Mirror engram tool names and envelope shape verbatim so any agent trained on engram's surface drives ion-mem with zero relearning — and so a future ion-mem Claude Code plugin can replace engram in user `mcp.json` without changing prompts. `mem_session_summary` ships as a dedicated tool (not an alias for `mem_save`) because agents have a distinct closure ritual and the explicit affordance matters more than DRY. `mem_context` returns markdown (engram parity) for v1; programmatic consumers can fall back to `mem_search` + `mem_timeline`.

Spot-check spec MUSTs against actual tests during apply (per discovery #57): apply must verify that test names match assertions for every silent-fall-through, idempotency, and envelope-shape requirement.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/mcp/server.go` | New | Stdio bootstrap, profile resolution, tool registration |
| `internal/mcp/context.go` | New | Per-server state: project → session ID + latest prompt |
| `internal/mcp/envelope.go` | New | Standard response envelope helpers |
| `internal/mcp/handlers/*.go` | New | One file per tool group (save, search, context, session, prompt, project, suggest, stats) |
| `internal/mcp/*_test.go` | New | Handler unit tests + integration tests |
| `internal/mcp/doc.go` | Modified | Replace placeholder package comment |
| `go.mod` | Modified | Add `github.com/mark3labs/mcp-go v0.44.0` |
| `cmd/ion-mem/main.go` | Unchanged | CLI wiring deferred to `cli-mvp` |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `mark3labs/mcp-go` API drift during MVP | Low | Pin v0.44.0 (matches engram); upgrade as a separate change |
| Apply agent ships wrong envelope shape silently (per discovery #57) | Med | Spec MUSTs explicit; apply spot-checks envelope-shape tests against spec; integration test covers every tool's envelope |
| `mem_context` markdown shape locks us out of programmatic consumers | Low | Documented v1 limitation; structured-JSON variant is additive in future |
| FTS5 WAL contention under concurrent `mem_save` | Low | `SetMaxOpenConns(1)` already serializes; write queue deferred unless profiling shows blocking |
| Project ambiguity at MCP boundary surfaces as tool errors | Med | `mem_current_project` returns ambiguity as structured response, never errors; other tools return envelope with `project_source: "ambiguous"` and instruct caller |
| `ION_MEM_PROJECT` rename breaks users who set `ENGRAM_PROJECT` in muscle memory | Low | Document in README; if user feedback shows pain, accept both env vars in a follow-up |

## Rollback Plan

`internal/mcp/` is a leaf package consumed by no other code in ion-mem (CLI wiring is deferred). Rollback = `git revert` the merge commits; `internal/store/` and `internal/project/` remain untouched and continue to serve tests. No data migration, no schema change, no breaking change to existing capabilities.

## Dependencies

- `local-store` capability (shipped — provides `Store` methods)
- `project-detection` capability (shipped — provides `DetectFull` + `ErrAmbiguousProject`)
- `project-scaffold` capability (shipped — Go module, CI, build wiring)
- New external: `github.com/mark3labs/mcp-go v0.44.0`

## Success Criteria

- [ ] All 14 tools registered and callable via in-process MCP client integration test
- [ ] Every tool except `mem_current_project` returns the standard envelope `{project, project_source, project_path, result, ...}`
- [ ] `mem_save` round-trips: write → `mem_search` finds it → `mem_get_observation` returns full content
- [ ] `mem_save_prompt` followed by `mem_save` with `capture_prompt: true` attaches the prompt to the observation
- [ ] `mem_session_start` is idempotent on duplicate ID (returns existing session, does not error)
- [ ] `mem_current_project` returns structured ambiguity response when `project.ErrAmbiguousProject` fires (never errors at MCP layer)
- [ ] `ION_MEM_PROJECT` env var and `--project` flag override auto-detection in that precedence
- [ ] `--tools=agent` profile exposes the 10–12 daily-driver subset; `--tools=all` exposes everything
- [ ] `go test ./internal/mcp/...` passes with ≥75% coverage
- [ ] `gofmt -l . && go vet ./...` clean

## Open Questions (must be resolved in design)

1. **MCP Go library**: `mark3labs/mcp-go v0.44.0` (engram parity, mature) vs alternative SDK. Recommendation: `mark3labs/mcp-go`.
2. **Process-local context shape**: package-level singleton vs `*Server` struct vs `context.Context`. Recommendation: `*Server` struct.
3. **`mem_session_summary` as dedicated tool vs `mem_save` alias**. Recommendation: dedicated tool (engram parity, clearer agent affordance).
4. **`mem_context` output**: structured JSON vs markdown. Recommendation: markdown for v1 (engram parity).
5. **Write queue**: explicit `write_queue.go` vs rely on `SetMaxOpenConns(1)`. Recommendation: skip for MVP.
6. **Prompt buffer**: ring (N=10) vs single-slot per session. Recommendation: single-slot.
7. **Project override env name**: `ION_MEM_PROJECT` (identity) vs keep `ENGRAM_PROJECT` (engram parity). Recommendation: `ION_MEM_PROJECT`; revisit if users complain.
8. **Slice cut**: 3 slices (scaffold + 3 tools / 7 tools / 4 tools + integration). Recommendation: as listed.
9. **Tool naming prefix**: keep `mem_*` (engram parity, plugin drop-in) vs rename to `ion_*` (identity). Recommendation: keep `mem_*`.
