#!/usr/bin/env bash
# Claude Code UserPromptSubmit hook. Hook failures are intentionally non-fatal.
set -uo pipefail

BEACON_ROOT="${BEACON_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
input=$(cat 2>/dev/null || true)
prompt=$(jq -r '.prompt // .user_prompt // .input // ""' <<<"$input" 2>/dev/null || true)
cwd=$(jq -r '.cwd // ""' <<<"$input" 2>/dev/null || true)
"$BEACON_ROOT/bin/beacon" report working "$prompt" "$cwd" >/dev/null 2>&1 || true
exit 0
