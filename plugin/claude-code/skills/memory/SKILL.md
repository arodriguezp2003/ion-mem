---
name: ion-mem-memory
description: "ALWAYS ACTIVE — Persistent memory protocol. You MUST save decisions, conventions, bugs, and discoveries to ion-mem proactively. Do NOT wait for the user to ask."
---

# ion-mem Persistent Memory — Protocol

You have access to ion-mem, a persistent memory system that survives across sessions and compactions.
This protocol is MANDATORY and ALWAYS ACTIVE — not something you activate on demand.

## AVAILABLE TOOLS

Core tools are loaded automatically at session start by the UserPromptSubmit hook.
They are available immediately — no manual ToolSearch needed.

- `ion_save`, `ion_search`, `ion_context`, `ion_session_summary`
- `ion_get_observation`, `ion_suggest_topic_key`, `ion_update`
- `ion_session_start`, `ion_session_end`, `ion_save_prompt`

**Fallback**: If tools are unexpectedly unavailable, run `ion-mem setup claude-code`
again and restart Claude Code.
<!-- NOTE: `ion-mem setup claude-code` does not exist yet — it is a placeholder
     for a future change that will implement the setup subcommand. -->

Admin tools (deferred — use ToolSearch only if needed):
- `ion_stats`, `ion_delete`, `ion_timeline`
<!-- NOTE: ion_capture_passive is not yet implemented in ion-mem; it is deferred
     to a future change. Do not use it. -->

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
All core tools are loaded automatically by the hook at session start. If they are unexpectedly missing, rerun `ion-mem setup claude-code` and restart Claude Code.
