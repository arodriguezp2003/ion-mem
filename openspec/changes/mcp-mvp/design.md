# Design: MCP Server MVP for ion-mem

## 1. Overview

ion-mem ships its first user-facing surface as an **MCP stdio server in Go** that wraps the
shipped `internal/store` and `internal/project` packages. AI coding agents (Claude Code,
OpenCode, etc.) speak MCP and gain persistent memory across sessions/compactions with
zero shell context-switching. This is the change that turns ion-mem from "a Go library
with tests" into "a tool a developer can plug into their editor today".

We adopt `github.com/mark3labs/mcp-go v0.44.0` — the same library and version engram pins.
Re-using the upstream framework avoids hand-rolling JSON-RPC framing/transport/initialize
negotiation and keeps our wire behavior bug-compatible with the tool people already trust.
The `internal/mcp` package owns server lifecycle, tool registration, project resolution
per call, implicit session bootstrap, prompt-buffer plumbing, and the response envelope.

Per the locked decisions, **ion-mem chooses the Ionix identity over engram drop-in
compatibility at the wire level**: every tool name is prefixed `ion_*` (not `mem_*`), and
the project-override env var is `ION_MEM_PROJECT` (not `ENGRAM_PROJECT`). Behavioral shape
(envelope keys, store delegation, session affordances) still mirrors engram so a future
`claude-code-plugin` change can ship skill text that just substitutes the tool names.

## 2. Resolved Decisions

| # | Decision | One-line justification | Upstream ref |
|---|----------|------------------------|--------------|
| 1 | `github.com/mark3labs/mcp-go v0.44.0` (pin exact) | Engram parity, mature framework, no roll-your-own protocol | `engram-source/go.mod:11` |
| 2 | `*Server` struct carries store + resolver + session/prompt state; passed via closures | Testable, no package-level singletons, fresh state per `New` | `engram-source/internal/mcp/mcp.go:233` |
| 3 | `ion_session_summary` as dedicated tool (not alias for `ion_save`) | Explicit closure ritual; agents have distinct semantic | `engram-source/internal/mcp/mcp.go:570` |
| 4 | `ion_context` returns markdown string | What trained agents already consume; engram parity | `engram-source/internal/mcp/mcp.go:482` |
| 5 | Skip `write_queue.go` for v1 — `SetMaxOpenConns(1)` already serializes | Adding queue without profiling is premature; revisit only on contention | `internal/store/store.go:45` |
| 6 | Single-slot prompt buffer per session (latest prompt only) | Simpler than ring buffer; covers `capture_prompt:true` UX in one read | engram uses richer activity tracking — deferred |
| 7 | Env var override name is `ION_MEM_PROJECT` | Ionix identity wins; users who set `ENGRAM_PROJECT` migrate manually | n/a |
| 8 | Three slices: server-scaffold-and-first-tools / daily-driver-tools / utility-tools-and-polish (~600/800/700 LOC) | Each slice is independently mergeable and demoable; respects 400-line review budget per chained PR | n/a |
| 9 | Tool naming prefix is `ion_*` (NOT `mem_*`) | Ionix identity; future plugin maps skill text; non-negotiable | n/a |

These nine are **CLOSED**. Do NOT re-open in apply.

## 3. Architecture

### 3.1 Package layout

```
internal/mcp/
├── doc.go              Package comment (existing — to be replaced)
├── server.go           NewServer + tool registration framework + profile resolver
├── context.go          *Server struct + state (store, resolver, sessions, prompt buffer)
├── envelope.go         Envelope helpers (build, marshal, attach extensions)
├── project.go          Server.resolveProject (env > flag > cwd) + ambiguity handling
├── session.go          Server.ensureSession + Server.{record,lastPromptFor}Session
├── handlers/
│   ├── doc.go          Package comment
│   ├── project.go      ion_current_project
│   ├── save.go         ion_save (+ ion_save_prompt lives here too)
│   ├── search.go       ion_search + ion_get_observation
│   ├── context.go      ion_context
│   ├── session.go      ion_session_start, ion_session_end, ion_session_summary
│   ├── suggest.go      ion_suggest_topic_key
│   ├── update.go       ion_update, ion_delete
│   └── stats.go        ion_timeline, ion_stats
├── server_test.go      Integration tests (in-process MCP client)
└── handlers/*_test.go  Per-group black-box handler tests
```

The `handlers/` subdirectory is **kept separate** (engram inlines everything into one
2741-LOC `mcp.go`; we split for clarity — 14 tools across 8 files keeps each file under
~250 LOC and matches the rest of ion-mem's small-file convention).

### 3.2 `*Server` struct

```go
type Server struct {
    store        *store.Store
    detect       func(cwd string) (project.DetectionResult, error) // injected for tests; default = project.DetectFull
    defaultProj  string  // from ION_MEM_PROJECT or --project flag; empty = auto-detect each call
    profile      string  // "agent" | "all"; default "all"
    sessionMu    sync.Mutex
    sessionsByProj map[string]string  // project -> active session ID
    promptsBySession map[string]string // sessionID -> latest prompt (single slot)
}
```

- `New(s *store.Store, opts ...Option) *Server` — functional options for `WithDefaultProject`, `WithProfile`, `WithDetectFunc` (test seam).
- `(s *Server) Register(srv *mcpserver.MCPServer)` — registers every tool whose name passes `s.allowsTool(name)`; handler closures capture `s`.
- `(s *Server) Serve(ctx context.Context) error` — wraps `mcpserver.ServeStdio(srv)`.

Handlers receive `*Server` by closure during registration — never via package globals.

### 3.3 Project resolution (per tool call)

```go
func (s *Server) resolveProject(ctx context.Context, projectArg, cwdOverride string) (project.DetectionResult, error)
```

Precedence (first non-empty wins):
1. **`projectArg`** — explicit `project` argument on the tool call (per-call override).
2. **`s.defaultProj`** — process-level override (`ION_MEM_PROJECT` env or `--project` flag).
3. **`cwdOverride`** — `cwd` argument on the tool call, if present.
4. **`project.DetectFull(os.Getwd())`** — auto-detect.

When 1 or 2 fires, `Source` is set to `"env_override"` (resolver synthesizes a
`DetectionResult` with `Project`, `Source="env_override"`, `Path=cwd`). When 4 returns
`ErrAmbiguousProject`, `ion_current_project` returns the structured ambiguity inline; all
other tools wrap-and-return so the caller can recover.

### 3.4 Session auto-create

```go
func (s *Server) ensureSession(ctx context.Context, project, sessionIDArg string) (string, error)
```

- If `sessionIDArg` is non-empty, return it (caller-managed session).
- Else, with `sessionMu` held: look up `sessionsByProj[project]`; if present return it; else
  generate `mcp-<project>-<unixnano>`, call `store.CreateSession`, cache, return.
- Idempotent on duplicate ID: `ion_session_start` calls `Server.ensureSession` with an
  explicit ID; if `CreateSession` returns a primary-key conflict, treat as success and
  return the existing session via `store.GetSession`.

### 3.5 Prompt buffer

```go
func (s *Server) recordPrompt(sessionID, content string)            // overwrites slot
func (s *Server) lastPromptForSession(sessionID string) string      // "" if empty
```

Single-slot per session — `ion_save_prompt` writes to both `store.AddPromptIfMissing`
AND the slot. `ion_save` with `capture_prompt:true` (default) pulls from the slot if
non-empty and attaches the prompt to the observation via `store.AddPromptIfMissing` on
the same session (no observation-prompt FK; correlation is by session+timestamp).

### 3.6 Profile filtering

`Server.profile` is "agent" or "all". `Server.allowsTool(name)` checks a hardcoded
`agentTools` set in `server.go`:

```go
var agentTools = map[string]bool{
    "ion_save": true, "ion_search": true, "ion_context": true,
    "ion_session_summary": true, "ion_session_start": true, "ion_session_end": true,
    "ion_get_observation": true, "ion_suggest_topic_key": true,
    "ion_save_prompt": true, "ion_current_project": true, "ion_update": true,
}
```

`--tools=agent` registers the 11 above; `--tools=all` registers all 14.

## 4. Tool Surface (formal contract — 14 tools)

Standard envelope (see §6) is attached to every response except `ion_current_project`,
which returns the `DetectionResult` shape directly.

| Tool | Slice | Input | Output extras | Side effects |
|------|-------|-------|---------------|--------------|
| `ion_current_project` | 1 | `{cwd?: string}` | (returns `DetectionResult` directly, NOT wrapped: `{project, project_source, project_path, available_projects?, warning?}`) | `project.DetectFull(cwd \|\| os.Getwd())` |
| `ion_save` | 1 | `{title: string (req), content: string, type?: string="manual", project?: string, scope?: string="project", topic_key?: string, session_id?: string, capture_prompt?: bool=true, cwd?: string}` | `{id: int64, sync_id: string, revision_count: int, duplicate_count: int, prompt_attached: bool}` | `resolveProject`; `ensureSession`; if `capture_prompt` and slot non-empty: `store.AddPromptIfMissing`; `store.AddObservation` |
| `ion_search` | 1 | `{query: string (req), type?: string, project?: string, scope?: string, limit?: int=10, all_projects?: bool=false, cwd?: string}` | `{results: [{id, sync_id, title, type, project, scope, topic_key?, content_preview, score, created_at}], count: int}` | `resolveProject` (unless `all_projects`); `store.Search` (`content_preview` = first 300 chars) |
| `ion_context` | 2 | `{project?: string, limit?: int=10, cwd?: string}` | (envelope wraps `result: <markdown>`) | `resolveProject`; `store.RecentSessions` + `store.RecentObservations` formatted as markdown |
| `ion_get_observation` | 2 | `{id: int64 (req)}` | `{observation: {id, sync_id, session_id, type, title, content, tool_name?, project, scope, topic_key?, revision_count, duplicate_count, last_seen_at, created_at, updated_at}}` | `store.GetObservation` |
| `ion_session_start` | 2 | `{session_id: string (req), project?: string, directory?: string, cwd?: string}` | `{session_id: string, created: bool}` | `resolveProject`; `store.CreateSession` (idempotent — PK conflict → `created:false` + existing row) |
| `ion_session_end` | 2 | `{session_id: string (req), summary?: string=""}` | `{session_id: string, ended_at: string}` | `store.EndSession` (last-write-wins) |
| `ion_session_summary` | 2 | `{summary: string (req), session_id?: string, project?: string, topic_key?: string, cwd?: string}` | `{session_id, observation_id, sync_id}` | `resolveProject`; `ensureSession`; `store.AddObservation{type:"session_summary", title:"Session summary: <project>", content:summary, topic_key:topic_key \|\| "session-summary"}` then optional `store.EndSession` if `session_id` argument was supplied |
| `ion_save_prompt` | 2 | `{content: string (req), session_id?: string, project?: string, cwd?: string}` | `{id: int64, sync_id: string, session_id: string}` | `resolveProject`; `ensureSession`; `store.AddPromptIfMissing`; `Server.recordPrompt(sessionID, content)` |
| `ion_suggest_topic_key` | 2 | `{title: string (req), type?: string}` | `{topic_key: string}` | Pure helper (lowercase, replace non-`[a-z0-9]` with `-`, prefix with `type` if provided); no store call |
| `ion_update` | 3 | `{id: int64 (req), title?: string, content?: string, type?: string, topic_key?: string, tool_name?: string}` | `{id, sync_id, revision_count, updated_at}` | `store.UpdateObservation` |
| `ion_delete` | 3 | `{id: int64 (req), hard?: bool=false}` | `{id: int64, hard: bool}` | `store.DeleteObservation` |
| `ion_timeline` | 3 | `{observation_id: int64 (req), before?: int=5, after?: int=5}` | `{anchor_id: int64, entries: [{kind: "observation"\|"prompt", id, content_preview, created_at, ...}]}` | `store.Timeline` |
| `ion_stats` | 3 | `{cwd?: string}` | `{total_sessions, total_observations, total_prompts, by_project: [{project, observation_count, prompt_count}]}` | `store.Stats` |

Input/output shapes are declared in `handlers/<group>.go` and exercised by per-group
black-box tests (see §7).

## 5. Slice Boundaries

### Slice 1 — server-scaffold-and-first-tools (~600 LOC)
- **Tools**: `ion_current_project`, `ion_save`, `ion_search`
- **New files**: `server.go`, `context.go`, `envelope.go`, `project.go`, `session.go`,
  `handlers/doc.go`, `handlers/project.go`, `handlers/save.go`, `handlers/search.go`,
  `handlers/project_test.go`, `handlers/save_test.go`, `handlers/search_test.go`,
  `server_test.go` (smoke only — full integration in Slice 3)
- **Shared infra introduced here (covers all slices)**: envelope helpers, `*Server`,
  project resolver, session ensure, prompt buffer, profile gate, registration loop
- **Acceptance gate**: `go build ./...` green; `go test ./internal/mcp/...` green;
  manual MCP smoke test (`mcp.json` pointed at built binary or `go run` harness)
  drives `ion_current_project` → `ion_save` → `ion_search` round-trip

### Slice 2 — daily-driver-tools (~800 LOC)
- **Tools**: `ion_context`, `ion_get_observation`, `ion_session_start`,
  `ion_session_end`, `ion_session_summary`, `ion_save_prompt`, `ion_suggest_topic_key`
- **New files**: `handlers/context.go`, `handlers/session.go`, `handlers/suggest.go`,
  plus their `_test.go` siblings; appended cases in existing `handlers/save_test.go`
  for `capture_prompt` interaction
- **Acceptance gate**: `mem_save` + `mem_save_prompt` + `mem_save{capture_prompt:true}`
  round-trip attaches prompt; `ion_session_start` idempotent on duplicate ID;
  `ion_context` returns non-empty markdown after a few saves

### Slice 3 — utility-tools-and-polish (~700 LOC)
- **Tools**: `ion_update`, `ion_delete`, `ion_timeline`, `ion_stats`
- **New files**: `handlers/update.go`, `handlers/stats.go`, their `_test.go` siblings,
  plus `server_test.go` expanded to full 14-tool integration coverage
- **Acceptance gate**: `go test ./internal/mcp/... -cover` ≥ 75%;
  `gofmt -l . && go vet ./...` clean; full integration test asserts every tool returns
  its declared envelope shape

Total: ~2100 LOC across three chained PRs (`stacked-to-main`). No CLI wiring in any
slice — `cmd/ion-mem/main.go` stays unchanged, deferred to `cli-mvp`.

## 6. Envelope Contract (formal)

Every tool except `ion_current_project` returns a JSON object with **at minimum** these
four keys:

```json
{
  "project": "ion-memory",
  "project_source": "git_root",
  "project_path": "/Users/.../ion-memory",
  "result": "Saved observation #42 (sync_id obs-abc123)"
}
```

`project_source` is one of: `"config"`, `"git_remote"`, `"git_root"`, `"git_child"`,
`"dir_basename"`, `"env_override"`, `"ambiguous"`.

Tool-specific keys are **appended** to the same object (not nested):

```json
{
  "project": "ion-memory",
  "project_source": "git_root",
  "project_path": "/Users/.../ion-memory",
  "result": "Saved observation #42",
  "id": 42,
  "sync_id": "obs-abc123",
  "revision_count": 1,
  "duplicate_count": 0,
  "prompt_attached": true
}
```

**Exception**: `ion_current_project` returns the `DetectionResult` shape directly
(no `result` field, no wrapping) — engram convention, kept for parity:

```json
{
  "project": "ion-memory",
  "project_source": "git_root",
  "project_path": "/Users/.../ion-memory",
  "available_projects": null,
  "warning": ""
}
```

When project resolution returns `ErrAmbiguousProject`:
- `ion_current_project` returns the result inline (with `project: ""`, `available_projects: [...]`).
- All other tools return the envelope with `project_source: "ambiguous"`,
  `project: ""`, `result: "ambiguous project — call ion_current_project"`, and
  `available_projects: [...]` appended as an extension.

The envelope helper `envelope.Build(det project.DetectionResult, msg string, extras map[string]any)`
is the **single** entry point — handlers MUST go through it. Apply must NOT hand-roll
JSON marshaling per tool.

## 7. Test Strategy

| Layer | What to test | Approach |
|-------|--------------|----------|
| Unit | `envelope.Build` shape, `Server.resolveProject` precedence, `Server.ensureSession` idempotency, `Server.recordPrompt`/`lastPromptForSession` | Table-driven, `package mcp` white-box where helpers are unexported |
| Handler | Each tool's input parsing, store delegation, envelope output | Black-box `package handlers_test`, **real `*store.Store` with `t.TempDir()`** (not mocked), in-process `mcp.CallTool` driver |
| Integration | All 14 tools registered, profile filter works, error envelope shape | `server_test.go` — boot `*Server`, exercise via `mcpserver.NewInProcessTransport` (mcp-go test helper) |

**Real `*Store` over mocks** — decision made here. Justification: the store API surface
is small, tests run in ~ms with `t.TempDir()`, and end-to-end (handler → store → SQLite
→ back) catches the "test name lies about assertion" class of bug (discovery #57) that
mocks would hide.

Coverage target: **≥ 75%** package-wide. Lower than `internal/store` (79%) because
mcp-go protocol surface (initialize, capabilities negotiation) is exercised but not
asserted line-by-line.

Test helpers live in `mcp_helpers_test.go` (matching local-store convention):
`mustServer(t)`, `mustCall(t, srv, toolName, args)`, `mustEnvelope(t, raw []byte)`.

## 8. Strict TDD Operational Notes

Per `sdd-init` (strict_tdd=true), every handler ships via red → green → refactor:

1. **`[TDD-RED]`** Write `handlers/<group>_test.go` case that drives `ion_<tool>` via
   the in-process MCP transport with crafted args, asserts envelope keys and the
   expected store side-effects (e.g. row count after `ion_save`).
2. **`[TDD-GREEN]`** Implement `handle<Tool>` in `handlers/<group>.go` + register it
   in `server.go`. Run only the failing test, then the package.
3. **`[TDD-REFACTOR]`** Extract shared helpers as patterns emerge — typically
   `parseStringArg`, `parseInt64Arg`, envelope assertion helpers.

**Pre-commit pattern guard (per discoveries #46 and #57)**:

- Apply MUST run `rg "<tool_name>" internal/mcp` after each tool lands and confirm the
  test name describes what the assertion actually checks. E.g. a test named
  `TestSave_attaches_prompt_when_capture_prompt_true` MUST end with
  `if !env.PromptAttached { t.Fatal(...) }`, not the inverse.
- The orchestrator may sample-check by reading the artifact (spec / test / handler)
  directly rather than trusting the apply agent's status report.

## 9. Open Questions (post-design)

- **None blocking apply.** All nine proposal-level questions are locked. Subdir split,
  envelope helper signature, and real-Store-over-mocks are all decided above.

## 10. Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `mark3labs/mcp-go` API drift between versions | Low | Pin v0.44.0 in `go.mod`; bump as its own change with regression suite |
| Apply ships inconsistent envelope shapes across the 14 tools | Med | Single `envelope.Build` entry point; integration test asserts envelope keys per tool; reviewer spot-checks per discovery #57 |
| Session-ID ambiguity if multiple MCP clients connect to same project from separate processes | Low | In-process single-server state (one MCP binary per agent); cross-process sharing is a future `multi-client` change if it ever matters |
| `ion_*` identity creates engram-skill mismatch for trained agents | Med | Document explicitly; future `claude-code-plugin` change MUST ship skill files that say `ion_save` (not `mem_save`); add a regression note in archive report |
| Apply misreads "silent fall-through" requirements (per discovery #57) | Med | Restate in design (§3.3, §3.4); spec MUST require the test-name-matches-assertion guard; orchestrator audits handler diffs before merging each slice |
| `ION_MEM_PROJECT` rename costs muscle memory for engram users | Low | README mentions both env vars; if user pain materializes, accept `ENGRAM_PROJECT` as fallback in a follow-up — additive, non-breaking |
