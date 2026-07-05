#!/bin/bash
# ion-mem PATH guard: GUI-launched Claude Code does not load ~/.zshrc.
# Cover the common per-user Go install dir + system Homebrew locations.
export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
# ion-mem — UserPromptSubmit hook for Claude Code
#
# On the FIRST message of a session: injects a ToolSearch instruction so
# Claude Code loads the ion-mem MCP tools (which are deferred by default),
# AND saves the prompt to the store in the background — every prompt is
# captured so the session record is complete from message 1.
#
# On subsequent messages: saves the prompt to the store in the background
# (best-effort, non-blocking) and outputs {} so Claude is not slowed down.
#
# MUST exit 0 always and output valid JSON — otherwise Claude Code blocks
# the message.
#
# Deferred: 15-minute save nudge logic (needs CLI subcommand for
# last-save-time). Deferred: Windows .ps1 fallback.

# Load shared helpers
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/_helpers.sh"

# Read hook input from stdin
INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
PROMPT=$(echo "$INPUT" | jq -r '.prompt // empty')

# save_prompt_bg fires `ion-mem save-prompt` in the background, fully
# detached so the hook returns immediately. No-op when SESSION_ID, PROMPT,
# or PROJECT is empty (avoids FK errors / silent inserts of garbage).
save_prompt_bg() {
  local project
  project=$(detect_project "$CWD")
  if [ -z "$SESSION_ID" ] || [ -z "$PROMPT" ] || [ -z "$project" ]; then
    return 0
  fi
  (
    ion-mem save-prompt \
      --session-id="$SESSION_ID" \
      --content="$PROMPT" \
      --project="$project" \
      >/dev/null 2>&1
  ) &
}

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

  # Capture the first prompt too — the session record starts at message 1.
  save_prompt_bg

  printf '%s\n' '{"systemMessage":"CRITICAL FIRST ACTION — Execute this ToolSearch NOW before responding:\nselect:mcp__plugin_ion-mem_ion-mem__ion_save,mcp__plugin_ion-mem_ion-mem__ion_search,mcp__plugin_ion-mem_ion-mem__ion_context,mcp__plugin_ion-mem_ion-mem__ion_session_summary,mcp__plugin_ion-mem_ion-mem__ion_session_start,mcp__plugin_ion-mem_ion-mem__ion_session_end,mcp__plugin_ion-mem_ion-mem__ion_get_observation,mcp__plugin_ion-mem_ion-mem__ion_suggest_topic_key,mcp__plugin_ion-mem_ion-mem__ion_save_prompt,mcp__plugin_ion-mem_ion-mem__ion_update,mcp__plugin_ion-mem_ion-mem__ion_current_project,mcp__plugin_ion-mem_ion-mem__ion_timeline,mcp__plugin_ion-mem_ion-mem__ion_stats,mcp__plugin_ion-mem_ion-mem__ion_delete,mcp__plugin_ion-mem_ion-mem__ion_history,mcp__plugin_ion-mem_ion-mem__ion_undelete\n\nAfter loading tools, call ion_context to check for prior session history before responding."}'
  exit 0
fi

# ── SUBSEQUENT MESSAGES ──────────────────────────────────────────────────────
save_prompt_bg

printf '%s\n' '{}'
exit 0
