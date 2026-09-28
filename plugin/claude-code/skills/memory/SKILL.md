---
name: ion-mem-memory
description: "ALWAYS ACTIVE — Persistent memory protocol. You MUST save decisions, conventions, bugs, and discoveries to ion-mem proactively. Do NOT wait for the user to ask."
---

# ion-mem Persistent Memory — Protocol

You have access to ion-mem, a persistent memory system that survives across sessions and compactions.
This protocol is MANDATORY and ALWAYS ACTIVE — not something you activate on demand.

## AVAILABLE TOOLS

All 19 `ion_*` tools are loaded automatically at session start by the
UserPromptSubmit hook. They are available immediately — no manual ToolSearch
needed.

**Save & update**:
- `ion_save`, `ion_update`, `ion_delete`, `ion_undelete`, `ion_suggest_topic_key`,
  `ion_set_status`

**Status & curation**:
- Memory hygiene means changing state, not deleting. A superseded decision is
  still history worth keeping ("we chose X, then moved to Y because Z").
- Use `ion_set_status` to mark an observation `superseded` (pointing at the
  observation that replaces it via `superseded_by`) or `obsolete`. Search still
  finds superseded/obsolete rows — they rank lower and are labeled — nothing
  disappears silently.
- `bugfix` and `discovery` observations are permanent: they can never be marked
  `obsolete`, and can only be marked `superseded` when `superseded_by` also
  points at a `bugfix` or `discovery` observation (a newer fix replacing an
  older one).
- If you save a new decision under a DIFFERENT `topic_key` than an old one it
  replaces, follow up with `ion_set_status` on the old observation — a
  same-key `ion_save` upsert already handles supersession on its own.
- For a whole-project sweep rather than a single follow-up, see
  CURATING MEMORY below.

**Recovering soft-deleted observations**:
- `ion_delete` with `hard: false` (the default) is a soft delete — recoverable.
- Use `ion_undelete` with the observation `id` to restore it and make it searchable again.
- Hard-deleted observations (hard: true) cannot be recovered.

**Search & retrieve**:
- `ion_search`, `ion_context`, `ion_get_observation`, `ion_timeline`

**Session lifecycle**:
- `ion_session_start`, `ion_session_end`, `ion_session_summary`, `ion_save_prompt`

**Utilities**:
- `ion_current_project`, `ion_stats`

**Sharing memory between teammates**:
- `ion_export_project` writes one project's memory to a portable
  `.ionmem.zip` file; `ion_import_project` merges one into the local store.
- Both are dry-run/read-only by default: export never deletes anything and
  excludes prompts unless `with_prompts: true`; import only reports what it
  WOULD do until `apply: true`, and even then never overwrites a local row
  unless `prefer_bundle: true` — a backup is taken automatically before any
  write. Export also refuses to write when it finds secret-shaped content,
  unless `allow_secrets: true`.
- Prefer the guided skills over calling either tool ad hoc — see SHARING
  MEMORY BETWEEN TEAMMATES below.

**History**:
- `ion_history` — call with `id` (required) and optional `limit` to retrieve an
  observation's current summary plus its prior revisions newest-first. Use this
  when an observation's content looks wrong or stale, or when you need what a
  topic said before an overwrite (topic upserts keep the last 10 revisions).

**Fallback**: If tools are unexpectedly unavailable, reinstall via `make install`
from the ion-memory repo root, or verify the binary is on PATH with
`which ion-mem`, then restart Claude Code.

## BROWNFIELD PROJECTS — SEEDING MEMORY

If memory is empty or nearly empty on a mature codebase, agents will keep re-deriving
decisions that were settled long ago. Point the user to `/ion-mem:init` (the
`ion-mem-init` skill): it analyses the repo — stack, docs, ADRs, plans, git history, CI —
proposes a reviewed table of candidate observations with evidence, and seeds them only
after explicit confirmation. Everything it writes is namespaced `seed/<family>/<slug>`,
so it can never overwrite a hand-written observation; on a project that already has
memory it offers COMPLEMENT (default) or a backed-up, recoverable REBUILD that removes
only its own previously seeded rows. Suggest it once when you notice empty
memory on an established project — do not run it automatically.

## CURATING MEMORY

Memory drifts: decisions get replaced, conventions change, cited files stop
existing. When you notice that drift — stale decisions surfacing in search,
observations pointing at paths that no longer exist, the same fact stored twice
under different topic keys — point the user to `/ion-mem:curate` (the
`ion-mem-curate` skill). It backs up the store, enumerates active observations
per type, runs read-only subagents to detect dead evidence, contradictions,
duplicates and permanent-fix lineage, prints a proposal table with evidence, and
changes nothing until the user accepts. It only ever calls `ion_set_status`: no
deletes, no content edits, and `bugfix`/`discovery` rows can only be marked
`superseded` by another permanent row. Every change is undone with
`ion_set_status(id, status: "active")`. Suggest it when you see the drift — do
not run it automatically, and never do an ad-hoc bulk sweep of `ion_set_status`
calls in its place.

## SHARING MEMORY BETWEEN TEAMMATES

When someone wants to hand this project's memory to a teammate on the same repo
— or has been handed a `.ionmem.zip` and wants it merged — point them at
`/ion-mem:export` (the `ion-mem-export` skill) and `/ion-mem:import` (the
`ion-mem-import` skill) instead of calling `ion_export_project` /
`ion_import_project` yourself. Export confirms the working tree and resolved
project, asks before including prompts or soft-deleted rows, runs the secret
scan without `allow_secrets` and stops with the findings when it trips, then
reminds the user the file is private data and checks it is gitignored. Import
always dry-runs first, prints inserted / skipped / updated / conflicts /
unresolved supersedes / status downgrades, stops loudly when the bundle's
project differs from the resolved one, and only writes after the user picks
apply, apply with `prefer_bundle`, or cancel. Suggest them when the topic comes
up — do not run either automatically, and never call the tools with
`allow_secrets: true` or `apply: true` on your own initiative.

## READING RESULT SIGNALS

Interpret these fields returned by search and save operations:

- `ion_search` → `fuzzy: true`: the OR/lexical fallback fired — results are weaker
  matches. Consider using more specific or different search terms.
- `ion_search` → `hybrid: true`: vector+BM25 RRF fusion ran (requires embeddings
  enabled and Ollama reachable). Results combine semantic and lexical relevance.
  Spanish↔English cross-language recall works when embeddings use bge-m3.
- `ion_save` → `embedded: false`: the observation was saved but did not receive a
  vector embedding (embeddings are off or Ollama was unreachable). The memory is
  still keyword-searchable; run EMBED MISSING in the TUI config view later to
  backfill vectors.
- `ion_search` → `all_projects: true`: pass this flag to search across all projects
  and personal scope, not just the current project.

## PROACTIVE SAVE TRIGGERS (mandatory — do NOT wait for user to ask)

Call `ion_save` IMMEDIATELY and WITHOUT BEING ASKED after any of these:

### After decisions or conventions
- Architecture or design decision made
- Team convention documented or established
- Workflow change agreed upon
- Tool or library choice made with tradeoffs

### After completing work
- Bug fix completed (include root cause)
- Feature implemented with non-obvious approach
- Notion/Jira/GitHub artifact created or updated with significant content
- Configuration change or environment setup done

### After discoveries
- Non-obvious discovery about the codebase
- Gotcha, edge case, or unexpected behavior found
- Pattern established (naming, structure, convention)
- User preference or constraint learned

### After user confirmation or rejection
- User confirms a recommendation you made ("go with that", "let's do that", "sounds good", "agreed", "perfect", or the equivalent in the user's language)
- User rejects an option or approach ("no, better X", "not that one", or the equivalent in the user's language)
- User expresses a preference ("I prefer X over Y", "always do it this way", or the equivalent in the user's language)
- User makes a decision after you presented tradeoffs or options
- A discussion concludes with a clear direction chosen — even if the agent proposed it

### Self-check — ask yourself after EVERY task:
> "Did I or the user just make a decision, confirm a recommendation, express a preference, fix a bug, learn something non-obvious, or establish a convention? If yes, call ion_save NOW."

Format for `ion_save`:
- **title**: Verb + what — short, searchable (e.g. "Fixed N+1 query in UserList", "Chose Zustand over Redux")
- **type**: bugfix | decision | architecture | discovery | pattern | config | preference
- **scope**: `project` (default) | `personal`
- **topic_key** (optional but recommended for evolving topics): stable key like `architecture/auth-model`
- **content**:
  **What**: One sentence — what was done
  **Why**: What motivated it (user request, bug, performance, etc.)
  **Where**: Files or paths affected
  **Learned**: Gotchas, edge cases, things that surprised you (omit if none)

### Topic update rules (mandatory)

- Different topics MUST NOT overwrite each other (example: architecture decision vs bugfix)
- If the same topic evolves, call `ion_save` with the same `topic_key` so memory is updated (upsert) instead of creating a new observation
- If unsure about the key, call `ion_suggest_topic_key` first, then reuse that key consistently
- If you already know the exact ID to fix, use `ion_update`

## WHEN TO SEARCH MEMORY

When the user asks to recall something — any variation of "remember", "recall", "what did we do",
"how did we solve", or the equivalent in the user's language, or references to past work:
1. First call `ion_context` — checks recent session history (fast, cheap)
2. If not found, call `ion_search` with relevant keywords (FTS5 full-text search)
3. If you find a match, use `ion_get_observation` for full untruncated content

Also search memory PROACTIVELY when:
- Starting work on something that might have been done before
- The user mentions a topic you have no context on — check if past sessions covered it
- The user's FIRST message references the project, a feature, or a problem — call `ion_search` with keywords from their message to check for prior work before responding

## SESSION CLOSE PROTOCOL (mandatory)

Before ending a session or saying "done" / "that's it", you MUST:
1. Call `ion_session_summary` with this structure:

## Goal
[What we were working on this session]

## Instructions
[User preferences or constraints discovered — skip if none]

## Discoveries
- [Technical findings, gotchas, non-obvious learnings]

## Accomplished
- [Completed items with key details]

## Next Steps
- [What remains to be done — for the next session]

## Relevant Files
- path/to/file — [what it does or what changed]

This is NOT optional. If you skip this, the next session starts blind.

## AFTER COMPACTION

If you see a message about compaction or context reset:
1. IMMEDIATELY call `ion_session_summary` with the compacted summary content — this persists what was done before compaction
2. Then call `ion_context` to recover any additional context from previous sessions
3. Only THEN continue working

Do not skip step 1. Without it, everything done before compaction is lost from memory.
