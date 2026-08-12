# Beacon

Beacon is a local-first attention and status tool for agent-driven terminal
work. It observes the current machine only: agent lifecycle hooks write a
small bounded JSON snapshot, tmux renders it, notifications surface events,
and `beacon jump` returns to the most recently completed pane.

Beacon does not schedule tasks, own tmux sessions, or require Pantheon. A Mac
and an Omarchy host run independent Beacon instances. Pantheon may become an
optional provider later, but local operation must never depend on a network.

## Capabilities

- Claude Code prompt/stop/notification hooks
- explicit status reporting for any agent via `beacon report`
- concurrent-safe, atomic local state under
  `${XDG_DATA_HOME:-~/.local/share}/beacon/panes.json`
- bounded tmux status-right output with local memory usage
- jump to the last completed live pane
- macOS and Linux desktop notifications
- no daemon, database, network, or model calls

## Install

Requirements: Bash, jq, and tmux.

```bash
./install.sh
beacon doctor
```

Claude Code hooks:

```json
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "beacon hook prompt"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "beacon hook stop"}]}],
    "Notification": [{"hooks": [{"type": "command", "command": "beacon hook notification"}]}]
  }
}
```

Codex can use the same prompt/stop hooks and the permission hook in
`~/.codex/hooks.json`; each Beacon entry may coexist with other integrations:

```json
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "beacon hook prompt"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "beacon hook stop"}]}],
    "PermissionRequest": [{"hooks": [{"type": "command", "command": "beacon hook permission"}]}]
  }
}
```

tmux:

```tmux
set -g status-right '#(beacon status-tmux "#{client_width}" "#{status-bg}" "#{session_name}" "#{window_index}" "#{pane_id}" "#{window_id}")'
bind-key P run-shell 'beacon jump'
```

Any agent or wrapper can report a state without knowing about Pantheon:

```bash
beacon report working 'running tests'
beacon report waiting 'needs user input'
beacon report blocked 'dependency unavailable'
beacon report completed 'tests passed'
```

## Boundaries

- Runtime state is local and disposable; Git stores configuration, not state.
- tmux is a presentation/execution surface, not a task-state authority.
- Beacon does not replace Pantheon orchestration or Mnemos memory.
- Host telemetry stays deliberately small and cheap. Use a monitoring system
  for historical or fleet-level metrics.
