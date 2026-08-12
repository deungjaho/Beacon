#!/usr/bin/env bash
# tmux status-right: bounded agent attention plus fast local host status.
set -euo pipefail

BEACON_ROOT="${BEACON_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
STATE="$BEACON_ROOT/lib/state.sh"
STATE_FILE="${BEACON_STATE_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/beacon}/panes.json"

width="${1:-100}"
status_bg="${2:-black}"
pane_id="${5:-}"
window_id="${6:-}"
[[ -z "$status_bg" || "$status_bg" == "default" ]] && status_bg="black"
if [[ "$width" =~ ^[0-9]+$ ]] && (( width < ${BEACON_MIN_WIDTH:-80} )); then
  exit 0
fi

state='{"panes":{}}'
if [[ -f "$STATE_FILE" ]]; then
  state=$(cat "$STATE_FILE" 2>/dev/null || printf '{"panes":{}}')
fi

segments=$(jq -r --arg wid "$window_id" '
  .panes // {} | to_entries
  | map(select(.value.window == $wid and $wid != ""))
  | sort_by(.key) | .[] | .value.status as $st
  | if $st == "working" then "#1d1f21|#F0DFAF| ● \(.value.summary[:30]) "
    elif $st == "completed" then "#1d1f21|#7F9F7F| ✓ \(.value.summary[:30]) "
    elif $st == "waiting" then "#1d1f21|#CC9393| ⚠ \(.value.summary[:30]) "
    elif $st == "blocked" then "#ffffff|#CC3333| ✗ \(.value.summary[:30]) "
    else empty end
' <<<"$state" 2>/dev/null || true)

# Existing sessions may predate Beacon hook installation. If no hook state is
# available for the active pane, use tmux's current command as a cheap,
# read-only indication that an agent is running. Hooks remain authoritative
# whenever a pane has an explicit Beacon record.
if [[ -z "$segments" && -n "$pane_id" ]]; then
  has_record=$(jq -r --arg pane "$pane_id" '.panes[$pane] != null' <<<"$state" 2>/dev/null || printf false)
  if [[ "$has_record" != "true" ]]; then
    pane_command=$("${BEACON_TMUX_BIN:-tmux}" display-message -p -t "$pane_id" '#{pane_current_command}' 2>/dev/null || true)
    case "${pane_command##*/}" in
      codex|claude|devin|opencode|op|agy|gemini)
        segments="#1d1f21|#F0DFAF| ● ${pane_command##*/} working "
        ;;
    esac
  fi
fi

if [[ "${BEACON_SHOW_SYSTEM:-1}" == "1" ]]; then
  system_segments=$("$BEACON_ROOT/lib/system.sh" 2>/dev/null || true)
  if [[ -n "$system_segments" ]]; then
    segments="${segments}${segments:+$'\n'}${system_segments}"
  fi
fi

[[ -n "$segments" ]] || exit 0
prev_bg="$status_bg"
output=""
while IFS='|' read -r fg bg text; do
  [[ -n "$fg" && -n "$bg" && -n "$text" ]] || continue
  output+="#[fg=${bg},bg=${prev_bg}]#[fg=${fg},bg=${bg}]${text}"
  prev_bg="$bg"
done <<<"$segments"

[[ -n "$output" ]] || exit 0
printf '%s#[fg=%s,bg=%s]█' "$output" "$prev_bg" "$status_bg"
