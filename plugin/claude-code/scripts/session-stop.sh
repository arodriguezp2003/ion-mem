#!/bin/bash
# ion-mem — Stop hook for Claude Code (async)
#
# Marks the session as ended via the ion-mem CLI.
# Runs async so it does not block Claude's response.

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')

if [ -z "$SESSION_ID" ]; then
  exit 0
fi

ion-mem session-end --id="$SESSION_ID" >/dev/null 2>&1 || true

exit 0
