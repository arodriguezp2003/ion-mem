#!/bin/bash
# ion-mem PATH guard: GUI-launched Claude Code does not load ~/.zshrc.
# Cover the common per-user Go install dir + system Homebrew locations.
export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
# ion-mem — Shared helpers for Claude Code hooks
# WARNING: Do not read from stdin here — scripts source this before reading their hook input.

# detect_project detects the project name from a given directory.
# Priority: git remote origin repo name > git root basename > cwd basename
# Result is lowercased.
detect_project() {
  local dir="$1"

  # Try git remote origin URL
  local url
  url=$(git -C "$dir" remote get-url origin 2>/dev/null)
  if [ -n "$url" ]; then
    # Handles both SSH (git@github.com:user/repo.git) and HTTPS (https://github.com/user/repo.git)
    local name
    name=$(echo "$url" | sed 's/\.git$//' | sed 's|.*[/:]||' | tr '[:upper:]' '[:lower:]')
    if [ -n "$name" ]; then
      echo "$name"
      return
    fi
  fi

  # Fallback: git root directory name (works in worktrees)
  local root
  root=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)
  if [ -n "$root" ]; then
    basename "$root" | tr '[:upper:]' '[:lower:]'
    return
  fi

  # Final fallback: cwd basename
  basename "$dir" | tr '[:upper:]' '[:lower:]'
}

# search_health_block runs `ion-mem doctor --json --autostart --timeout 5s`
# and, when the verdict is "degraded" or "down", prints a short plain-text
# warning block suitable for appending to a SessionStart/compact hook's
# additionalContext stdout. Prints nothing on verdict "ok", on a
# missing/erroring/slow `doctor` binary, or on malformed JSON — callers must
# never fail the hook over this.
#
# No external `timeout` command is used (stock macOS doesn't ship one):
# `--timeout 5s` bounds the whole run internally via doctor's own root
# context (see cli_doctor.go's doctorBudget), well under a 10s hook budget.
search_health_block() {
  local doctor_json verdict hint reason

  # Explicit PATH check (rather than just letting the call below fail) so
  # the reason shows up in Claude Code's hook debug output instead of just
  # silently producing no health block.
  if ! command -v ion-mem >/dev/null 2>&1; then
    echo "ion-mem: doctor skipped — ion-mem binary not found on PATH" >&2
    return 0
  fi

  # `ion-mem doctor` exits non-zero BY DESIGN on verdict degraded/down (1/2)
  # — that is not a failure to swallow, it is the signal we're reading. Do
  # NOT `|| return 0` on this assignment: a real "doctor is missing/broken"
  # failure naturally yields empty stdout, which the next check already
  # handles — this also covers an older `ion-mem` binary that predates
  # --timeout: flag parsing fails before anything is written to stdout, so
  # doctor_json is empty and we fall through to "no health block" exactly
  # the same way.
  doctor_json=$(ion-mem doctor --json --autostart --timeout 5s 2>/dev/null)
  [ -n "$doctor_json" ] || return 0

  verdict=$(echo "$doctor_json" | jq -r '.verdict // empty' 2>/dev/null) || return 0
  case "$verdict" in
    degraded) reason="search will fall back to lexical" ;;
    down) reason="the ion-mem store is unreadable" ;;
    *) return 0 ;; # "ok", empty, or unrecognized — nothing to report
  esac

  hint=$(echo "$doctor_json" | jq -r '.hints[0] // empty' 2>/dev/null)

  if [ -n "$hint" ]; then
    printf 'ion-mem search health: %s — %s. Fix: %s' "$verdict" "$reason" "$hint"
  else
    printf 'ion-mem search health: %s — %s.' "$verdict" "$reason"
  fi
}
