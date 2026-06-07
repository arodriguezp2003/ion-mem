#!/bin/bash
# ion-mem — SessionStart hook for Claude Code
#
# 1. Creates (or ensures) a session in the ion-mem store via CLI
# 2. Fetches memory context for the project
# 3. Injects Memory Protocol instructions + context as additionalContext

# Load shared helpers
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/_helpers.sh"

# Read hook input from stdin
INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
PROJECT=$(detect_project "$CWD")

# Create session (best-effort; idempotent — already-exists is not an error)
if [ -n "$SESSION_ID" ] && [ -n "$PROJECT" ]; then
  ion-mem session-start \
    --id="$SESSION_ID" \
    --project="$PROJECT" \
    --cwd="$CWD" \
    >/dev/null 2>&1 || true
fi

# Fetch memory context (best-effort)
CONTEXT=""
if [ -n "$PROJECT" ]; then
  CONTEXT=$(ion-mem context --project="$PROJECT" 2>/dev/null || true)
fi

# Inject Memory Protocol + context — stdout becomes additionalContext in Claude
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
- Non-obvious discovery, gotcha, or edge case found
- Pattern established (naming, structure, approach)
- User preference or constraint learned
- Feature implemented with non-obvious approach
- User confirms your recommendation ("go with that", "sounds good", or the equivalent in the user's language)
- User rejects an approach or expresses a preference

**Self-check after EVERY task**: "Did I or the user just make a decision, confirm a recommendation, fix a bug, learn something, or establish a convention? If yes → ion_save NOW."

### SEARCH MEMORY when:
- User asks to recall anything ("remember", "what did we do", or the equivalent in the user's language)
- Starting work on something that might have been done before
- User mentions a topic you have no context on
- User's FIRST message references the project, a feature, or a problem — call `ion_search` with keywords before responding

### SESSION CLOSE — before saying "done":
Call `ion_session_summary` with: Goal, Discoveries, Accomplished, Next Steps, Relevant Files.
PROTOCOL

# Inject memory context if available
if [ -n "$CONTEXT" ]; then
  printf "\n%s\n" "$CONTEXT"
fi

exit 0
