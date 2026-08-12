# agent-tracker retirement

Status: cutover accepted on 2026-08-12 for Mac and Omarchy.

## Capability mapping

| Previous responsibility | New owner |
|---|---|
| local tmux pane state, status line, notifications, jump-to-agent | Beacon |
| browser control MCP | Argus |
| durable project/run/task orchestration | Pantheon |
| long-term memory | Mnemos |

Beacon deliberately does not preserve agent-tracker's task database, browser
server, or daemon. Runtime state remains local, bounded, and disposable.

## Cutover evidence

- Beacon tests pass on macOS and Linux.
- `beacon doctor` passes on both hosts.
- prompt and stop hook smoke tests update the selected tmux pane on both hosts.
- tmux status and `beacon jump` are active on both hosts.
- Codex, Claude, Devin, Gemini, and OpenCode new-session configurations no
  longer register tracker or agent_browser; Argus is registered instead.
- The macOS `com.agent-tracker.server` LaunchAgent is unloaded.
- The Omarchy `agent-tracker.service` unit is disabled and inactive.

Existing agent sessions may retain their already-spawned `tracker-mcp`
children until those sessions exit. They are not killed during migration,
because doing so would interrupt live work. New sessions do not spawn them.

## Retained rollback material

The old binaries, configuration, LaunchAgent/unit files, and two unreferenced
tmux compatibility scripts are retained temporarily. They are not in an
active execution path. Per-host pre-cutover configuration is stored under:

```text
~/.local/state/beacon/migrations/20260812-beacon-cutover/
```

The previous Bash/jq shell implementation of Beacon is preserved under
`~/.local/lib/beacon/shell-backup/` during the Go binary release. To roll
back, replace the Go binary with the shell wrapper and restart the daemon.
Remove the shell backup once the Go binary has run a full session lifecycle
without regression.

## Go binary upgrade (2026-08-12)

Beacon is now a single Go binary with a background daemon:

- `beacon daemon` samples host metrics (CPU, memory pressure, process count,
  per-pane/window/session/total tmux memory) every 4 seconds and writes an
  atomic snapshot cache.
- `beacon status-tmux` reads the snapshot cache and agent state in-process;
  no subprocess calls, p95 < 5ms.
- `beacon report` writes via Unix socket when the daemon is running, with
  atomic file fallback when it is not.
- launchd (macOS) and systemd (Linux) user services manage the daemon
  lifecycle with automatic restart.
- The shell scripts (`bin/beacon`, `lib/*.sh`, `tmux/*.sh`, `hooks/*.sh`)
  are retained in the repository for rollback but are no longer the primary
  execution path. Remove them in a separate cleanup once the Go binary has
  been verified across a full session lifecycle on both Mac and Omarchy.

## Regression checks

```bash
beacon doctor
beacon status | jq
tmux list-keys -T prefix | grep beacon
codex mcp list
```

Search active configuration for accidental reintroduction:

```bash
rg 'agent-tracker|tracker-client|mcp_servers\.tracker|agent_browser|OP_TRACKER' \
  ~/.codex ~/.config/devin ~/.gemini ~/.config/opencode ~/.config/claude \
  ~/.config/zsh/functions ~/.config/tmux
```

Ignore historical chat logs, migration backups, `retired/`, and explicitly
retained compatibility files when interpreting the search result.
