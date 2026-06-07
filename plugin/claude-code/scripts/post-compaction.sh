#!/bin/bash
# ion-mem PATH guard: GUI-launched Claude Code does not load ~/.zshrc.
# Cover the common per-user Go install dir + system Homebrew locations.
export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
# ion-mem — Post-compaction hook for Claude Code
#
# When compaction happens: ensure session exists, fetch context, and instruct
# the agent to persist the compacted summary via ion_session_summary.

# Load shared helpers
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/_helpers.sh"

# Read hook input from stdin
INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
PROJECT=$(detect_project "$CWD")

# Ensure session exists (best-effort; idempotent)
if [ -n "$SESSION_ID" ] && [ -n "$PROJECT" ]; then
  ion-mem session-start \
    --id="$SESSION_ID" \
    --project="$PROJECT" \
    --cwd="$CWD" \
    >/dev/null 2>&1 || true
fi

# Fetch context (best-effort)
CONTEXT=""
if [ -n "$PROJECT" ]; then
  CONTEXT=$(ion-mem context --project="$PROJECT" 2>/dev/null || true)
fi

# Inject Memory Protocol + post-compaction instructions + context
cat <<'PROTOCOL'
## ion-mem Persistent Memory — ACTIVE PROTOCOL

You have ion-mem memory tools. This protocol is MANDATORY and ALWAYS ACTIVE.

### CORE TOOLS — always available, no ToolSearch needed
ion_save, ion_search, ion_context, ion_session_summary, ion_get_observation, ion_save_prompt

Use ToolSearch for other tools: ion_update, ion_suggest_topic_key, ion_session_start, ion_session_end, ion_stats, ion_delete, ion_timeline

### PROACTIVE SAVE — do NOT wait for user to ask
Call `ion_save` IMMEDIATELY after ANY of these:
- Decision made (architecture, convention, workflow, tool choice)
- Bug fixed (include root cause)
- Convention or workflow documented/updated
- Notion/Jira/GitHub artifact created or updated with significant content
- Non-obvious discovery, gotcha, or edge case found
- Pattern established (naming, structure, approach)
- User preference or constraint learned
- Feature implemented with non-obvious approach

**Self-check after EVERY task**: "Did I just make a decision, fix a bug, learn something, or establish a convention? If yes → ion_save NOW."

### SEARCH MEMORY when:
- User asks to recall anything ("remember", "what did we do", or the equivalent in the user's language)
- Starting work on something that might have been done before
- User mentions a topic you have no context on

### SESSION CLOSE — before saying "done":
Call `ion_session_summary` with: Goal, Discoveries, Accomplished, Next Steps, Relevant Files.

---

CRITICAL INSTRUCTION POST-COMPACTION — follow these steps IN ORDER:
PROTOCOL

printf "\n1. FIRST: Call ion_session_summary with the content of the compacted summary above. Use project: '%s'.\n" "$PROJECT"
printf "   This preserves what was accomplished before compaction.\n\n"
printf "2. THEN: Call ion_context with project: '%s' to recover recent session history and observations.\n" "$PROJECT"
printf "   Read the returned context carefully — it tells you what was being worked on.\n\n"
cat <<'PROTOCOL'
3. If you need more detail on a specific topic, call ion_search with relevant keywords.

4. Only THEN continue working on what the user asked.

All 4 steps are MANDATORY. Without them, you lose context and start blind.
PROTOCOL

# Inject memory context if available
if [ -n "$CONTEXT" ]; then
  printf "\n%s\n" "$CONTEXT"
fi

exit 0
