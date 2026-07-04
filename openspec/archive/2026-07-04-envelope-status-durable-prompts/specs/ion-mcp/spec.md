---
ion-sdd-version: "1.0"
phase: ion-sdd-spec
generated: "2026-07-04T00:00:00Z"
mode: openspec
change: "envelope-status-durable-prompts"
capability: "ion-mcp"
spec_kind: delta
---

# Delta for ion-mcp

## ADDED Requirements

### Requirement: R-ENV-01 — Envelope status field

Every envelope response MUST include a top-level `status` field with value `"ok"` or `"error"`. `envelope.Build` MUST default `status` to `"ok"`.

#### Scenario: Success path status is ok

- GIVEN any tool other than `ion_current_project` completes without error
- WHEN the envelope JSON is inspected
- THEN `status` equals `"ok"`
- AND all existing required fields (`project`, `project_source`, `project_path`, `result`) are still present

#### Scenario: Backward compatibility — status is additive

- GIVEN an existing client that does not read the `status` field
- WHEN it receives a response with `status: "ok"`
- THEN the response remains valid (no existing field removed or renamed)

---

### Requirement: R-ENV-02 — Envelope error_code field

`envelope.BuildError(det, error_code, msg string) []byte` MUST exist as a dedicated constructor for error responses. It MUST set `status: "error"`, `error_code` to one of the closed vocabulary values, and `result` to the human-readable `msg`. The `error_code` vocabulary is closed: `not_found`, `db_error`, `invalid_argument`, `project_ambiguous`, `internal`.

#### Scenario: BuildError produces error envelope

- GIVEN a handler encounters a missing observation
- WHEN it calls `BuildError(det, "not_found", "observation 99 not found")`
- THEN the returned JSON has `status: "error"`, `error_code: "not_found"`, `result: "observation 99 not found"`
- AND `project`, `project_source`, `project_path` are present

#### Scenario: Error code mapping — store lookup failure uses db_error

- GIVEN a handler receives a database-layer error (not a not-found sentinel)
- WHEN it produces the error envelope
- THEN `error_code` equals `"db_error"`

#### Scenario: Error code mapping — malformed argument uses invalid_argument

- GIVEN a handler detects an empty required field (e.g. empty content)
- WHEN it produces the error envelope
- THEN `error_code` equals `"invalid_argument"`

#### Scenario: Error code mapping — ambiguous project uses project_ambiguous

- GIVEN `ErrAmbiguousProject` fires in a non-`ion_current_project` tool
- WHEN the handler produces the error envelope
- THEN `error_code` equals `"project_ambiguous"`

#### Scenario: Error code mapping — unexpected failures use internal

- GIVEN an unexpected runtime error is caught by the handler
- WHEN it produces the error envelope
- THEN `error_code` equals `"internal"`

---

### Requirement: R-ENV-03 — ion_current_project error vocabulary alignment

`ion_current_project` MUST continue to use its own flat response shape (not the standard envelope). When returning an ambiguous-project condition, the `error` field value MUST be `"project_ambiguous"` (aligned with the shared `error_code` vocabulary). No other `ion_current_project` response fields are changed by this change.

#### Scenario: Ambiguous project — aligned error key value

- GIVEN cwd has two git children
- WHEN `ion_current_project` is called
- THEN the response body has `error: "project_ambiguous"` (was `"ambiguous_project"` pre-change)
- AND the response still contains `project: ""`, `project_source: ""`, `available_projects: [...]`
- AND no `status`, `error_code`, or `result` envelope fields appear

---

## MODIFIED Requirements

### Requirement: R-S1-ENV-01 — Envelope entry point (Build)

`envelope.Build(det project.DetectionResult, msg string, extras map[string]any) []byte` MUST be the sole entry point for producing SUCCESS envelope JSON. For error responses, handlers MUST use `envelope.BuildError`. Handlers MUST NOT hand-roll JSON marshaling.

(Previously: `Build` was the sole entry point for ALL responses; no separate error constructor existed.)

#### Scenario: Build used only for success

- GIVEN a handler completes successfully
- WHEN it calls `envelope.Build`
- THEN the returned JSON has `status: "ok"`

#### Scenario: BuildError used for errors

- GIVEN a handler encounters any error condition
- WHEN it calls `envelope.BuildError`
- THEN the returned JSON has `status: "error"` and a valid `error_code`

---

### Requirement: R-S1-ENV-02 — Standard envelope fields

Every envelope response MUST contain exactly these five top-level keys: `project` (string), `project_source` (string), `project_path` (string), `result` (string), `status` (`"ok"` or `"error"`). Error envelopes MUST additionally contain `error_code` (string, one of the closed vocabulary values).

(Previously: four required fields only — `project`, `project_source`, `project_path`, `result`. No `status` or `error_code`.)

#### Scenario: Success envelope has exactly five standard fields

- GIVEN any non-`ion_current_project` tool succeeds
- WHEN the response JSON is parsed
- THEN `project`, `project_source`, `project_path`, `result`, `status` are all present at top level
- AND `status` is `"ok"`

#### Scenario: Error envelope has six standard fields

- GIVEN any non-`ion_current_project` tool encounters an error
- WHEN the response JSON is parsed
- THEN the five standard fields plus `error_code` are all present
- AND `status` is `"error"`
- AND `error_code` is one of: `not_found`, `db_error`, `invalid_argument`, `project_ambiguous`, `internal`

---

### Requirement: R-TOOL-CURRENT-03 — ion_current_project ambiguous error field value

`ion_current_project` MUST NEVER return a Go-level error. When `ErrAmbiguousProject` fires, it MUST surface as `project: ""`, `project_source: ""`, `project_path: <cwd>`, `error: "project_ambiguous"`, `available_projects: ["a","b"]` within the response body.

(Previously: `error` field value was `"ambiguous_project"`; now aligned to `"project_ambiguous"` to match shared vocabulary.)

#### Scenario: Ambiguous detection returns updated error value

- GIVEN cwd is a parent directory of two git repos
- WHEN client calls `ion_current_project`
- THEN `error` equals `"project_ambiguous"` (not `"ambiguous_project"`)
- AND NO Go-level error is returned by the handler

---

### Requirement: R-TOOL-SAVE-04 — ion_save prompt capture via durable store

`ion_save` with `capture_prompt: true` (default) MUST, within a single transaction, SELECT the latest unconsumed `user_prompts` row for the session (`consumed_at IS NULL ORDER BY created_at DESC LIMIT 1`) and UPDATE that row's `consumed_at` to the current UTC time. `prompt_attached` MUST be `true` when a row is consumed, `false` when no unconsumed row exists. The in-memory buffer (previously used) MUST NOT be consulted for prompt capture.

(Previously: capture read from in-memory `promptsBySession` buffer; now reads durably from the DB.)

#### Scenario: Prompt consumed from DB on save

- GIVEN `ion_save_prompt` was called (inserting a `user_prompts` row with `consumed_at IS NULL`)
- WHEN `ion_save` is called with `capture_prompt: true`
- THEN the `user_prompts` row has `consumed_at` set to a non-null timestamp
- AND `prompt_attached: true` in the response

#### Scenario: No unconsumed prompt — prompt_attached false

- GIVEN no `user_prompts` row exists with `consumed_at IS NULL` for this session
- WHEN `ion_save` is called with `capture_prompt: true`
- THEN `prompt_attached: false`
- AND no rows are modified in `user_prompts`

#### Scenario: Second save in same turn — prompt not re-attached

- GIVEN `ion_save_prompt` was called once, then `ion_save` was called (consuming the prompt)
- WHEN `ion_save` is called again in the same session
- THEN `prompt_attached: false` (already-consumed row is not re-consumed)

#### Scenario: Process-restart durability

- GIVEN `ion_save_prompt` was called and the MCP process restarted before `ion_save`
- WHEN `ion_save` is called after restart with `capture_prompt: true`
- THEN the `user_prompts` row (written before restart) is found and consumed
- AND `prompt_attached: true`

---

### Requirement: R-S2-SP-02 — ion_save_prompt write path (durable only)

`ion_save_prompt` MUST call `store.AddPromptIfMissing` to persist the prompt row. It MUST NOT write to any in-memory buffer. The tool response remains unchanged.

(Previously: `ion_save_prompt` called both `store.AddPromptIfMissing` AND `Server.recordPrompt` to fill the single-slot buffer. The buffer write is removed.)

#### Scenario: Prompt is written to DB only

- GIVEN `ion_save_prompt` is called with valid content
- WHEN the call completes
- THEN a row exists in `user_prompts` with `consumed_at IS NULL`
- AND no in-memory buffer is updated

#### Scenario: Dedup — same (session, content) pair returns existing row

- GIVEN a `user_prompts` row with `session_id=S`, `content=C`, `consumed_at IS NULL`
- WHEN `ion_save_prompt` is called again with the same session and content
- THEN no new row is inserted (dedup via `AddPromptIfMissing`)
- AND the existing row's `consumed_at` remains NULL

---

### Requirement: R-S2-SESSION-01 — Session prompt integration (durable path)

`ion_save_prompt` followed by `ion_save` (with `capture_prompt: true`) within the same MCP session MUST result in the `user_prompts` row being consumed (`consumed_at` set) and `prompt_attached: true`. This guarantee survives process restarts between the two calls.

(Previously: relied on in-memory buffer surviving both calls in the same process lifetime.)

#### Scenario: Cross-call prompt attachment works after restart

- GIVEN `ion_save_prompt` called in one process instance
- WHEN `ion_save` called in a new process instance for the same session
- THEN `prompt_attached: true`

---

### Requirement: R-S2-SESSION-02 — Deduplicated prompt and consumption interaction

A repeated identical `ion_save_prompt` call (same `session_id` and `content`) hits the existing row via `(session_id, content)` dedup. If that row is already consumed (`consumed_at IS NOT NULL`), a subsequent `ion_save` MUST NOT re-consume it. A new distinct prompt (different `content`) creates a new row and is available for consumption independently.

(Previously: the single-slot buffer would silently overwrite. The new behavior is governed by DB state.)

#### Scenario: Consumed prompt not re-consumed via dedup

- GIVEN a `user_prompts` row with `content=C` already consumed (`consumed_at IS NOT NULL`)
- WHEN `ion_save_prompt` is called again with the same `content=C` (returns existing row)
- AND `ion_save` is called
- THEN `prompt_attached: false` (no unconsumed row available)

#### Scenario: New distinct content creates consumable row

- GIVEN the previous prompt `content=C` is already consumed
- WHEN `ion_save_prompt` is called with `content=C2` (new content)
- AND `ion_save` is called
- THEN `prompt_attached: true` (new row with `consumed_at IS NULL` was found and consumed)

---

## REMOVED Requirements

### Requirement: R-S2-SP-04 — Single-slot buffer latest-only rule (in-memory)

(Reason: The in-memory single-slot buffer (`promptsBySession`, `promptMu`, `recordPrompt`, `lastPromptForSession`, `RecordPromptForTest`) is removed. Latest-per-session prompt semantics are now enforced by the DB query `ORDER BY created_at DESC LIMIT 1` on unconsumed rows, which selects the most recently written unconsumed prompt.)
(Migration: Tests that assert buffer overwrite behavior (e.g. `TestPromptBuffer_single_slot_overwrite`) must be replaced with DB-layer tests asserting the `consumed_at` fetch-and-consume query behavior. `RecordPromptForTest` must be removed from the test surface.)
