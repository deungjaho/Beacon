#!/usr/bin/env bash
# Codex PermissionRequest hook. Hook failures are intentionally non-fatal.
set -uo pipefail

BEACON_ROOT="${BEACON_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
input=$(cat 2>/dev/null || true)
tool=$(jq -r '.tool_name // .tool // "operation"' <<<"$input" 2>/dev/null || true)
cwd=$(jq -r '.cwd // ""' <<<"$input" 2>/dev/null || true)
message="Permission required: $tool"
"$BEACON_ROOT/bin/beacon" report waiting "$message" "$cwd" >/dev/null 2>&1 || true
"$BEACON_ROOT/lib/notify.sh" "${BEACON_AGENT_NAME:-Codex}" "$message" || true
exit 0
