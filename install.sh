#!/bin/bash
# ion-mem — installer
#
# Installs the ion-mem binary via Homebrew and registers the Claude Code
# plugin, idempotently. Safe to re-run: every step detects existing state and
# refreshes instead of duplicating.
#
# This repo publishes only prebuilt releases and the plugin — there is no
# `go install` fallback. If Homebrew is not available, install a prebuilt
# archive from the GitHub Releases page instead (see step 1 below).
#
# Steps:
#   1. Install the binary via Homebrew (brew tap arodriguezp2003/tap &&
#      brew install ion-mem). If 'brew' is not on PATH, print the Releases
#      URL and exit 1.
#   2. Register the GitHub marketplace: claude plugin marketplace add
#      arodriguezp2003/ion-mem
#   3. Install (or update) the `ion-mem` plugin from that marketplace.
#   4. Symlink the binary into a system-PATH directory so GUI-launched Claude
#      Code can spawn it (skip with --skip-path-edit).
# After running, restart Claude Code (or /reload-plugins) to pick everything up.
#
# Usage:
#   ./install.sh                  install everything
#   ./install.sh --skip-path-edit install but skip the system-PATH symlink step
#   ./install.sh --help           print this message
#   ./install.sh --uninstall      remove plugin + marketplace + binary + symlink

set -euo pipefail

MARKETPLACE_REPO="arodriguezp2003/ion-mem"
RELEASES_URL="https://github.com/arodriguezp2003/ion-mem/releases"
BREW_TAP="arodriguezp2003/tap"
BREW_FORMULA="ion-mem"

SKIP_PATH_EDIT=0

# Legacy symlink path used by earlier (broken) versions of this script.
# Cleaned up on every install/uninstall so re-runs don't carry stale state.
LEGACY_PLUGIN_SYMLINK="$HOME/.claude/plugins/ion-mem"

# ─── helpers ────────────────────────────────────────────────────────────────

have() { command -v "$1" >/dev/null 2>&1; }

claude_available() { have claude; }

# True (exit 0) when the ion-mem marketplace is already registered with claude.
marketplace_registered() {
  claude plugin marketplace list 2>/dev/null | grep -q 'ion-mem'
}

# True (exit 0) when the ion-mem plugin is already installed.
plugin_installed() {
  claude plugin list 2>/dev/null | grep -qE 'ion-mem@ion-mem'
}

brew_formula_installed() {
  brew list --formula 2>/dev/null | grep -qE '^ion-mem$'
}

print_help() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
}

# ─── uninstall ──────────────────────────────────────────────────────────────

uninstall() {
  if claude_available; then
    if plugin_installed; then
      echo "[ion-mem install] Uninstalling plugin via claude plugin uninstall"
      claude plugin uninstall ion-mem@ion-mem 2>&1 || true
    fi
    if marketplace_registered; then
      echo "[ion-mem install] Removing marketplace via claude plugin marketplace remove"
      claude plugin marketplace remove ion-mem 2>&1 || true
    fi
  else
    echo "[ion-mem install] ⚠ 'claude' CLI not found — skipping plugin/marketplace removal"
  fi

  if have brew && brew_formula_installed; then
    echo "[ion-mem install] Uninstalling Homebrew formula"
    brew uninstall "$BREW_FORMULA" 2>&1 || true
  fi

  # Remove the system-PATH symlinks created during install.
  for sys_dir in /opt/homebrew/bin /usr/local/bin; do
    if [ -L "$sys_dir/ion-mem" ]; then
      echo "[ion-mem install] Removing system-PATH symlink at $sys_dir/ion-mem"
      rm -f "$sys_dir/ion-mem"
    fi
  done

  if [ -L "$LEGACY_PLUGIN_SYMLINK" ] || [ -e "$LEGACY_PLUGIN_SYMLINK" ]; then
    echo "[ion-mem install] Removing legacy plugin symlink at $LEGACY_PLUGIN_SYMLINK"
    rm -rf "$LEGACY_PLUGIN_SYMLINK"
  fi

  echo "✓ ion-mem uninstalled"
  echo "  Your memory store at ~/.ion-mem was left untouched."
}

# ─── arg parsing ────────────────────────────────────────────────────────────

while [ $# -gt 0 ]; do
  case "${1:-}" in
    -h|--help)         print_help; exit 0 ;;
    --uninstall)       uninstall; exit 0 ;;
    --skip-path-edit)  SKIP_PATH_EDIT=1; shift ;;
    "")                shift ;;
    *)                 echo "ion-mem install: unknown option: $1" >&2; print_help; exit 2 ;;
  esac
done

# ─── preflight ──────────────────────────────────────────────────────────────

if ! claude_available; then
  echo "ion-mem install: 'claude' CLI not found on PATH. Install Claude Code first." >&2
  exit 1
fi

if ! have brew; then
  echo "ion-mem install: 'brew' (Homebrew) not found on PATH." >&2
  echo "  Install Homebrew (https://brew.sh) and re-run, or download a prebuilt" >&2
  echo "  archive for your OS/arch from: $RELEASES_URL" >&2
  exit 1
fi

# ─── 1. Install the binary via Homebrew ─────────────────────────────────────

echo "[ion-mem install] Installing via Homebrew..."
if brew tap 2>/dev/null | grep -qE "^${BREW_TAP}$"; then
  echo "[ion-mem install] Tap '$BREW_TAP' already registered"
else
  brew tap "$BREW_TAP"
fi

if brew_formula_installed; then
  echo "[ion-mem install] Formula already installed — upgrading if a newer version exists"
  brew upgrade "$BREW_FORMULA" 2>&1 | sed 's/^/  /' || true
else
  brew install "$BREW_FORMULA"
fi

if ! have ion-mem; then
  echo "ion-mem install: Homebrew install finished but 'ion-mem' is still not on PATH." >&2
  echo "  Check 'brew --prefix'/bin is on your PATH and re-run." >&2
  exit 1
fi
BIN_DIR="$(dirname "$(command -v ion-mem)")"
echo "[ion-mem install] Binary installed: $BIN_DIR/ion-mem"

# ─── 2. Clean up legacy symlink (from broken pre-marketplace install.sh) ────

if [ -L "$LEGACY_PLUGIN_SYMLINK" ] || [ -e "$LEGACY_PLUGIN_SYMLINK" ]; then
  echo "[ion-mem install] Removing stale legacy symlink at $LEGACY_PLUGIN_SYMLINK"
  rm -rf "$LEGACY_PLUGIN_SYMLINK"
fi

# ─── 3. Register marketplace + install plugin ──────────────────────────────

if marketplace_registered; then
  echo "[ion-mem install] Marketplace 'ion-mem' already registered — refreshing"
  claude plugin marketplace update ion-mem 2>&1 | sed 's/^/  /' || true
else
  echo "[ion-mem install] Registering marketplace from $MARKETPLACE_REPO"
  claude plugin marketplace add "$MARKETPLACE_REPO" 2>&1 | sed 's/^/  /'
fi

if plugin_installed; then
  echo "[ion-mem install] Plugin 'ion-mem' already installed — refreshing"
  claude plugin update ion-mem@ion-mem 2>&1 | sed 's/^/  /' || true
else
  echo "[ion-mem install] Installing plugin 'ion-mem@ion-mem'"
  claude plugin install ion-mem@ion-mem 2>&1 | sed 's/^/  /'
fi

# ─── 4. Make ion-mem findable on GUI-launched Claude Code's PATH ───────────
# Claude Code launched from Spotlight/Finder/Dock inherits a minimal system
# PATH that may not include Homebrew's bin directory. The plugin's MCP config
# says `command: "ion-mem"` (bare, not absolute) so Claude Code's spawn fails
# with `Executable not found in $PATH` unless the binary is reachable from
# that minimal PATH.
#
# Fix: symlink the binary into a system-PATH location that GUI apps inherit.
# On Apple Silicon Homebrew that's /opt/homebrew/bin; on Intel Homebrew it's
# /usr/local/bin. A standard Homebrew install already lands in one of those,
# making this a no-op. Skip entirely with --skip-path-edit.

if [ "$SKIP_PATH_EDIT" -eq 1 ]; then
  echo "[ion-mem install] --skip-path-edit set; not touching system-PATH symlinks."
  echo "  If Claude Code can't find 'ion-mem' when launched from the Dock/Spotlight,"
  echo "  symlink it manually: ln -sfn $BIN_DIR/ion-mem /opt/homebrew/bin/ion-mem"
else
  SYMLINK_NOTE=""
  PATH_ALREADY_OK=0
  echo ":$PATH:" | grep -q ":$BIN_DIR:" && PATH_ALREADY_OK=1

  if [ "$PATH_ALREADY_OK" -eq 1 ] && { [ "$BIN_DIR" = "/opt/homebrew/bin" ] || [ "$BIN_DIR" = "/usr/local/bin" ]; }; then
    SYMLINK_NOTE="already in a system-PATH directory"
  else
    SYSTEM_BIN_DIRS=("/opt/homebrew/bin" "/usr/local/bin")
    for sys_dir in "${SYSTEM_BIN_DIRS[@]}"; do
      [ "$sys_dir" = "$BIN_DIR" ] && { SYMLINK_NOTE="already in a system-PATH directory"; break; }
      if [ -d "$sys_dir" ] && [ -w "$sys_dir" ]; then
        target="$sys_dir/ion-mem"
        if [ -L "$target" ] && [ "$(readlink "$target")" = "$BIN_DIR/ion-mem" ]; then
          echo "[ion-mem install] System-PATH symlink already in place: $target"
        elif [ -e "$target" ] && [ ! -L "$target" ]; then
          echo "[ion-mem install] ⚠ $target exists and is not a symlink — leaving it alone"
        else
          ln -sfn "$BIN_DIR/ion-mem" "$target" \
            && echo "[ion-mem install] Created symlink: $target → $BIN_DIR/ion-mem"
        fi
        SYMLINK_NOTE="$target"
        break
      fi
    done
  fi

  if [ -z "$SYMLINK_NOTE" ]; then
    echo "[ion-mem install] ⚠ Neither /opt/homebrew/bin nor /usr/local/bin is writable."
    echo "  GUI-launched Claude Code will not find 'ion-mem' on its inherited PATH."
    echo "  Fixes:"
    echo "    1. sudo ln -sfn $BIN_DIR/ion-mem /usr/local/bin/ion-mem"
    echo "    2. Or launch Claude Code from a terminal that has $BIN_DIR on PATH."
  fi
fi

# ─── Verification banner ────────────────────────────────────────────────────

if [ -x "$BIN_DIR/ion-mem" ]; then
  VERSION_OUT="$("$BIN_DIR/ion-mem" version 2>/dev/null || true)"
else
  VERSION_OUT=""
fi

echo
echo "✓ ion-mem installed"
echo "  Binary:      $BIN_DIR/ion-mem"
echo "  Version:     ${VERSION_OUT:-<unable to read>}"
echo "  Marketplace: ion-mem (from $MARKETPLACE_REPO)"
echo
echo "Next: restart Claude Code (or run /reload-plugins in your session) to load ion-mem."
echo "Uninstall: ./install.sh --uninstall"
