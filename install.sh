#!/bin/bash
# ion-mem — installer
#
# Installs the ion-mem binary and registers the Claude Code plugin, idempotently.
# Safe to re-run: every step detects existing state and refreshes instead of
# duplicating.
#
# Steps:
#   1. Install the binary — Homebrew first (brew tap arodriguezp2003/tap &&
#      brew install ion-mem), falling back to
#      `go install github.com/arodriguezp2003/ion-mem/cmd/ion-mem@latest`.
#   2. When installed via Go, append a PATH stanza to the shell rc so the binary
#      is findable in future shells (skip with --skip-path-edit).
#   3. Register the GitHub marketplace: claude plugin marketplace add
#      arodriguezp2003/ion-mem
#   4. Install (or update) the `ion-mem` plugin from that marketplace.
#   5. Symlink the binary into a system-PATH directory so GUI-launched Claude
#      Code can spawn it.
# After running, restart Claude Code (or /reload-plugins) to pick everything up.
#
# Env overrides:
#   GOBIN / GOPATH            standard Go install destination resolution
#   ION_MEM_INSTALL_METHOD    force "brew" or "go" instead of auto-detecting
#
# Usage:
#   ./install.sh                  install everything
#   ./install.sh --skip-path-edit install but do NOT touch shell config
#   ./install.sh --help           print this message
#   ./install.sh --uninstall      remove plugin + marketplace + binary + PATH

set -euo pipefail

MARKETPLACE_REPO="arodriguezp2003/ion-mem"
GO_MODULE="github.com/arodriguezp2003/ion-mem/cmd/ion-mem@latest"
BREW_TAP="arodriguezp2003/tap"
BREW_FORMULA="ion-mem"

SKIP_PATH_EDIT=0
FORCED_METHOD="${ION_MEM_INSTALL_METHOD:-}"
INSTALL_METHOD=""

# Marker block used to identify and later remove the PATH stanza we inject.
PATH_MARKER_BEGIN="# >>> ion-mem install.sh >>>"
PATH_MARKER_END="# <<< ion-mem install.sh <<<"

# Legacy symlink path used by earlier (broken) versions of this script.
# Cleaned up on every install/uninstall so re-runs don't carry stale state.
LEGACY_PLUGIN_SYMLINK="$HOME/.claude/plugins/ion-mem"

# ─── helpers ────────────────────────────────────────────────────────────────

have() { command -v "$1" >/dev/null 2>&1; }

resolve_bin_dir() {
  local gobin gopath
  gobin="$(go env GOBIN 2>/dev/null || true)"
  if [ -n "$gobin" ]; then echo "$gobin"; return; fi
  gopath="$(go env GOPATH 2>/dev/null || true)"
  if [ -n "$gopath" ]; then echo "${gopath%%:*}/bin"; return; fi
  echo "$HOME/go/bin"
}

detect_shell_rc() {
  local shell_name
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh)
      echo "$HOME/.zshrc"
      ;;
    bash)
      local f
      for f in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
        if [ -f "$f" ]; then echo "$f"; return; fi
      done
      echo "$HOME/.bashrc"
      ;;
    *)
      echo ""
      ;;
  esac
}

add_path_to_shell_rc() {
  local rc="$1" bin_dir="$2"
  if [ -z "$rc" ]; then return 1; fi
  if [ -f "$rc" ] && grep -qF "$PATH_MARKER_BEGIN" "$rc" 2>/dev/null; then
    echo "ℹ PATH stanza for ion-mem already present in $rc"
    return 0
  fi
  {
    echo ""
    echo "$PATH_MARKER_BEGIN"
    echo "export PATH=\"$bin_dir:\$PATH\""
    echo "$PATH_MARKER_END"
  } >> "$rc"
  echo "✓ Added $bin_dir to PATH in $rc"
}

remove_path_from_shell_rc() {
  local rc
  rc="$(detect_shell_rc)"
  if [ -z "$rc" ] || [ ! -f "$rc" ]; then return 0; fi
  if ! grep -qF "$PATH_MARKER_BEGIN" "$rc"; then return 0; fi
  local tmp
  tmp="$(mktemp)"
  awk -v b="$PATH_MARKER_BEGIN" -v e="$PATH_MARKER_END" '
    $0 == b { skip=1; next }
    skip == 1 && $0 == e { skip=0; next }
    skip == 0 { print }
  ' "$rc" > "$tmp" && mv "$tmp" "$rc"
  echo "[ion-mem install] Removed PATH stanza from $rc"
}

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

  if have go; then
    local bin_dir
    bin_dir="$(resolve_bin_dir)"
    if [ -f "$bin_dir/ion-mem" ]; then
      echo "[ion-mem install] Removing binary at $bin_dir/ion-mem"
      rm -f "$bin_dir/ion-mem"
    fi
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

  remove_path_from_shell_rc
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

case "$FORCED_METHOD" in
  brew)
    have brew || { echo "ion-mem install: ION_MEM_INSTALL_METHOD=brew but 'brew' is not on PATH." >&2; exit 1; }
    INSTALL_METHOD="brew"
    ;;
  go)
    have go || { echo "ion-mem install: ION_MEM_INSTALL_METHOD=go but 'go' is not on PATH." >&2; exit 1; }
    INSTALL_METHOD="go"
    ;;
  "")
    if have brew; then
      INSTALL_METHOD="brew"
    elif have go; then
      INSTALL_METHOD="go"
    else
      echo "ion-mem install: neither 'brew' nor 'go' found on PATH." >&2
      echo "  Install Homebrew (https://brew.sh) or Go 1.25+ and re-run." >&2
      exit 1
    fi
    ;;
  *)
    echo "ion-mem install: ION_MEM_INSTALL_METHOD must be 'brew' or 'go' (got: $FORCED_METHOD)" >&2
    exit 2
    ;;
esac

# ─── 1. Install the binary ──────────────────────────────────────────────────

BIN_DIR=""

if [ "$INSTALL_METHOD" = "brew" ]; then
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
    echo "  Check 'brew --prefix'/bin is on your PATH, or re-run with ION_MEM_INSTALL_METHOD=go." >&2
    exit 1
  fi
  BIN_DIR="$(dirname "$(command -v ion-mem)")"
  echo "[ion-mem install] Binary installed: $BIN_DIR/ion-mem"
else
  echo "[ion-mem install] Installing via 'go install $GO_MODULE'..."
  go install "$GO_MODULE"
  BIN_DIR="$(resolve_bin_dir)"
  if [ ! -x "$BIN_DIR/ion-mem" ]; then
    echo "ion-mem install: expected binary at $BIN_DIR/ion-mem but it is not there." >&2
    exit 1
  fi
  echo "[ion-mem install] Binary installed: $BIN_DIR/ion-mem"
fi

# ─── 2. PATH stanza (only needed for the Go install path) ───────────────────

SHELL_RC=""
PATH_ALREADY_OK=0
echo ":$PATH:" | grep -q ":$BIN_DIR:" && PATH_ALREADY_OK=1

if [ "$INSTALL_METHOD" = "go" ]; then
  if [ "$SKIP_PATH_EDIT" -eq 1 ]; then
    if [ "$PATH_ALREADY_OK" -eq 0 ]; then
      echo "[ion-mem install] --skip-path-edit set; not modifying shell config."
      echo "  Add manually: export PATH=\"$BIN_DIR:\$PATH\""
    fi
  elif [ "$PATH_ALREADY_OK" -eq 1 ]; then
    echo "[ion-mem install] $BIN_DIR already on PATH — no shell config change needed"
  else
    SHELL_RC="$(detect_shell_rc)"
    if [ -n "$SHELL_RC" ]; then
      add_path_to_shell_rc "$SHELL_RC" "$BIN_DIR"
    else
      echo "⚠ Could not detect shell config (\$SHELL=${SHELL:-unset}). Add manually:"
      echo "    export PATH=\"$BIN_DIR:\$PATH\""
    fi
  fi
fi

# ─── 3. Clean up legacy symlink (from broken pre-marketplace install.sh) ────

if [ -L "$LEGACY_PLUGIN_SYMLINK" ] || [ -e "$LEGACY_PLUGIN_SYMLINK" ]; then
  echo "[ion-mem install] Removing stale legacy symlink at $LEGACY_PLUGIN_SYMLINK"
  rm -rf "$LEGACY_PLUGIN_SYMLINK"
fi

# ─── 4. Register marketplace + install plugin ──────────────────────────────

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

# ─── 5. Make ion-mem findable on GUI-launched Claude Code's PATH ───────────
# Claude Code launched from Spotlight/Finder/Dock inherits a minimal system
# PATH that does NOT include $HOME/go/bin. The plugin's MCP config says
# `command: "ion-mem"` (bare, not absolute) so Claude Code's spawn fails with
# `Executable not found in $PATH`.
#
# Fix: symlink the binary into a system-PATH location that GUI apps inherit.
# On Apple Silicon Homebrew that's /opt/homebrew/bin; on Intel Homebrew it's
# /usr/local/bin. Both are user-writable for Homebrew users. A Homebrew
# install already lands in one of those, so this is a no-op there.

SYMLINK_NOTE=""
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

# ─── Verification banner ────────────────────────────────────────────────────

if [ -x "$BIN_DIR/ion-mem" ]; then
  VERSION_OUT="$("$BIN_DIR/ion-mem" version 2>/dev/null || true)"
else
  VERSION_OUT=""
fi

echo
echo "✓ ion-mem installed"
echo "  Method:      $INSTALL_METHOD"
echo "  Binary:      $BIN_DIR/ion-mem"
echo "  Version:     ${VERSION_OUT:-<unable to read>}"
echo "  Marketplace: ion-mem (from $MARKETPLACE_REPO)"
echo

if [ "$INSTALL_METHOD" = "go" ] && [ "$PATH_ALREADY_OK" -eq 0 ] && [ "$SKIP_PATH_EDIT" -eq 0 ] && [ -n "$SHELL_RC" ]; then
  echo "Reload PATH in this shell: source \"$SHELL_RC\""
  echo "(New shells will pick it up automatically.)"
  echo
fi

echo "Next: restart Claude Code (or run /reload-plugins in your session) to load ion-mem."
echo "Uninstall: ./install.sh --uninstall"
