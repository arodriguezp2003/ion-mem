# ion-mcp Specification

**Status**: Active
**Shipped**: 2026-06-07 via SDD change mcp-mvp (3 stacked PRs: 3cdea8d + 6f10d34 + 6b35314)
**Last Updated**: 2026-07-04 via SDD change envelope-status-durable-prompts (envelope + durable prompt capture)
**Tools**: 14 ion_* registered under "agent" profile
**Tests**: 69 functions passing at 78.6% cross-package coverage

## 1. Capability: ion-mcp

### 1.1 Summary

`ion-mcp` is the MCP stdio server in Go that exposes 14 `ion_*` tools to any MCP-compatible
agent (Claude Code, OpenCode, etc.), wrapping `internal/store` and `internal/project` behind
a stable envelope contract. It turns ion-mem from a tested Go library into a live tool
an agent can drive today: persistent memory across sessions and compactions, with
the same behavioral shape as upstream engram but under the Ionix `ion_*` identity.

---

## 2. Slice 1 — Scaffold + first 3 tools

### 2.1 Requirements

#### Dependency

| ID | Requirement |
|----|-------------|
| R-S1-DEP-01 | `go.mod` MUST declare `github.com/mark3labs/mcp-go v0.44.0` at the exact pinned version. |

#### Server scaffold

| ID | Requirement |
|----|-------------|
| R-S1-SVR-01 | `internal/mcp` MUST expose a `Server` struct with fields: `store *store.Store`, `detect` (project-detection func), `defaultProj string`, `profile string` ("agent" \| "all"), `sessionMu sync.Mutex`, `sessionsByProj map[string]string`, `promptsBySession map[string]string`. |
| R-S1-SVR-02 | MUST expose `New(s *store.Store, opts ...Option) *Server`; options include `WithProfile`, `WithDefaultProject`, `WithDetectFunc`. |
| R-S1-SVR-03 | MUST expose `(s *Server) Serve(ctx context.Context) error` that starts the MCP stdio JSON-RPC loop and returns when `ctx` is cancelled. |
| R-S1-SVR-04 | MUST register tools via the mcp-go tool registration API; the set of registered tools MUST be filtered by `profile` via `Server.allowsTool(name)` before registration. |
| R-S1-SVR-05 | All public functions and types in `internal/mcp` MUST accept `context.Context` as first parameter where applicable. |
| R-S1-SVR-06 | All exported types and functions MUST carry package-doc comments. |

#### Project resolver

| ID | Requirement |
|----|-------------|
| R-S1-PROJ-01 | Per-call project resolution MUST follow this precedence (first non-empty wins): (1) per-call `project` argument, (2) `s.defaultProj` (from `ION_MEM_PROJECT` env or `--project` flag), (3) per-call `cwd` argument, (4) `project.DetectFull(os.Getwd())`. |
| R-S1-PROJ-02 | The resolved project MUST be cached for the process lifetime when derived from `s.defaultProj`. Re-detection via `project.DetectFull` is acceptable per tool call only when neither `s.defaultProj` nor a per-call override is set. |
| R-S1-PROJ-03 | `os.Getenv` MUST NOT appear outside a single config-loading function (testability guard). |

#### Envelope

| ID | Requirement |
|----|-------------|
| R-ENV-01 | Every envelope response MUST include a top-level `status` field with value `"ok"` or `"error"`. `envelope.Build` MUST default `status` to `"ok"`. |
| R-ENV-02 | `envelope.BuildError(det, error_code, msg string) []byte` MUST exist as a dedicated constructor for error responses. It MUST set `status: "error"`, `error_code` to one of the closed vocabulary values, and `result` to the human-readable `msg`. The `error_code` vocabulary is closed: `not_found`, `db_error`, `invalid_argument`, `project_ambiguous`, `internal`. |
| R-ENV-03 | `ion_current_project` MUST continue to use its own flat response shape (not the standard envelope). When returning an ambiguous-project condition, the `error` field value MUST be `"project_ambiguous"` (aligned with the shared `error_code` vocabulary). No other `ion_current_project` response fields are changed by this change. |
| R-S1-ENV-01 | `envelope.Build(det project.DetectionResult, msg string, extras map[string]any) []byte` MUST be the sole entry point for producing SUCCESS envelope JSON. For error responses, handlers MUST use `envelope.BuildError`. Handlers MUST NOT hand-roll JSON marshaling. |
| R-S1-ENV-02 | Every envelope response MUST contain exactly these five top-level keys: `project` (string), `project_source` (string), `project_path` (string), `result` (string), `status` (`"ok"` or `"error"`). Error envelopes MUST additionally contain `error_code` (string, one of the closed vocabulary values). |
| R-S1-ENV-03 | Per-tool extension fields (e.g., `id`, `sync_id`) MUST be merged at the top level of the same object, NOT nested under a `"data"` key or any other wrapper. |
| R-S1-ENV-04 | When `project.ErrAmbiguousProject` fires in any tool other than `ion_current_project`, the envelope MUST have `project: ""`, `project_source: "ambiguous"`, `result: "ambiguous project — call ion_current_project"`, and `available_projects: [...]` appended as an extension field. |

#### ion_current_project

| ID | Requirement |
|----|-------------|
| R-TOOL-CURRENT-01 | `ion_current_project` MUST accept one optional input: `cwd?: string`. |
| R-TOOL-CURRENT-02 | `ion_current_project` MUST return the `DetectionResult` shape DIRECTLY, NOT wrapped in the standard envelope. Required output fields: `project`, `project_source`, `project_path`, `available_projects` (null when unambiguous), `warning` (empty string when absent). |
| R-TOOL-CURRENT-03 | `ion_current_project` MUST NEVER return a Go-level error. When `ErrAmbiguousProject` fires, it MUST surface as `project: ""`, `project_source: ""`, `project_path: <cwd>`, `error: "project_ambiguous"`, `available_projects: ["a","b"]` within the response body. (Updated to align `error` field value to `"project_ambiguous"` for vocabulary consistency.) |
| R-TOOL-CURRENT-04 | `ion_current_project` MUST NOT attach the standard envelope fields (`result`, `project_source` in the envelope sense). It is the sole exception to the envelope rule. |

#### ion_save

| ID | Requirement |
|----|-------------|
| R-TOOL-SAVE-01 | `ion_save` MUST accept: `title: string (req)`, `content: string`, `type?: string = "manual"`, `project?: string`, `scope?: string = "project"`, `topic_key?: string`, `session_id?: string`, `capture_prompt?: bool = true`, `cwd?: string`. |
| R-TOOL-SAVE-02 | `ion_save` MUST return envelope with extensions: `id: int64`, `sync_id: string`, `revision_count: int`, `duplicate_count: int`, `prompt_attached: bool`. |
| R-TOOL-SAVE-03 | `ion_save` MUST use the cached/resolved project unless the `project` param is supplied; when `project` is supplied it MUST override the cached project and `envelope.project` MUST reflect the override. |
| R-TOOL-SAVE-04 | `ion_save` with `capture_prompt: true` (default) MUST, within a single transaction, SELECT the latest unconsumed `user_prompts` row for the session (`consumed_at IS NULL ORDER BY created_at DESC LIMIT 1`) and UPDATE that row's `consumed_at` to the current UTC time. `prompt_attached` MUST be `true` when a row is consumed, `false` when no unconsumed row exists. The in-memory buffer (previously used) MUST NOT be consulted for prompt capture. |
| R-TOOL-SAVE-05 | `ion_save` MUST delegate to `store.AddObservation`; dedup, topic_key upsert, and soft-delete semantics are inherited from the store layer without reimplementation. |
| R-TOOL-SAVE-06 | `ion_save` on a dedup hash collision (same content, same session) MUST return the existing observation's envelope (not error, not silent new insert). `duplicate_count` MUST be incremented. |
| R-TOOL-SAVE-07 | `ion_save` with an unknown `session_id` argument MUST call `ensureSession` which auto-creates a session. It MUST NOT silently fall through to a nil session. |

#### ion_search

| ID | Requirement |
|----|-------------|
| R-TOOL-SEARCH-01 | `ion_search` MUST accept: `query: string (req)`, `type?: string`, `project?: string`, `scope?: string`, `limit?: int = 10`, `all_projects?: bool = false`, `cwd?: string`. |
| R-TOOL-SEARCH-02 | `ion_search` MUST return envelope with extensions: `results: [{id, sync_id, title, type, project, scope, topic_key?, content_preview, score, created_at}]`, `count: int`. |
| R-TOOL-SEARCH-03 | `ion_search` with zero matching results MUST return `results: []` (empty JSON array). It MUST NOT return a Go error for empty results. |
| R-TOOL-SEARCH-04 | `ion_search` MUST use the cached/resolved project unless `project` param overrides or `all_projects: true`. |
| R-TOOL-SEARCH-05 | `content_preview` MUST be limited to the first 300 characters of the observation content. |

### 2.2 Scenarios (Slice 1)

#### Server scaffold

**S1-T-SVR-01 — profile="agent" registers exactly 3 tools in slice 1**
- GIVEN `Server.New` with `WithProfile("agent")`
- WHEN `Serve` initializes and the tool list is inspected
- THEN exactly 3 tools are registered: `ion_current_project`, `ion_save`, `ion_search`

**S1-T-SVR-02 — profile="all" also registers exactly 3 tools in slice 1 scope**
- GIVEN `Server.New` with `WithProfile("all")`
- WHEN `Serve` initializes (slice 1 only)
- THEN same 3 tools registered (additional tools added per slice)

#### ion_current_project

**S1-T-CURRENT-01 — unambiguous git repo**
- GIVEN cwd is inside a git repo with a remote
- WHEN client calls `ion_current_project`
- THEN response is `{project: <repo-name>, project_source: "git_remote", project_path: <repoRoot>, available_projects: null, warning: ""}`
- AND response contains NO `result` field (direct DetectionResult, not envelope)

**S1-T-CURRENT-02 — ambiguous: two git children**
- GIVEN cwd is a parent directory of two git repos
- WHEN client calls `ion_current_project`
- THEN response is `{project: "", project_source: "", project_path: <cwd>, error: "project_ambiguous", available_projects: ["a","b"]}`
- AND NO Go-level error is returned by the handler

**S1-T-CURRENT-03 — cwd override via argument**
- GIVEN `ion_current_project` called with `cwd: "/some/other/path"` that is a valid git repo
- WHEN handler executes
- THEN detection runs against the supplied cwd, NOT `os.Getwd()`

**S1-T-CURRENT-04 — env override via ION_MEM_PROJECT**
- GIVEN `ION_MEM_PROJECT=my-project` is set in process env
- WHEN client calls `ion_current_project`
- THEN response has `project: "my-project"`, `project_source: "env_override"`

#### ion_save

**S1-T-SAVE-01 — happy path with prompt attachment**
- GIVEN store empty, project resolved, last prompt buffered for the session
- WHEN client calls `ion_save` with `title="Test"`, `content="Body"`, `capture_prompt=true` (default)
- THEN envelope returned with `id > 0`, `sync_id` non-empty, `prompt_attached: true`
- AND store has 1 observation; observation is linked to the buffered prompt

**S1-T-SAVE-02 — topic_key upsert**
- GIVEN an existing observation with `topic_key: "arch/foo"`
- WHEN client calls `ion_save` with `topic_key: "arch/foo"` and new content
- THEN response `id` equals the prior observation's id
- AND `revision_count` is incremented by 1

**S1-T-SAVE-03 — cached project used by default**
- GIVEN resolved project is "ion-memory" (no per-call override)
- WHEN client calls `ion_save` without `project` param
- THEN `envelope.project` equals "ion-memory"

**S1-T-SAVE-04 — per-call project override**
- GIVEN resolved project is "ion-memory"
- WHEN client calls `ion_save` with `project: "other-project"`
- THEN `envelope.project` equals "other-project"
- AND observation stored under "other-project"

**S1-T-SAVE-05 — dedup collision returns existing**
- GIVEN an observation already saved with content "C"
- WHEN client calls `ion_save` with identical content "C" in same session
- THEN `duplicate_count` >= 1
- AND store row count does NOT increase

**S1-T-SAVE-06 — no prompt buffer: prompt_attached false**
- GIVEN no prior `ion_save_prompt` call for this session
- WHEN client calls `ion_save` with `capture_prompt: true`
- THEN `prompt_attached: false`

#### ion_search

**S1-T-SEARCH-01 — matches ordered by score**
- GIVEN store seeded with 3 observations; query "X" matches 2 of them
- WHEN client calls `ion_search` with `query: "X"`
- THEN response `results` has 2 entries
- AND entries are ordered highest score first

**S1-T-SEARCH-02 — empty results returns empty array, not error**
- GIVEN store is empty
- WHEN client calls `ion_search` with `query: "nonexistent"`
- THEN `results` is `[]`
- AND `count` is 0
- AND no Go error is returned

**S1-T-SEARCH-03 — per-call project override**
- GIVEN resolved project is "ion-memory"
- WHEN client calls `ion_search` with `project: "other"`
- THEN search scopes to "other", not "ion-memory"

**S1-T-SEARCH-04 — all_projects bypasses project filter**
- GIVEN two projects each with 1 observation
- WHEN client calls `ion_search` with `all_projects: true`
- THEN `results` includes observations from both projects

#### Envelope

**S1-T-ENV-01 — all four standard fields present on every envelope tool**
- GIVEN any tool other than `ion_current_project`
- WHEN response is inspected
- THEN `project`, `project_source`, `project_path`, `result` are all present at top level
- AND no `"data"` wrapper object exists

**S1-T-ENV-02 — extension fields at top level, not nested**
- GIVEN `ion_save` response
- WHEN JSON is parsed
- THEN `id`, `sync_id`, `revision_count`, `duplicate_count`, `prompt_attached` are direct top-level keys

**S1-T-ENV-03 — ambiguous project envelope shape**
- GIVEN cwd has two git children (ambiguous)
- WHEN any non-current-project tool is called
- THEN envelope has `project: ""`, `project_source: "ambiguous"`, `available_projects: [...]`
- AND `result` contains "call ion_current_project"

### 2.3 Acceptance Criteria (Slice 1)

- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/mcp/...` exits 0 with >= 10 test functions
- [ ] `go vet ./...` and `gofmt -l .` produce no output
- [ ] Exactly 3 tools register under profile "agent" in slice 1
- [ ] In-process MCP client can call all 3 tools successfully end-to-end
- [ ] Coverage >= 70% on `internal/mcp/...` for slice 1 surface

---

## 3. Slice 2 — 7 daily-driver tools

### 3.1 Requirements

#### ion_context

| ID | Requirement |
|----|-------------|
| R-S2-CTX-01 | `ion_context` MUST accept: `project?: string`, `limit?: int = 10`, `cwd?: string`. |
| R-S2-CTX-02 | `ion_context` MUST return the standard envelope with `result` set to a markdown string composed from `store.RecentSessions` and `store.RecentObservations` for the resolved project. |
| R-S2-CTX-03 | When the project has no observations, `ion_context` MUST return `result` as a valid empty markdown string (NOT a Go error). |

#### ion_get_observation

| ID | Requirement |
|----|-------------|
| R-S2-GET-01 | `ion_get_observation` MUST accept: `id: int64 (req)`. |
| R-S2-GET-02 | `ion_get_observation` MUST return envelope with extension `observation: {id, sync_id, session_id, type, title, content, tool_name?, project, scope, topic_key?, revision_count, duplicate_count, last_seen_at, created_at, updated_at}`. |
| R-S2-GET-03 | `ion_get_observation` on a missing or soft-deleted `id` MUST return an envelope with `result` describing the error, NOT a Go-level error. |

#### ion_session_start

| ID | Requirement |
|----|-------------|
| R-S2-SS-01 | `ion_session_start` MUST accept: `session_id: string (req)`, `project?: string`, `directory?: string`, `cwd?: string`. |
| R-S2-SS-02 | `ion_session_start` MUST return envelope with extensions: `session_id: string`, `created: bool`. |
| R-S2-SS-03 | `ion_session_start` MUST be idempotent: a duplicate `session_id` MUST return the existing session with `created: false`, NOT an error. |

#### ion_session_end

| ID | Requirement |
|----|-------------|
| R-S2-SE-01 | `ion_session_end` MUST accept: `session_id: string (req)`, `summary?: string = ""`. |
| R-S2-SE-02 | `ion_session_end` MUST return envelope with extensions: `session_id: string`, `ended_at: string`. |
| R-S2-SE-03 | `ion_session_end` on an unknown `session_id` MUST return envelope with an error description in `result`, NOT a Go-level error. |

#### ion_session_summary

| ID | Requirement |
|----|-------------|
| R-S2-SSUM-01 | `ion_session_summary` MUST accept: `summary: string (req)`, `session_id?: string`, `project?: string`, `topic_key?: string`, `cwd?: string`. |
| R-S2-SSUM-02 | `ion_session_summary` MUST store the summary as an observation with `type: "session_summary"`, `title: "Session summary: <project>"`. |
| R-S2-SSUM-03 | `ion_session_summary` MUST return envelope with extensions: `session_id: string`, `observation_id: int64`, `sync_id: string`. |
| R-S2-SSUM-04 | When no `session_id` is supplied, `ion_session_summary` MUST auto-create or reuse the process-lifetime session for the resolved project (same as `ensureSession` behavior). |

#### ion_save_prompt

| ID | Requirement |
|----|-------------|
| R-S2-SP-01 | `ion_save_prompt` MUST accept: `content: string (req)`, `session_id?: string`, `project?: string`, `cwd?: string`. |
| R-S2-SP-02 | `ion_save_prompt` MUST call `store.AddPromptIfMissing` to persist the prompt row. It MUST NOT write to any in-memory buffer. The tool response remains unchanged. |
| R-S2-SP-03 | `ion_save_prompt` MUST return envelope with extensions: `id: int64`, `sync_id: string`, `session_id: string`. |

#### ion_suggest_topic_key

| ID | Requirement |
|----|-------------|
| R-S2-STK-01 | `ion_suggest_topic_key` MUST accept: `title: string (req)`, `type?: string`. |
| R-S2-STK-02 | `ion_suggest_topic_key` MUST return envelope with extension `topic_key: string` in `family/specific-description` format. |
| R-S2-STK-03 | `ion_suggest_topic_key` MUST be a pure function: it MUST NOT call any `store` method. |
| R-S2-STK-04 | Key generation rule: lowercase the title, replace non-`[a-z0-9]` characters with `-`, collapse consecutive hyphens, strip leading/trailing hyphens. Prefix with `<type>/` when `type` is provided. |

#### Profile

| ID | Requirement |
|----|-------------|
| R-S2-PROFILE-01 | After slice 2, profile "agent" MUST register exactly 10 tools: the 3 from slice 1 plus `ion_context`, `ion_get_observation`, `ion_session_start`, `ion_session_end`, `ion_session_summary`, `ion_save_prompt`, `ion_suggest_topic_key`. |
| R-S2-PROFILE-02 | Profile "all" MUST register the same 10 tools after slice 2 (admin tools are future scope). |

#### Session + prompt integration

| ID | Requirement |
|----|-------------|
| R-S2-SESSION-01 | `ion_save_prompt` followed by `ion_save` (with `capture_prompt: true`) within the same MCP session MUST result in the `user_prompts` row being consumed (`consumed_at` set) and `prompt_attached: true`. This guarantee survives process restarts between the two calls. |
| R-S2-SESSION-02 | A repeated identical `ion_save_prompt` call (same `session_id` and `content`) hits the existing row via `(session_id, content)` dedup. If that row is already consumed (`consumed_at IS NOT NULL`), a subsequent `ion_save` MUST NOT re-consume it. A new distinct prompt (different `content`) creates a new row and is available for consumption independently. |

### 3.2 Scenarios (Slice 2)

#### ion_context

**S2-T-CTX-01 — non-empty markdown after saves**
- GIVEN project has 3 observations saved
- WHEN client calls `ion_context`
- THEN `result` is a non-empty markdown string mentioning the observations

**S2-T-CTX-02 — empty project returns valid empty markdown**
- GIVEN project has no observations
- WHEN client calls `ion_context`
- THEN `result` is a valid (possibly empty) markdown string
- AND NO Go error is returned

#### ion_get_observation

**S2-T-GET-01 — happy path retrieves full content**
- GIVEN observation id=5 exists with title "T" and content "C"
- WHEN client calls `ion_get_observation` with `id: 5`
- THEN `observation.title` equals "T", `observation.content` equals "C"

**S2-T-GET-02 — missing id returns error in result, not Go error**
- GIVEN no observation with id=999
- WHEN client calls `ion_get_observation` with `id: 999`
- THEN envelope `result` describes "not found"
- AND handler does not return a Go-level error

#### ion_session_start

**S2-T-SS-01 — new session created**
- GIVEN session id "sess-abc" does not exist
- WHEN client calls `ion_session_start` with `session_id: "sess-abc"`
- THEN `created: true`, `session_id: "sess-abc"` returned

**S2-T-SS-02 — duplicate id is idempotent**
- GIVEN session id "sess-abc" already exists
- WHEN client calls `ion_session_start` with `session_id: "sess-abc"` again
- THEN `created: false`, `session_id: "sess-abc"` returned
- AND NO error at any layer

#### ion_session_end

**S2-T-SE-01 — ends known session**
- GIVEN session "sess-abc" is open
- WHEN client calls `ion_session_end` with `session_id: "sess-abc"`
- THEN `ended_at` is a non-empty timestamp, `session_id: "sess-abc"`

**S2-T-SE-02 — unknown session returns error in result**
- GIVEN session "sess-missing" does not exist
- WHEN client calls `ion_session_end` with `session_id: "sess-missing"`
- THEN envelope `result` contains an error description
- AND handler does NOT return a Go-level error

#### ion_session_summary

**S2-T-SSUM-01 — saves as session_summary type**
- GIVEN session "sess-abc" is open
- WHEN client calls `ion_session_summary` with `session_id: "sess-abc"`, `summary: "We did X"`
- THEN observation stored with `type: "session_summary"`, `content: "We did X"`
- AND `observation_id > 0`, `sync_id` non-empty

**S2-T-SSUM-02 — no session_id auto-creates session**
- GIVEN no session exists for the resolved project
- WHEN client calls `ion_session_summary` without `session_id`
- THEN a session is auto-created and `session_id` is returned in the envelope

#### ion_save_prompt

**S2-T-SP-01 — prompt written to DB and consumed by ion_save**
- GIVEN no prior prompt for this session
- WHEN client calls `ion_save_prompt` with `content: "User asked X"`, then `ion_save` with default `capture_prompt: true`
- THEN the `user_prompts` row has `consumed_at` set to a non-null timestamp
- AND `prompt_attached: true` in `ion_save` response

**S2-T-SP-02 — latest unconsumed prompt consumed by ion_save**
- GIVEN client calls `ion_save_prompt` twice: "First prompt" then "Second prompt" (both unconsumed)
- WHEN client calls `ion_save` with `capture_prompt: true`
- THEN the "Second prompt" row (latest unconsumed) is consumed
- AND `prompt_attached: true`

**S2-T-SP-03 — empty content not stored**
- GIVEN client calls `ion_save_prompt` with `content: ""`
- WHEN the call completes
- THEN `result` contains an error description
- AND subsequent `ion_save` has `prompt_attached: false`

#### ion_suggest_topic_key

**S2-T-STK-01 — type prefix applied**
- GIVEN `ion_suggest_topic_key` called with `title: "Auth Model"`, `type: "architecture"`
- WHEN handler executes
- THEN `topic_key: "architecture/auth-model"` (no store call made)

**S2-T-STK-02 — no type, no prefix**
- GIVEN `ion_suggest_topic_key` called with `title: "My Decision"`, no `type`
- WHEN handler executes
- THEN `topic_key: "my-decision"`

**S2-T-STK-03 — special characters normalized**
- GIVEN title "Fix: Auth & Session!!"
- WHEN handler executes
- THEN `topic_key` contains only `[a-z0-9-]` characters, no consecutive hyphens

### 3.3 Acceptance Criteria (Slice 2)

- [ ] All slice 1 tests still pass
- [ ] Slice 2 adds >= 18 new test functions
- [ ] Profile "agent" registers exactly 10 tools
- [ ] `ion_save_prompt` + `ion_save` prompt-attachment integration test passes
- [ ] `ion_session_start` idempotency test passes
- [ ] Coverage >= 72% on `internal/mcp/...` overall

---

## 4. Slice 3 — 4 utility tools + polish

### 4.1 Requirements

#### ion_update

| ID | Requirement |
|----|-------------|
| R-S3-UPD-01 | `ion_update` MUST accept: `id: int64 (req)`, `title?: string`, `content?: string`, `type?: string`, `topic_key?: string`, `tool_name?: string`. |
| R-S3-UPD-02 | `ion_update` MUST return envelope with extensions: `id: int64`, `sync_id: string`, `revision_count: int`, `updated_at: string`. |
| R-S3-UPD-03 | `ion_update` MUST preserve fields that are absent from the call (patch semantics, not replace). |
| R-S3-UPD-04 | `ion_update` on a missing `id` MUST return envelope with error description in `result`, NOT a Go-level error. |

#### ion_delete

| ID | Requirement |
|----|-------------|
| R-S3-DEL-01 | `ion_delete` MUST accept: `id: int64 (req)`, `hard?: bool = false`. |
| R-S3-DEL-02 | `ion_delete` with `hard: false` (default) MUST perform a soft delete; the observation MUST NOT appear in subsequent `ion_search` results. |
| R-S3-DEL-03 | `ion_delete` with `hard: true` MUST permanently remove the row from storage. |
| R-S3-DEL-04 | `ion_delete` MUST return envelope with extensions: `id: int64`, `hard: bool`. |

#### ion_timeline

| ID | Requirement |
|----|-------------|
| R-S3-TL-01 | `ion_timeline` MUST accept: `observation_id: int64 (req)`, `before?: int = 5`, `after?: int = 5`. |
| R-S3-TL-02 | `ion_timeline` MUST return envelope with extensions: `anchor_id: int64`, `entries: [{kind: "observation"\|"prompt", id, content_preview, created_at, ...}]`. |
| R-S3-TL-03 | When there are no entries before or after the anchor, the corresponding slice MUST be an empty array (`[]`), NOT null, NOT an error. |

#### ion_stats

| ID | Requirement |
|----|-------------|
| R-S3-STATS-01 | `ion_stats` MUST accept: `cwd?: string`. |
| R-S3-STATS-02 | `ion_stats` MUST return envelope with extensions: `total_sessions: int`, `total_observations: int`, `total_prompts: int`, `by_project: [{project, observation_count, prompt_count}]`. |
| R-S3-STATS-03 | `ion_stats` MUST reflect the current store state at call time (no caching). |

#### Profile and integration

| ID | Requirement |
|----|-------------|
| R-S3-PROFILE-01 | After slice 3, profile "agent" MUST register exactly 14 tools total (11 from slices 1+2, plus `ion_update`, `ion_delete`, `ion_timeline`, `ion_stats`). Wait — per design §3.6 `agentTools` has 11 entries. `ion_timeline` and `ion_stats` bring total to 14 counting all three slices. Profile "all" also registers 14 (no admin tools in MVP). |
| R-S3-INT-01 | A single end-to-end integration test MUST exercise the full lifecycle: `ion_session_start` → `ion_save_prompt` → `ion_save` → `ion_search` → `ion_get_observation` → `ion_context` → `ion_session_summary` → `ion_session_end` → `ion_stats`. |
| R-S3-INT-02 | The integration test MUST assert expected observation and session counts in `ion_stats` at the end of the lifecycle. |

### 4.2 Scenarios (Slice 3)

#### ion_update

**S3-T-UPD-01 — patch preserves unchanged fields**
- GIVEN observation id=1 with `title: "Old"`, `content: "Body"`, `type: "manual"`
- WHEN client calls `ion_update` with `id: 1`, `title: "New"` (no `content`, no `type`)
- THEN `title` is "New"; `content` remains "Body"; `type` remains "manual"
- AND `revision_count` is incremented

**S3-T-UPD-02 — missing id returns error in result**
- GIVEN no observation with id=999
- WHEN client calls `ion_update` with `id: 999`
- THEN `result` contains error description; no Go-level error

#### ion_delete

**S3-T-DEL-01 — soft delete hides from search**
- GIVEN observation id=1 is searchable by its content
- WHEN client calls `ion_delete` with `id: 1` (default `hard: false`)
- THEN subsequent `ion_search` does NOT return id=1

**S3-T-DEL-02 — hard delete permanently removes**
- GIVEN observation id=2 exists
- WHEN client calls `ion_delete` with `id: 2`, `hard: true`
- THEN `ion_get_observation` with `id: 2` returns "not found" in `result`

#### ion_timeline

**S3-T-TL-01 — entries within window**
- GIVEN 10 observations in sequence; anchor is id=5
- WHEN client calls `ion_timeline` with `observation_id: 5`, `before: 2`, `after: 2`
- THEN `entries` contains at most 4 items (2 before, 2 after, not counting anchor)

**S3-T-TL-02 — empty before/after slices are arrays not null**
- GIVEN anchor is the first observation (nothing before it)
- WHEN client calls `ion_timeline` with `observation_id: <first>`, `before: 5`
- THEN the "before" portion of `entries` is `[]` (empty array), NOT null

#### ion_stats

**S3-T-STATS-01 — reflects current state**
- GIVEN store has 2 sessions, 5 observations, 3 prompts
- WHEN client calls `ion_stats`
- THEN `total_sessions: 2`, `total_observations: 5`, `total_prompts: 3`

**S3-T-INT-01 — full lifecycle integration**
- GIVEN fresh in-memory store with temp dir
- WHEN the following are called in sequence: `ion_session_start` → `ion_save_prompt` → `ion_save` → `ion_search` → `ion_get_observation` → `ion_context` → `ion_session_summary` → `ion_session_end` → `ion_stats`
- THEN each call returns its expected envelope shape
- AND `ion_stats` shows `total_observations: 2` (one from `ion_save`, one from `ion_session_summary`), `total_prompts: 1`, `total_sessions: 1`

### 4.3 Acceptance Criteria (Slice 3)

- [ ] All slice 1 and slice 2 tests still pass
- [ ] Slice 3 adds >= 12 new test functions
- [ ] Profile "agent" registers exactly 14 tools total
- [ ] Profile "all" registers exactly 14 tools total (no admin in MVP)
- [ ] Coverage >= 75% on `internal/mcp/...` overall
- [ ] Full lifecycle integration test passes and asserts expected counts
- [ ] `go vet ./...` and `gofmt -l .` produce no output (clean)

---

## 5. Cross-cutting requirements

| ID | Requirement |
|----|-------------|
| R-CC-01 | All public functions MUST accept `context.Context` as first parameter where the function involves I/O or external calls. |
| R-CC-02 | All exported types and functions MUST carry godoc comments. |
| R-CC-03 | `internal/mcp` MUST NOT import any internal package other than `internal/store` and `internal/project`. |
| R-CC-04 | `os.Getenv` MUST NOT appear outside a single config-loading function in the package (testability). |
| R-CC-05 | All tool names MUST start with the `ion_` prefix. No tool MUST use `mem_*` naming. |
| R-CC-06 | `envelope.Build` is the SOLE entry point for producing envelope JSON. Handlers MUST NOT hand-roll JSON marshaling per tool. |
| R-CC-07 | Tests MUST use real `*store.Store` with `t.TempDir()`. Mocks are NOT permitted at the store layer. |
| R-CC-08 | Test helpers MUST be in `mcp_helpers_test.go`: `mustServer(t)`, `mustCall(t, srv, toolName, args)`, `mustEnvelope(t, raw []byte)`. |
| R-CC-09 | No `testify` dependency. Tests use stdlib assertions only. |
| R-CC-10 | Table-driven tests MUST be used where multiple cases exercise the same behavior. |

---

## 6. Silent fall-through and error semantics — explicit contract

The following per-tool rules are stated explicitly because apply has historically misread
"always returns a usable result" as permission to silently ignore errors.

| Tool | Behavior on error condition | MUST NOT do |
|------|----------------------------|-------------|
| `ion_current_project` | Ambiguous project → return structured body with `error: "project_ambiguous"`. Any other detection error → return with `project: ""`, describe error in `warning` field. | Return Go error. Panic. Return empty struct. |
| `ion_save` | Unknown session_id → call `ensureSession` (auto-creates). Dedup collision → return existing observation envelope, increment `duplicate_count`. | Silently insert duplicate. Return nil id. |
| `ion_search` | Zero results → return `results: []`, `count: 0`. FTS5 syntax error → return envelope with error in `result`. | Return Go error for empty results. Return null for `results`. |
| `ion_get_observation` | Missing id → envelope with error in `result`. | Return Go error. Panic. |
| `ion_session_start` | Duplicate id → return existing session, `created: false`. | Return Go error. Return new session with same id. |
| `ion_session_end` | Unknown id → envelope with error in `result`. | Return Go error. |
| `ion_update` | Missing id → envelope with error in `result`. | Return Go error. Silently no-op. |
| `ion_delete` | Missing id → envelope with error in `result`. | Return Go error. Silently no-op. |
| `ion_save_prompt` | Empty content → MUST NOT overwrite buffer; return envelope with error in `result`. | Overwrite buffer with empty string. |
| `ion_context` | No observations for project → return valid (empty) markdown. | Return Go error. Return null. |
| `ion_timeline` | No entries before/after anchor → return empty array `[]`. | Return null. Return Go error. |

---

## 7. Out of scope

The following are explicitly deferred and MUST NOT be implemented in this change:

- `ion_judge`, `ion_compare` — needs `memory_relations` table; deferred to `mcp-conflict-surfacing`
- `ion_doctor`, `ion_merge_projects` — admin tools; deferred to `mcp-admin`
- `ion_capture_passive` — deferred
- HTTP REST API — deferred to `local-api-mvp`
- Cloud sync, setup installer, TUI — separate changes
- CLI wiring in `cmd/ion-mem/main.go` — deferred to `cli-mvp`
- Write queue (`write_queue.go`) — `SetMaxOpenConns(1)` is sufficient for MVP
