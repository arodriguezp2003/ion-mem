#!/bin/bash
# ion-mem PATH guard: GUI-launched Claude Code does not load ~/.zshrc.
# Cover the common per-user Go install dir + system Homebrew locations.
export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
# ion-mem — UserPromptSubmit hook for Claude Code
#
# On the FIRST message of a session: injects a ToolSearch instruction to force
# Claude Code to load all ion-mem memory tools (which are deferred by default).
#
# On subsequent messages: saves the prompt to the store in the background
# (best-effort, non-blocking) and outputs {} so Claude is not slowed down.
#
# MUST exit 0 always and output valid JSON — otherwise Claude Code blocks the message.
#
# Deferred: 15-minute save nudge logic (needs CLI subcommand for last-save-time).
# Deferred: Windows .ps1 fallback.

# Load shared helpers
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/_helpers.sh"

# Read hook input from stdin
INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
PROMPT=$(echo "$INPUT" | jq -r '.prompt // empty')

# Build state file path keyed by session ID
if [ -n "$SESSION_ID" ]; then
  STATE_FILE="/tmp/ion-mem-claude-${SESSION_ID}-tools-loaded"
else
  STATE_FILE="/tmp/ion-mem-claude-$$-tools-loaded"
fi

if [ ! -f "$STATE_FILE" ]; then
  # ── FIRST MESSAGE ───────────────────────────────────────────────────────────
  # Touch state file immediately to prevent repeat injections.
  touch "$STATE_FILE" 2>/dev/null || true

  printf '%s\n' '{"systemMessage":"CRITICAL FIRST ACTION — Execute this ToolSearch NOW before responding:\nselect:mcp__ion-mem__ion_save,mcp__ion-mem__ion_search,mcp__ion-mem__ion_context,mcp__ion-mem__ion_session_summary,mcp__ion-mem__ion_session_start,mcp__ion-mem__ion_session_end,mcp__ion-mem__ion_get_observation,mcp__ion-mem__ion_suggest_topic_key,mcp__ion-mem__ion_save_prompt,mcp__ion-mem__ion_update,mcp__ion-mem__ion_current_project,mcp__ion-mem__ion_timeline,mcp__ion-mem__ion_stats,mcp__ion-mem__ion_delete\n\nAfter loading tools, call ion_context to check for prior session history before responding."}'
  exit 0
fi

# ── SUBSEQUENT MESSAGES ──────────────────────────────────────────────────────
# Save prompt in background (best-effort — never block the user's message).
if [ -n "$SESSION_ID" ] && [ -n "$PROMPT" ]; then
  PROJECT=$(detect_project "$CWD")
  (
    ion-mem save-prompt \
      --session-id="$SESSION_ID" \
      --content="$PROMPT" \
      --project="$PROJECT" \
      >/dev/null 2>&1
  ) &
fi

printf '%s\n' '{}'
exit 0
