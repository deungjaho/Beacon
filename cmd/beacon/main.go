// Beacon: local-first Agent/tmux status tool. Single binary, no dependencies.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"beacon/internal/collector"
	"beacon/internal/daemon"
	"beacon/internal/render"
	"beacon/internal/state"
)

const (
	envStateDir   = "BEACON_STATE_DIR"
	envCacheDir   = "BEACON_CACHE_DIR"
	envTmuxBin    = "BEACON_TMUX_BIN"
	envNow        = "BEACON_NOW"
	envNotify     = "BEACON_NOTIFY"
	envShowSystem = "BEACON_SHOW_SYSTEM"
)

func defaultStateDir() string {
	if v := os.Getenv(envStateDir); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "beacon")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "beacon")
}

func defaultCacheDir() string {
	if v := os.Getenv(envCacheDir); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "beacon")
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches", "beacon")
	}
	return filepath.Join(home, ".cache", "beacon")
}

func defaultSocketPath() string {
	// Socket lives in the state dir so that overriding BEACON_STATE_DIR
	// also redirects the socket, preventing mismatched daemon/CLI state.
	return filepath.Join(defaultStateDir(), "beacon.sock")
}

func tmuxBin() string {
	if v := os.Getenv(envTmuxBin); v != "" {
		return v
	}
	return "tmux"
}

func nowSeconds() int64 {
	if v := os.Getenv(envNow); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return time.Now().Unix()
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: beacon <command> [args]

commands:
  report <working|waiting|blocked|completed> [summary] [cwd]
  notify <title> <message>
  status                  print JSON state
  status-tmux [args...]   render tmux status-right
  jump                    jump to the last completed pane
  cleanup                 remove expired or dead-pane records
  reset                   clear local Beacon state
  hook <prompt|stop|notification|permission>
  daemon [start|stop|status]  manage background sampler
  doctor                  validate local dependencies and state
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "report":
		os.Exit(cmdReport(args))
	case "notify":
		os.Exit(cmdNotify(args))
	case "status":
		os.Exit(cmdStatus(args))
	case "status-tmux":
		os.Exit(cmdStatusTmux(args))
	case "jump":
		os.Exit(cmdJump(args))
	case "cleanup":
		os.Exit(cmdCleanup(args))
	case "reset":
		os.Exit(cmdReset(args))
	case "hook":
		os.Exit(cmdHook(args))
	case "daemon":
		os.Exit(cmdDaemon(args))
	case "doctor":
		os.Exit(cmdDoctor(args))
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "beacon: unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func cmdReport(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "beacon: report requires a status")
		return 2
	}
	status := args[0]
	summary := ""
	if len(args) > 1 {
		summary = args[1]
	}
	cwd := ""
	if len(args) > 2 {
		cwd = args[2]
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return 0
	}
	session, window := tmuxContext(pane)
	now := nowSeconds()
	summary = state.SanitizeSummary(summary)

	// Try daemon socket first (fast path, no fork).
	sock := defaultSocketPath()
	if daemon.IsRunning(sock) {
		req := daemon.SocketRequest{
			Action:  "report",
			Pane:    pane,
			Status:  status,
			Summary: summary,
			Window:  window,
			Session: session,
			Cwd:     cwd,
			Time:    now,
		}
		if err := daemon.SendReport(sock, req); err == nil {
			if status == "completed" {
				_ = daemon.SendReport(sock, daemon.SocketRequest{
					Action:  "set-last",
					Pane:    pane,
					Session: session,
					Window:  window,
					Summary: summary,
					Time:    now,
				})
			}
			// Best-effort cleanup.
			_ = daemon.SendReport(sock, daemon.SocketRequest{Action: "cleanup"})
			return 0
		}
	}

	// Fallback: direct file write.
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 0 // non-fatal
	}
	rec := state.PaneRecord{
		Status:  status,
		Summary: summary,
		Window:  window,
		Session: session,
		Time:    now,
		Cwd:     cwd,
	}
	if err := store.SetPane(pane, rec); err != nil {
		return 0
	}
	if status == "completed" {
		_ = store.SetLast(state.LastCompleted{
			Pane:    pane,
			Session: session,
			Window:  window,
			Summary: summary,
			Time:    now,
		})
	}
	// Best-effort cleanup.
	livePanes := listLivePanes()
	store.Cleanup(now, state.CompletedTTLSeconds, livePanes)
	return 0
}

func tmuxContext(pane string) (session, window string) {
	tb := tmuxBin()
	out, err := exec.Command(tb, "display-message", "-p", "-t", pane, "#{session_name}").Output()
	if err == nil {
		session = strings.TrimSpace(string(out))
	}
	out, err = exec.Command(tb, "display-message", "-p", "-t", pane, "#{window_id}").Output()
	if err == nil {
		window = strings.TrimSpace(string(out))
	}
	return
}

func listLivePanes() []string {
	out, err := exec.Command(tmuxBin(), "list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		return nil
	}
	var panes []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			panes = append(panes, line)
		}
	}
	return panes
}

func cmdNotify(args []string) int {
	title := "Agent"
	if len(args) > 0 {
		title = args[0]
	}
	message := ""
	if len(args) > 1 {
		message = args[1]
	}
	if message == "" || os.Getenv(envNotify) == "0" {
		return 0
	}
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", "--", title, message).Run()
	case "linux":
		_ = exec.Command("notify-send", "--", title, message).Run()
	}
	return 0
}

func cmdStatus(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
		return 1
	}
	st, err := store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
		return 1
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	fmt.Println(string(data))
	return 0
}

func cmdStatusTmux(args []string) int {
	// Args: width status_bg session_name window_index pane_id window_id
	rargs := render.Args{
		Width:       100,
		StatusBG:    "black",
		SessionName: "",
		WindowIndex: "",
		PaneID:      "",
		WindowID:    "",
	}
	if len(args) > 0 {
		rargs.Width, _ = strconv.Atoi(strings.TrimSpace(args[0]))
	}
	if len(args) > 1 {
		rargs.StatusBG = strings.TrimSpace(args[1])
	}
	if len(args) > 2 {
		rargs.SessionName = strings.TrimSpace(args[2])
	}
	if len(args) > 3 {
		rargs.WindowIndex = strings.TrimSpace(args[3])
	}
	if len(args) > 4 {
		rargs.PaneID = strings.TrimSpace(args[4])
	}
	if len(args) > 5 {
		rargs.WindowID = strings.TrimSpace(args[5])
	}

	// Read agent state (fast file read).
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 0
	}
	st, _ := store.Load()

	// Read metrics snapshot (fast file read, no subprocess).
	var m collector.Metrics
	snapshotPath := filepath.Join(defaultCacheDir(), "snapshot.json")
	if data, err := os.ReadFile(snapshotPath); err == nil {
		_ = json.Unmarshal(data, &m)
	}

	output := render.Render(rargs, st, m)
	if output != "" {
		fmt.Print(output)
	}
	return 0
}

func cmdJump(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 0
	}
	st, err := store.Load()
	if err != nil || st.LastCompleted == nil {
		return 0
	}
	pane := st.LastCompleted.Pane
	session := st.LastCompleted.Session
	if pane == "" || session == "" {
		return 0
	}
	// Verify pane is still live.
	if _, err := exec.Command(tmuxBin(), "display-message", "-p", "-t", pane, "#{pane_id}").Output(); err != nil {
		return 0
	}
	_ = exec.Command(tmuxBin(), "switch-client", "-t", session).Run()
	_ = exec.Command(tmuxBin(), "select-pane", "-t", pane).Run()
	return 0
}

func cmdCleanup(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 0
	}
	livePanes := listLivePanes()
	store.Cleanup(nowSeconds(), state.CompletedTTLSeconds, livePanes)
	return 0
}

func cmdReset(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 1
	}
	if err := store.Reset(); err != nil {
		return 1
	}
	return 0
}

func cmdHook(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "beacon: hook requires an event name")
		return 2
	}
	event := args[0]
	switch event {
	case "prompt", "stop", "notification", "permission":
	default:
		fmt.Fprintf(os.Stderr, "beacon: unsupported hook: %s\n", event)
		return 2
	}
	// Read stdin (hook payload). Non-fatal on parse errors.
	input, _ := io.ReadAll(os.Stdin)
	var payload map[string]interface{}
	_ = json.Unmarshal(input, &payload)

	strVal := func(key string) string {
		if payload == nil {
			return ""
		}
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	cwd := strVal("cwd")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	switch event {
	case "prompt":
		prompt := strVal("prompt")
		if prompt == "" {
			prompt = strVal("user_prompt")
		}
		if prompt == "" {
			prompt = strVal("input")
		}
		runReport("working", prompt, cwd)
	case "stop":
		msg := strVal("last_assistant_message")
		if msg == "" {
			msg = strVal("last-assistant-message")
		}
		if msg == "" {
			msg = strVal("message")
		}
		if msg == "" {
			msg = "completed"
		}
		runReport("completed", msg, cwd)
		agentName := os.Getenv("BEACON_AGENT_NAME")
		if agentName == "" {
			agentName = "Claude"
		}
		runNotify(agentName, "✓ "+msg)
	case "notification":
		msg := strVal("message")
		if msg == "" {
			msg = strVal("notification")
		}
		if msg == "" {
			msg = "Agent is waiting"
		}
		runReport("waiting", msg, cwd)
		agentName := os.Getenv("BEACON_AGENT_NAME")
		if agentName == "" {
			agentName = "Claude"
		}
		runNotify(agentName, "⚠ "+msg)
	case "permission":
		tool := strVal("tool_name")
		if tool == "" {
			tool = strVal("tool")
		}
		if tool == "" {
			tool = "operation"
		}
		msg := "Permission required: " + tool
		runReport("waiting", msg, cwd)
		agentName := os.Getenv("BEACON_AGENT_NAME")
		if agentName == "" {
			agentName = "Codex"
		}
		runNotify(agentName, msg)
	}
	return 0
}

// runReport is a thin wrapper around cmdReport logic for hooks.
func runReport(status, summary, cwd string) {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return
	}
	session, window := tmuxContext(pane)
	now := nowSeconds()
	summary = state.SanitizeSummary(summary)

	sock := defaultSocketPath()
	if daemon.IsRunning(sock) {
		req := daemon.SocketRequest{
			Action:  "report",
			Pane:    pane,
			Status:  status,
			Summary: summary,
			Window:  window,
			Session: session,
			Cwd:     cwd,
			Time:    now,
		}
		if err := daemon.SendReport(sock, req); err == nil {
			if status == "completed" {
				_ = daemon.SendReport(sock, daemon.SocketRequest{
					Action:  "set-last",
					Pane:    pane,
					Session: session,
					Window:  window,
					Summary: summary,
					Time:    now,
				})
			}
			return
		}
	}

	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return
	}
	rec := state.PaneRecord{
		Status:  status,
		Summary: summary,
		Window:  window,
		Session: session,
		Time:    now,
		Cwd:     cwd,
	}
	_ = store.SetPane(pane, rec)
	if status == "completed" {
		_ = store.SetLast(state.LastCompleted{
			Pane:    pane,
			Session: session,
			Window:  window,
			Summary: summary,
			Time:    now,
		})
	}
}

func runNotify(title, message string) {
	if os.Getenv(envNotify) == "0" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", "--", title, message).Run()
	case "linux":
		_ = exec.Command("notify-send", "--", title, message).Run()
	}
}

func cmdDaemon(args []string) int {
	action := "start"
	if len(args) > 0 {
		action = args[0]
	}
	sock := defaultSocketPath()
	switch action {
	case "start":
		if daemon.IsRunning(sock) {
			fmt.Println("beacon: daemon already running")
			return 0
		}
		cfg := daemon.Config{
			StateDir:   defaultStateDir(),
			CacheDir:   defaultCacheDir(),
			SocketPath: sock,
			Interval:   4 * time.Second,
			OS:         runtime.GOOS,
			TmuxBin:    tmuxBin(),
		}
		d, err := daemon.New(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
			return 1
		}
		// Write pid file.
		_ = os.WriteFile(sock+".pid", []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
		defer os.Remove(sock + ".pid")
		if err := d.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
			return 1
		}
		return 0
	case "stop":
		if !daemon.IsRunning(sock) {
			// Try pid file fallback.
			if err := daemon.KillDaemon(sock); err != nil {
				fmt.Fprintln(os.Stderr, "beacon: daemon not running")
				return 1
			}
			fmt.Println("beacon: daemon stopped")
			return 0
		}
		// Send stop via socket? We don't have a stop action. Use pid file.
		if err := daemon.KillDaemon(sock); err != nil {
			fmt.Fprintf(os.Stderr, "beacon: %v\n", err)
			return 1
		}
		fmt.Println("beacon: daemon stopped")
		return 0
	case "status":
		if daemon.IsRunning(sock) {
			fmt.Println("running")
			return 0
		}
		fmt.Println("stopped")
		return 1
	default:
		fmt.Fprintf(os.Stderr, "beacon: unknown daemon action: %s\n", action)
		return 2
	}
}

func cmdDoctor(args []string) int {
	failed := 0
	// Binary self-check.
	fmt.Println("ok      beacon")
	// tmux.
	if _, err := exec.LookPath(tmuxBin()); err == nil {
		fmt.Println("ok      tmux")
	} else {
		fmt.Println("missing tmux")
		failed = 1
	}
	// State.
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		fmt.Println("failed  state")
		failed = 1
	} else if _, err := store.Load(); err != nil {
		fmt.Println("failed  state")
		failed = 1
	} else {
		fmt.Println("ok      state")
	}
	// Daemon + socket.
	sock := defaultSocketPath()
	if daemon.IsRunning(sock) {
		fmt.Println("ok      daemon")
		fmt.Println("ok      socket")
	} else {
		fmt.Println("info    daemon-not-running")
	}
	// Cache freshness.
	snapshotPath := filepath.Join(defaultCacheDir(), "snapshot.json")
	if info, err := os.Stat(snapshotPath); err == nil {
		age := time.Since(info.ModTime())
		if age < 30*time.Second {
			fmt.Printf("ok      cache (%.0fs old)\n", age.Seconds())
		} else {
			fmt.Printf("stale   cache (%.0fs old)\n", age.Seconds())
		}
	} else {
		fmt.Println("missing cache")
	}
	// tmux environment.
	if os.Getenv("TMUX") != "" {
		fmt.Println("ok      tmux-environment")
	} else {
		fmt.Println("info    outside-tmux")
	}
	return failed
}
