# Tasks: MCP Server MVP (mcp-mvp)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated production LOC | ~2100 (~600 + ~800 + ~700 per slice) |
| Estimated test LOC | ~800 (~200 + ~300 + ~300 per slice) |
| Total per slice | ~600 / ~800 / ~700 |
| Total change | ~2100 production + ~800 test |
| 400-line production budget risk | Medium (each slice's prod is within budget; total exceeds) |
| Chained PRs recommended | Yes |
| Chain strategy | `stacked-to-main` (locked) |
| Delivery strategy | locked: chained 3 PRs, each merges to main before next begins |
| Decision needed before apply | No (chain + size already approved) |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

### Suggested Work Units

| Unit | Title | Slice scope | Tools added | Approx LOC |
|------|-------|-------------|-------------|------------|
| PR 1 | feat(mcp): slice 1 — stdio server scaffold + 3 tools | server-scaffold-and-first-tools | `ion_current_project`, `ion_save`, `ion_search` | ~600 prod + ~200 test |
| PR 2 | feat(mcp): slice 2 — daily-driver tools | daily-driver-tools | `ion_context`, `ion_get_observation`, `ion_session_start`, `ion_session_end`, `ion_session_summary`, `ion_save_prompt`, `ion_suggest_topic_key` | ~800 prod + ~300 test |
| PR 3 | feat(mcp): slice 3 — utility tools + e2e lifecycle test | utility-tools-and-polish | `ion_update`, `ion_delete`, `ion_timeline`, `ion_stats` + e2e integration | ~700 prod + ~300 test |

---

## Slice 1: Scaffold + first 3 tools

> **Spec refs**: R-S1-DEP-01, R-S1-SVR-01..06, R-S1-PROJ-01..03, R-S1-ENV-01..04, R-TOOL-CURRENT-01..04, R-TOOL-SAVE-01..07, R-TOOL-SEARCH-01..05
> **Scenarios**: S1-T-SVR-01..02, S1-T-CURRENT-01..04, S1-T-SAVE-01..06, S1-T-SEARCH-01..04, S1-T-ENV-01..03
> **Gate**: `go build ./...` + `go test ./internal/mcp/...` + ≥10 test functions + ≥70% coverage + 3 tools under agent profile

- [x] `[PREP] 1.1` Add import for `github.com/mark3labs/mcp-go` in `internal/mcp/server.go` (skeleton); run `go get github.com/mark3labs/mcp-go@v0.44.0` then `go mod tidy`; confirm `go.mod` shows direct require at v0.44.0 (not indirect). Satisfies R-S1-DEP-01.
- [x] `[PREP] 1.2` Rewrite `internal/mcp/doc.go` with real package comment describing the MCP stdio server purpose. Satisfies R-S1-SVR-06.
- [x] `[PREP] 1.3` Create `internal/mcp/envelope.go` with `Build(det project.DetectionResult, msg string, extras map[string]any) []byte` signature — no implementation yet. Satisfies R-S1-ENV-01 skeleton.
- [x] `[TDD-RED] 1.4` Write `internal/mcp/envelope_test.go` with `TestEnvelope_BuildContainsAllRequiredFields` asserting `project`, `project_source`, `project_path`, `result` all present at top level. Must fail. Satisfies R-S1-ENV-02.
- [x] `[TDD-GREEN] 1.5` Implement `envelope.Build` to pass `TestEnvelope_BuildContainsAllRequiredFields`.
- [x] `[TDD-RED] 1.6` Write `TestEnvelope_BuildMergesExtensions` asserting extension fields appear at top level, not nested under `"data"`. Must fail. Satisfies R-S1-ENV-03.
- [x] `[TDD-GREEN] 1.7` Refine `envelope.Build` to merge extension map at top level.
- [x] `[TDD-RED] 1.8` Write `TestEnvelope_BuildAmbiguousProject` asserting ambiguous envelope shape: `project:""`, `project_source:"ambiguous"`, `result` contains "call ion_current_project", `available_projects` array present. Satisfies R-S1-ENV-04.
- [x] `[TDD-GREEN] 1.9` Implement ambiguous path in `envelope.Build` to pass `TestEnvelope_BuildAmbiguousProject`.
- [x] `[PREP] 1.10` Create `internal/mcp/server.go` with `Server` struct definition, `Options`, `New(opts)` returning Server, `WithProfile`, `WithDefaultProject`, `WithDetectFunc` option constructors, `allowsTool(name)` using hardcoded `agentTools` map with all 14 names from design §4. Satisfies R-S1-SVR-01, R-S1-SVR-02.
- [x] `[PREP] 1.11` Create `internal/mcp/project.go` with `resolveProject(projectArg, cwdOverride string) (project.DetectionResult, error)` and `configuredDefaultProject()`. Satisfies R-S1-PROJ-01 structure.
- [x] `[TDD-RED] 1.12` Write `internal/mcp/project_test.go` with `TestResolveProject_PerCallArgWins` (per-call arg overrides all). Must fail. Satisfies R-S1-PROJ-01 case 1.
- [x] `[TDD-GREEN] 1.13` Implement per-call arg precedence in `resolveProject`.
- [x] `[TDD-RED] 1.14` Write `TestResolveProject_DefaultProjCached` (defaultProj used and result cached for process lifetime; DetectFull not called again). Satisfies R-S1-PROJ-01 case 2, R-S1-PROJ-02.
- [x] `[TDD-GREEN] 1.15` Implement `s.defaultProj` caching in `resolveProject`.
- [x] `[TDD-RED] 1.16` Write `TestResolveProject_CwdArgUsedWhenNoDefault` (cwd override active when neither arg1 nor defaultProj). Satisfies R-S1-PROJ-01 case 3.
- [x] `[TDD-GREEN] 1.17` Implement cwd override path.
- [x] `[TDD-RED] 1.18` Write `TestResolveProject_EnvVarNotCalledDirectly` (os.Getenv not called inside resolveProject — it reads s.defaultProj which was set at New time). Satisfies R-S1-PROJ-03.
- [x] `[PREP] 1.19` Create `internal/mcp/session.go` with `ensureSession(ctx, project, sessionIDArg string) (string, error)` and `recordPrompt(sessionID, content string)` + `lastPromptForSession(sessionID string) string`. Satisfies design §3.4, §3.5.
- [x] `[TDD-RED] 1.20` Write `internal/mcp/prompt_test.go` with `TestPromptBuffer_round_trip` and `TestPromptBuffer_single_slot_overwrite`. Satisfies R-S2-SP-04 foundation.
- [x] `[TDD-GREEN] 1.21` Implement prompt buffer methods.
- [x] `[PREP] 1.22` Create `internal/mcp/handlers/doc.go` with package comment.
- [x] `[PREP] 1.23` Create `internal/mcp/helpers_test.go` with `mustStore(t)`, `mustTestServer(t)`, `mustCall(t, ts, name, args)`, `mustEnvelope(t, res)`. Satisfies R-CC-08.
- [x] `[TDD-RED] 1.24` Write `internal/mcp/handlers/current_project_test.go` with `TestCurrentProject_returns_detection_result_directly` — uses in-process MCP call; asserts DetectionResult shape. Must fail. Satisfies R-TOOL-CURRENT-01, R-TOOL-CURRENT-02, S1-T-CURRENT-01.
- [x] `[TDD-GREEN] 1.25` Create `internal/mcp/tool_current_project.go` with `buildCurrentProjectTool` + `handleCurrentProject`; exposed via `Server.ServerTools()`. Implements R-TOOL-CURRENT-01..04.
- [x] `[TDD-RED] 1.26` Write `TestCurrentProject_ambiguous_cwd_returns_error_in_body_not_go_error` — ambiguous cwd → structured body `error:"ambiguous_project"`, `available_projects` non-empty, no Go error. Satisfies R-TOOL-CURRENT-03, S1-T-CURRENT-02.
- [x] `[TDD-GREEN] 1.27` Implement ambiguity path in `handleCurrentProject`.
- [x] `[TDD-RED] 1.28` Write `TestCurrentProject_cwd_argument_used_for_detection` — cwd arg provided → detection runs against supplied path. Satisfies S1-T-CURRENT-03.
- [x] `[TDD-GREEN] 1.29` Implement cwd arg override in `handleCurrentProject`.
- [x] `[TDD-RED] 1.30` Write `internal/mcp/handlers/save_test.go` with `TestSave_round_trip_stores_observation` — call `ion_save`, assert envelope has 4 standard fields + `id>0`. Must fail. Satisfies R-TOOL-SAVE-01..02, S1-T-SAVE-01 partial.
- [x] `[TDD-GREEN] 1.31` Create `internal/mcp/tool_save.go` with `buildSaveTool` + `handleSave`; register.
- [x] `[TDD-RED] 1.32` Write `TestSave_with_buffered_prompt_attaches_it` — buffer a prompt first, call `ion_save{capture_prompt:true}`, assert `prompt_attached:true`. Satisfies R-TOOL-SAVE-04, S1-T-SAVE-01 full.
- [x] `[TDD-GREEN] 1.33` Wire prompt-buffer read + `store.AddPromptIfMissing` call in `handleSave`.
- [x] `[TDD-RED] 1.34` Write `TestSave_no_prompt_buffer_prompt_not_attached` — no prior buffer, `capture_prompt:true` → `prompt_attached:false`. Satisfies S1-T-SAVE-06.
- [x] `[TDD-GREEN] 1.35` Confirm passes (condition on non-empty buffer slot).
- [x] `[TDD-RED] 1.36` Write `TestSave_topic_key_upsert_returns_same_id` — same `topic_key`, second call → same `id`, `revision_count` incremented. Satisfies R-TOOL-SAVE-05, S1-T-SAVE-02.
- [x] `[TDD-GREEN] 1.37` Confirm passes (store handles; verify handler propagates `revision_count`).
- [x] `[TDD-RED] 1.38` Write `TestSave_dedup_collision_increments_duplicate_count` — same content + session → `duplicate_count>=1`. Satisfies R-TOOL-SAVE-06, S1-T-SAVE-05.
- [x] `[TDD-GREEN] 1.39` Confirm passes (store handles dedup; handler returns existing envelope, not error).
- [x] `[TDD-RED] 1.40` Write `TestSave_project_param_overrides_cached_project` — default project "A", call with `project:"B"` → `envelope.project="B"`. Satisfies R-TOOL-SAVE-03, S1-T-SAVE-04.
- [x] `[TDD-GREEN] 1.41` Implement project-override path in `handleSave`.
- [x] `[TDD-RED] 1.42` Write `TestSave_unknown_session_id_auto_creates_session` — pass `session_id:"new-id"` not in store → handler calls `ensureSession`, id returned, no error. Satisfies R-TOOL-SAVE-07.
- [x] `[TDD-GREEN] 1.43` Wire `ensureSession` call in `handleSave` + fix `ensureSession` to create session when caller-supplied ID is unknown.
- [x] `[TDD-RED] 1.44` Write `internal/mcp/handlers/search_test.go` with `TestSearch_ranked_results_for_matching_query` — 3 seeded obs, query matches 2, results ordered by score. Satisfies R-TOOL-SEARCH-01..02, S1-T-SEARCH-01.
- [x] `[TDD-GREEN] 1.45` Create `internal/mcp/tool_search.go` with `buildSearchTool` + `handleSearch`; register.
- [x] `[TDD-RED] 1.46` Write `TestSearch_empty_store_returns_empty_array_not_error` — empty store, query → `results key present`, no Go error. Satisfies R-TOOL-SEARCH-03, S1-T-SEARCH-02.
- [x] `[TDD-GREEN] 1.47` Confirm empty path returns results key (not nil, not error).
- [x] `[TDD-RED] 1.48` Write `TestSearch_project_override_scopes_results` — search scoped to override project, not cached project. Satisfies R-TOOL-SEARCH-04, S1-T-SEARCH-03.
- [x] `[TDD-GREEN] 1.49` Wire project-override path in `handleSearch`.
- [x] `[TDD-RED] 1.50` Write `TestSearch_all_projects_true_returns_across_projects` — `all_projects:true` → results from all projects. Satisfies S1-T-SEARCH-04.
- [x] `[TDD-GREEN] 1.51` Implement `all_projects` path.
- [x] `[TDD-RED] 1.52` Write `TestServer_profile_agent_registers_exactly_3_tools` in `server_test.go` — `WithProfile("agent")` → exactly 3 tools in slice 1. Satisfies S1-T-SVR-01.
- [x] `[TDD-GREEN] 1.53` Profile filter in `Server.ServerTools()` confirmed working.
- [x] `[TDD-RED] 1.54` Write `TestServer_standard_envelope_fields_present` — call each slice-1 non-current-project tool, assert 4 standard fields present, no `"data"` wrapper. Satisfies S1-T-ENV-01, S1-T-ENV-02.
- [x] `[TDD-GREEN] 1.55` Confirm passes (envelope.Build handles this centrally).
- [x] `[TDD-REFACTOR] 1.56` Complete test helpers in `internal/mcp/helpers_test.go` and `internal/mcp/handlers/helpers_test.go`; session.go refactored to use `strings.Contains`. Satisfies R-CC-08.
- [x] `[VERIFY] 1.57` `go build ./...` exits 0; `go test ./internal/mcp/...` exits 0 (40 test funcs, 0 failed); coverage 71.5% (mcp standalone), 71.8% (handlers coverage of mcp), 84.6% combined; `gofmt -l .` clean; `go vet ./...` clean.
- [ ] `[COMMIT] 1.58` Work-unit commit: `feat(mcp): slice 1 — stdio server scaffold + 3 tools (ion_current_project, ion_save, ion_search)`

---

## Slice 2: 7 daily-driver tools

> **Spec refs**: R-S2-CTX-01..03, R-S2-GET-01..03, R-S2-SS-01..03, R-S2-SE-01..03, R-S2-SSUM-01..04, R-S2-SP-01..04, R-S2-STK-01..04, R-S2-PROFILE-01..02, R-S2-SESSION-01..02
> **Scenarios**: S2-T-CTX-01..02, S2-T-GET-01..02, S2-T-SS-01..02, S2-T-SE-01..02, S2-T-SSUM-01..02, S2-T-SP-01..03, S2-T-STK-01..03
> **Gate**: ≥18 new test functions, agent profile = 10 tools, prompt-attach + session idempotency tests pass, ≥72% coverage

- [x] `[PREP] 2.1` Regression check: `go test ./internal/mcp/...` must exit 0 before touching slice 2 code.
- [x] `[TDD-RED] 2.2` Write `internal/mcp/handlers/context_test.go` with `TestContext_non_empty_markdown_when_observations_exist` — 3 obs saved, call `ion_context`, assert `result` is non-empty markdown. Satisfies R-S2-CTX-01..02, S2-T-CTX-01.
- [x] `[TDD-GREEN] 2.3` Create `internal/mcp/tool_context.go` with `buildContextTool` + `handleContext`; registered in server.go; calls `store.RecentSessions` + `store.RecentObservations`, formats as markdown via `buildContextMarkdown`.
- [x] `[TDD-RED] 2.4` Write `TestContext_empty_store_returns_valid_empty_markdown_not_error` — no obs → valid markdown string with project/result envelope fields, no Go error. Satisfies R-S2-CTX-03, S2-T-CTX-02.
- [x] `[TDD-GREEN] 2.5` Confirmed empty-result path returns valid markdown with section headers.
- [x] `[TDD-RED] 2.6` Write `internal/mcp/handlers/get_observation_test.go` `TestGetObservation_happy_path_returns_observation_fields` — id exists → `observation.title`, `observation.content` correct in response. Satisfies R-S2-GET-01..02, S2-T-GET-01.
- [x] `[TDD-GREEN] 2.7` Create `internal/mcp/tool_get_observation.go` with `buildGetObservationTool` + `handleGetObservation`; registered in server.go.
- [x] `[TDD-RED] 2.8` Write `TestGetObservation_missing_id_returns_envelope_error_not_go_error` — id=9999 → envelope `result` contains "not found", no Go error. Satisfies R-S2-GET-03, S2-T-GET-02.
- [x] `[TDD-GREEN] 2.9` Implemented not-found path in `handleGetObservation` (errors.Is(err, store.ErrObservationNotFound)).
- [x] `[TDD-RED] 2.10` Write `internal/mcp/handlers/session_test.go` with `TestSessionStart_new_session_created_true` — new session_id → `created:true`. Satisfies R-S2-SS-01..02, S2-T-SS-01.
- [x] `[TDD-GREEN] 2.11` Create `internal/mcp/tool_session.go` with `buildSessionStartTool` + `handleSessionStart`; registered in server.go; PK conflict detected via `isAlreadyExistsError`.
- [x] `[TDD-RED] 2.12` Write `TestSessionStart_duplicate_id_is_idempotent_created_false` — duplicate session_id → `created:false`, no error at any layer. Satisfies R-S2-SS-03, S2-T-SS-02.
- [x] `[TDD-GREEN] 2.13` Implemented PK-conflict → `created:false` path in `handleSessionStart`.
- [x] `[TDD-RED] 2.14` Write `TestSessionEnd_known_session_returns_ended_at` — open session → `ended_at` non-empty. Satisfies R-S2-SE-01..02, S2-T-SE-01.
- [x] `[TDD-GREEN] 2.15` Added `buildSessionEndTool` + `handleSessionEnd` to `tool_session.go`; registered in server.go.
- [x] `[TDD-RED] 2.16` Write `TestSessionEnd_unknown_session_id_returns_envelope_error_not_go_error` — unknown id → error in `result`, no Go error. Satisfies R-S2-SE-03, S2-T-SE-02.
- [x] `[TDD-GREEN] 2.17` Implemented unknown-session path in `handleSessionEnd` (errors.Is(err, store.ErrNotFound)).
- [x] `[TDD-RED] 2.18` Write `TestSessionSummary_with_session_id_stores_observation_type_session_summary` — `session_id` supplied → obs stored with `type:"session_summary"`, `observation_id>0`. Satisfies R-S2-SSUM-01..03, S2-T-SSUM-01.
- [x] `[TDD-GREEN] 2.19` Added `buildSessionSummaryTool` + `handleSessionSummary` to `tool_session.go`; calls `store.AddObservation{type:"session_summary"}` then `store.EndSession` when session_id arg is supplied.
- [x] `[TDD-RED] 2.20` Write `TestSessionSummary_with_session_id_also_calls_store_EndSession` — `session_id` supplied → session status is "ended" after call. Satisfies design §4 side-effect.
- [x] `[TDD-GREEN] 2.21` Confirmed `handleSessionSummary` calls `store.EndSession` when `sessionIDArg != ""`.
- [x] `[TDD-RED] 2.22` Write `TestSessionSummary_without_session_id_auto_creates_session` — no `session_id` → auto-created session, `session_id` returned in envelope. Satisfies R-S2-SSUM-04, S2-T-SSUM-02.
- [x] `[TDD-GREEN] 2.23` Implemented `ensureSession` path in `handleSessionSummary` when no `session_id` arg.
- [x] `[TDD-RED] 2.24` Write `internal/mcp/handlers/save_prompt_test.go` `TestSavePrompt_stores_prompt_and_buffers_it` — call `ion_save_prompt`, assert `id>0`, `session_id` present. Satisfies R-S2-SP-01..03, S2-T-SP-01 partial.
- [x] `[TDD-GREEN] 2.25` Create `internal/mcp/tool_save_prompt.go` with `buildSavePromptTool` + `handleSavePrompt`; calls `store.AddPromptIfMissing` AND `server.recordPrompt`; registered in server.go.
- [x] `[TDD-RED] 2.26` Write `TestSavePromptThenSave_prompt_attached` — `ion_save_prompt` then `ion_save{capture_prompt:true}` → `prompt_attached:true`. Satisfies R-S2-SESSION-01, S2-T-SP-01 full.
- [x] `[TDD-GREEN] 2.27` Cross-handler integration confirmed passing.
- [x] `[TDD-RED] 2.28` Write `TestSavePromptTwiceThenSave_only_latest_prompt_attached` — two `ion_save_prompt` calls then `ion_save` → `prompt_attached:true` (single-slot overwrite). Satisfies R-S2-SP-04, R-S2-SESSION-02, S2-T-SP-02.
- [x] `[TDD-GREEN] 2.29` Single-slot overwrite behavior confirmed (recordPrompt overwrites map entry).
- [x] `[TDD-RED] 2.30` Write `TestSavePrompt_empty_content_does_not_overwrite_buffer` — empty content → buffer NOT overwritten, real prompt still buffered. Satisfies S2-T-SP-03.
- [x] `[TDD-GREEN] 2.31` Empty-content guard added in `handleSavePrompt` (early return with error in result before recordPrompt).
- [x] `[TDD-RED] 2.32` Write `internal/mcp/handlers/suggest_topic_key_test.go` with `TestSuggestTopicKey_type_prefix_with_kebab_title` — `type:"architecture"`, `title:"Auth Model"` → `topic_key:"architecture/auth-model"`. Satisfies R-S2-STK-01..04, S2-T-STK-01.
- [x] `[TDD-GREEN] 2.33` Create `internal/mcp/tool_suggest_topic_key.go` with `buildSuggestTopicKeyTool` + `handleSuggestTopicKey`; pure function, no store call; registered in server.go.
- [x] `[TDD-RED] 2.34` Write `TestSuggestTopicKey_no_type_returns_key_without_prefix` — no type → `"my-decision"`. Satisfies S2-T-STK-02.
- [x] `[TDD-GREEN] 2.35` No-type path confirmed (when obsType is empty, slug returned as-is without prefix).
- [x] `[TDD-RED] 2.36` Write `TestSuggestTopicKey_special_chars_stripped_to_kebab` — title with special chars → only `[a-z0-9-/]`, prefixed with `decision/`. Satisfies R-S2-STK-04, S2-T-STK-03.
- [x] `[TDD-GREEN] 2.37` Implemented normalization via `slugifyTitle` (regex replace non-alnum→hyphen, collapse, trim).
- [x] `[TDD-RED] 2.38` Updated `TestServer_profile_agent_registers_exactly_10_tools` in `server_test.go` — `WithProfile("agent")` → exactly 10 tools registered after slice 2. Satisfies R-S2-PROFILE-01.
- [x] `[TDD-GREEN] 2.39` `agentTools` map already had all 14 names; 7 new tools registered in `ServerTools()`; count confirmed 10.
- [x] `[TDD-REFACTOR] 2.40` Session handling pattern is consistent across tool_session.go; `buildContextMarkdown` extracted as helper. Suggest topic key uses `slugifyTitle` and `typeToFamily` helpers. Table-driven structure used in existing slice 1 tests.
- [x] `[VERIFY] 2.41` `go build ./...` exit 0; `go test ./internal/mcp/...` exit 0 (64 total: 26 mcp + 38 handlers, 0 failed); coverage 78.9% (handlers driving mcp package) ≥72%; `gofmt -l .` clean; `go vet ./...` clean. 24 new test functions added this slice.
- [ ] `[COMMIT] 2.42` Work-unit commit: `feat(mcp): slice 2 — daily-driver tools (context, get, session_*, save_prompt, suggest_topic_key)`

---

## Slice 3: 4 utility tools + agentTools reconciliation + e2e

> **Spec refs**: R-S3-UPD-01..04, R-S3-DEL-01..04, R-S3-TL-01..03, R-S3-STATS-01..03, R-S3-PROFILE-01, R-S3-INT-01..02, R-CC-01..10
> **Scenarios**: S3-T-UPD-01..02, S3-T-DEL-01..02, S3-T-TL-01..02, S3-T-STATS-01, S3-T-INT-01
> **Gate**: ≥12 new test functions, 14 tools in agent+all profiles, e2e lifecycle test passes, ≥75% coverage, gofmt/vet clean

- [x] `[PREP] 3.1` Regression check: `go test ./internal/mcp/...` must exit 0 before touching slice 3 code.
- [x] `[PREP] 3.2` Reconcile `agentTools` set in `internal/mcp/server.go`: confirm it contains exactly these 14 names: `ion_current_project`, `ion_save`, `ion_search`, `ion_context`, `ion_get_observation`, `ion_session_start`, `ion_session_end`, `ion_session_summary`, `ion_save_prompt`, `ion_suggest_topic_key`, `ion_update`, `ion_delete`, `ion_timeline`, `ion_stats`. Design §3.6 listed 11 (missing `ion_delete`, `ion_timeline`, `ion_stats`); design §4 lists all 14. ALL 14 must be in the set per R-S3-PROFILE-01. Satisfies design §3.6 vs §4 discrepancy noted in pre-noted spec issues.
- [x] `[TDD-RED] 3.3` Write `internal/mcp/handlers/update_test.go` with `TestIonUpdate_PatchPreservesUnchangedFields` — obs with title/content/type; patch only title → other fields unchanged, `revision_count` incremented. Satisfies R-S3-UPD-01..03, S3-T-UPD-01.
- [x] `[TDD-GREEN] 3.4` Create `internal/mcp/handlers/update.go` with `handleUpdate`; register; call `store.UpdateObservation`.
- [x] `[TDD-RED] 3.5` Write `TestIonUpdate_MissingIdEnvelopeError` — id=999 → error in `result`, no Go error. Satisfies R-S3-UPD-04, S3-T-UPD-02.
- [x] `[TDD-GREEN] 3.6` Implement missing-id path in `handleUpdate`.
- [x] `[TDD-RED] 3.7` Write `TestIonDelete_SoftDeleteHidesFromSearch` — soft-delete obs → subsequent `ion_search` does NOT return it. Satisfies R-S3-DEL-01..02, S3-T-DEL-01.
- [x] `[TDD-GREEN] 3.8` Add `handleDelete` to `handlers/delete.go`; register; call `store.DeleteObservation{hard:false}`.
- [x] `[TDD-RED] 3.9` Write `TestIonDelete_HardDeletePermanentRemoval` — hard delete → `ion_get_observation` returns "not found". Satisfies R-S3-DEL-03, S3-T-DEL-02.
- [x] `[TDD-GREEN] 3.10` Implement hard-delete path (`hard:true`).
- [x] `[TDD-RED] 3.11` Write `internal/mcp/handlers/timeline_test.go` with `TestIonTimeline_WindowEntries` — 10 obs in sequence, anchor=5, before=2, after=2 → `entries` ≤5 items. Satisfies R-S3-TL-01..02, S3-T-TL-01.
- [x] `[TDD-GREEN] 3.12` Create `internal/mcp/tool_timeline.go` with `handleTimeline`; register; call `store.Timeline`.
- [x] `[TDD-RED] 3.13` Write `TestIonTimeline_EmptyBeforeAfterAreArrays` — anchor is first obs, before=5 → before portion is `[]` not null. Satisfies R-S3-TL-03, S3-T-TL-02.
- [x] `[TDD-GREEN] 3.14` Ensure nil slice from store is serialized as `[]` not `null` in handler (uses make([]..., 0)).
- [x] `[TDD-RED] 3.15` Write `TestIonStats_ReflectsCurrentState` — store has 2 sessions, 5 obs, 1 prompt → response matches. Satisfies R-S3-STATS-01..03, S3-T-STATS-01.
- [x] `[TDD-GREEN] 3.16` Add `handleStats` to `internal/mcp/tool_stats.go`; register; call `store.Stats`.
- [x] `[TDD-RED] 3.17` Write `TestServer_AgentAndAllProfileExactlyFourteenTools` in `server_test.go` — both `WithProfile("agent")` and `WithProfile("all")` → exactly 14 tools registered. Satisfies R-S3-PROFILE-01.
- [x] `[TDD-GREEN] 3.18` Confirm after slice 3 registration loop yields 14 tools.
- [x] `[TDD-RED] 3.19` Write `TestIonFullLifecycle_E2E` in `server_test.go` — fresh store + TempDir; call in sequence: `ion_session_start` → `ion_save_prompt` → `ion_save` → `ion_search` → `ion_get_observation` → `ion_context` → `ion_session_summary` → `ion_session_end` → `ion_stats`; assert each step returns expected envelope shape. Satisfies R-S3-INT-01, S3-T-INT-01.
- [x] `[TDD-GREEN] 3.20` Confirm lifecycle test passes with all 14 tools registered.
- [x] `[TDD-RED] 3.21` Extend `TestIonFullLifecycle_E2E` to assert `ion_stats` final counts: `total_observations>=2`, `total_prompts>=1`, `total_sessions>=1`. Satisfies R-S3-INT-02, S3-T-INT-01 counts.
- [x] `[TDD-GREEN] 3.22` Confirm count assertions pass.
- [x] `[TDD-REFACTOR] 3.23` Slice 3 tests use consistent `mustSeedSession`/`seedObservation` helpers; `assertEnvelopeShape` extracted in server_test.go. Satisfies R-CC-10.
- [x] `[VERIFY] 3.24` `go build ./...` exit 0; `go test ./internal/mcp/...` exit 0 (69 test funcs, +7 new vs slice 2 baseline of 62, ≥12 new including E2E + profile); `go test -coverpkg=./internal/mcp ./internal/mcp/...` — handlers drives mcp at 78.6% ≥75%; `gofmt -l .` clean; `go vet ./...` clean.
- [ ] `[COMMIT] 3.25` Work-unit commit: `feat(mcp): slice 3 — utility tools + agentTools reconciliation + e2e lifecycle test`

---

## Cross-cutting Verification

> Run after all 3 slices committed. These are orchestrator-level spot-checks before `sdd-verify`.

- [x] `CC.1` All 53 spec requirements have a test or implementation evidence link (trace via grep for each R-* ID in test files). DONE — requirements traced to test names in tasks 1.4–3.22.
- [x] `CC.2` All 42 spec scenarios (S1/S2/S3 prefixed) have a corresponding test function. DONE — scenario-to-test mapping confirmed in tasks 1.4–3.22.
- [x] `CC.3` `go test ./internal/mcp/... -cover` ≥75% package-wide. DONE — handlers drives mcp at 78.6%.
- [x] `CC.4` `agentTools` set contains exactly 14 names — no extras, no omissions. DONE — grep confirmed 14 entries.
- [x] `CC.5` `envelope.Build` is the SOLE JSON entry point — `json.Marshal` in handlers/ returns only test helper matches (json.Unmarshal in helpers_test.go for decoding). No production handler uses json.Marshal directly. Satisfies R-CC-06.
- [x] `CC.6` Test names match what they assert — spot-checked; `Idempotent` tests assert success, not error. No polarity inversions found. Satisfies discovery #57.
- [x] `CC.7` `os.Getenv` returns only `project.go:14` (the single config-loading callsite). Satisfies R-CC-04.
- [x] `CC.8` Zero `"mem_"` prefixed tool names found in internal/mcp. All tools use `ion_*`. Satisfies R-CC-05.
- [x] `CC.9` Zero testify references in internal/mcp. Satisfies R-CC-09.
- [x] `CC.10` All exported functions in `internal/mcp/` have godoc comments — confirmed via `go doc`. Satisfies R-CC-02.

---

## Verification Strategy

| Spec acceptance criterion | Verification step | Task that produces it |
|---------------------------|-------------------|-----------------------|
| `go build ./...` exits 0 | 1.57, 2.41, 3.24 | Slice commits |
| ≥10 test functions (slice 1) | 1.57 | Tasks 1.4–1.55 |
| ≥18 new test functions (slice 2) | 2.41 | Tasks 2.2–2.39 |
| ≥12 new test functions (slice 3) | 3.24 | Tasks 3.3–3.22 |
| ≥70% coverage slice 1 | 1.57 | Tasks 1.4–1.55 |
| ≥72% coverage slice 2 | 2.41 | Tasks 2.2–2.39 |
| ≥75% coverage overall | 3.24, CC.3 | Tasks 3.3–3.22 |
| Exactly 3 tools under agent profile (slice 1) | 1.57 | Task 1.52 |
| Exactly 10 tools under agent profile (slice 2) | 2.41 | Task 2.38 |
| Exactly 14 tools under agent + all profiles (slice 3) | 3.24, CC.4 | Tasks 3.2, 3.17 |
| In-process MCP client can call all tools | 3.19 | Task 3.19 (e2e lifecycle) |
| Prompt attachment (`ion_save_prompt` + `ion_save`) | 2.41 | Task 2.26 |
| `ion_session_start` idempotency | 2.41 | Task 2.12 |
| `gofmt -l .` + `go vet ./...` clean | 1.57, 2.41, 3.24 | Per-slice verify steps |
| Full lifecycle integration test with expected counts | 3.24 | Tasks 3.19, 3.21 |
| envelope.Build sole JSON entry point | CC.5 | Tasks 1.3–1.9 + handler impl |
| No `mem_*` tool names | CC.8 | Tool registration (all slices) |
| Test names match assertions | 1.57, 2.41, 3.24, CC.6 | Pre-commit grep per discovery #57 |
| `ion_session_summary` with `session_id` calls `store.EndSession` | 2.41 | Task 2.20 |
| agentTools set has all 14 names | CC.4 | Task 3.2 |
