#!/bin/bash
# ion-mem PATH guard: GUI-launched Claude Code does not load ~/.zshrc.
# Cover the common per-user Go install dir + system Homebrew locations.
export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
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
