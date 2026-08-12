# Beacon

Beacon is a local-first attention and status tool for agent-driven terminal
work. It observes the current machine only: agent lifecycle hooks write a
small bounded JSON snapshot, tmux renders it, notifications surface events,
and `beacon jump` returns to the most recently completed pane.

Beacon does not schedule tasks, own tmux sessions, or require Pantheon. A Mac
and an Omarchy host run independent Beacon instances. Pantheon may become an
optional provider later, but local operation must never depend on a network.

## Architecture

Beacon is a single Go binary with a background daemon:

- `beacon daemon`: local daemon, samples host metrics every 4 seconds,
  maintains an atomic snapshot cache, and listens on a Unix socket for
  report/hook updates.
- `beacon report/status/status-tmux/jump/notify/doctor/reset/hook`: CLI
  commands that read the snapshot cache and agent state. `status-tmux` is
  read-only and never invokes subprocesses; it reads pre-sampled metrics
  from the daemon's cache and renders in-process (p95 < 5ms).
- When the daemon is unreachable, `report` falls back to direct atomic file
  writes and `status-tmux` reads the last cached snapshot.

## Capabilities

- Claude Code prompt/stop/notification hooks
- Codex permission hook
- explicit status reporting for any agent via `beacon report`
- concurrent-safe, atomic local state under
  `${XDG_DATA_HOME:-~/.local/share}/beacon/panes.json`
- bounded tmux status-right output with:
  - CPU usage (macOS `top`, Linux `/proc/stat`)
  - memory pressure (macOS `memory_pressure`, Linux `/proc/meminfo`)
  - process count
  - per-pane, per-window, per-session, and total tmux memory (RSS aggregation)
  - Agent working/waiting/blocked/completed status
- jump to the last completed live pane
- macOS and Linux desktop notifications
- launchd (macOS) and systemd (Linux) user service management
- no network, database, or model calls

## Install

Requirements: Go 1.26+ and tmux.

```bash
./install.sh
beacon doctor
```

The install script builds the Go binary, installs it to `~/.local/lib/beacon/`,
symlinks it to `~/.local/bin/beacon`, and sets up the daemon service
(launchd on macOS, systemd on Linux). The previous shell-based implementation
is preserved under `~/.local/lib/beacon/shell-backup/` for rollback within
the current release.

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

## Daemon management

```bash
beacon daemon start    # start the daemon (foreground)
beacon daemon stop     # stop the running daemon
beacon daemon status   # check if the daemon is running
```

Under launchd/systemd, the daemon starts automatically and restarts on
failure. `beacon doctor` reports daemon, socket, and cache freshness.

## Status bar segments

Segments are ordered by priority; narrow widths drop lower-priority segments
first. Priority order: Agent status, memory pressure, pane memory, CPU,
window memory, session memory, total tmux memory, process count.

Icons and colors match the previous agent-tracker implementation:

| Segment | Icon | Color (bg) |
|---|---|---|
| CPU | `` | green/yellow/red by usage |
| Memory pressure | `` | green/yellow/red by pressure |
| Pane memory | `` | `#7CB8BB` |
| Window memory | `󰖲` | `#5A8A8A` |
| Session memory | `` | `#4A7A7A` |
| Total tmux memory | `󰍛` | `#3A6A6A` |
| Process count | `` | `#B48EAD` |
| Agent working | `●` | `#F0DFAF` |
| Agent completed | `✓` | `#7F9F7F` |
| Agent waiting | `⚠` | `#CC9393` |
| Agent blocked | `✗` | `#CC3333` |

## Boundaries

- Runtime state is local and disposable; Git stores configuration, not state.
- tmux is a presentation/execution surface, not a task-state authority.
- Beacon does not replace Pantheon orchestration or Mnemos memory.
- Host telemetry stays deliberately small and cheap. Use a monitoring system
  for historical or fleet-level metrics.

## Development

```bash
go test ./...           # run all tests
go test -race ./...     # run with race detector
go build ./cmd/beacon   # build the binary
```
