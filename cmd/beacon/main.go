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
  status-tmux [args...]   render tmux status-right (resource metrics only)
  jump                    jump to oldest unacknowledged notification pane
  acknowledge <pane_id>   clear notification bell for a pane
  acknowledge-visible     clear bells for all panes visible to attached clients
  sync-bells             sync tmux user options from Beacon state
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
	case "acknowledge":
		os.Exit(cmdAcknowledge(args))
	case "acknowledge-visible":
		os.Exit(cmdAcknowledgeVisible(args))
	case "sync-bells":
		os.Exit(cmdSyncBells(args))
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
			// Sync tmux bell options (event-driven, not per-render).
			if store, err := state.NewStore(defaultStateDir()); err == nil {
				syncBells(store)
			}
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
	syncBells(store)
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

	// Read metrics snapshot (fast file read, no subprocess).
	var m collector.Metrics
	snapshotPath := filepath.Join(defaultCacheDir(), "snapshot.json")
	if data, err := os.ReadFile(snapshotPath); err == nil {
		_ = json.Unmarshal(data, &m)
	}

	output := render.Render(rargs, m)
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
	// Jump to the oldest unacknowledged notification pane.
	// If no pending notifications, this is a no-op (no fallback to LastCompleted).
	pending := store.PendingNotifications()
	for _, target := range pending {
		// Verify pane is still live.
		if _, err := exec.Command(tmuxBin(), "display-message", "-p", "-t", target.Pane, "#{pane_id}").Output(); err != nil {
			// Pane is gone; acknowledge it (clears stale bell) and try next.
			_ = ackPane(store, target.Pane)
			continue
		}
		_ = exec.Command(tmuxBin(), "switch-client", "-t", target.Session).Run()
		_ = exec.Command(tmuxBin(), "select-pane", "-t", target.Pane).Run()
		// Acknowledge the bell on arrival.
		_ = ackPane(store, target.Pane)
		return 0
	}
	// No pending notifications or all pending panes are gone.
	return 0
}

func cmdAcknowledge(args []string) int {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "beacon: acknowledge requires a pane ID\n")
		return 2
	}
	pane := args[0]
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 1
	}
	if err := ackPane(store, pane); err != nil {
		fmt.Fprintf(os.Stderr, "beacon: acknowledge: %v\n", err)
		return 1
	}
	return 0
}

// cmdAcknowledgeVisible acknowledges all pending notification panes that are
// currently visible to any attached tmux client. This is the unified hook
// handler: after-select-pane, after-select-window, and client-session-changed
// all call this. It runs one tmux list-clients, deduplicates visible pane IDs,
// acknowledges only those that have pending (unacknowledged) notifications,
// then syncs bells once. Detached sessions keep their bells.
func cmdAcknowledgeVisible(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 1
	}

	// Get all pending notification panes (unacknowledged waiting/blocked/completed).
	pending := store.PendingNotifications()
	if len(pending) == 0 {
		return 0
	}
	pendingSet := make(map[string]bool, len(pending))
	for _, p := range pending {
		pendingSet[p.Pane] = true
	}

	// One tmux call: list all attached clients and their visible pane.
	// Use | as separator — tmux format \t produces literal backslash-t, not a real tab.
	tb := tmuxBin()
	out, err := exec.Command(tb, "list-clients", "-F", "#{client_tty}|#{pane_id}").Output()
	if err != nil {
		return 0 // non-fatal: tmux not available
	}

	// Collect unique visible pane IDs that have pending notifications.
	visiblePending := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "|", 2)
		if len(fields) < 2 {
			continue
		}
		paneID := strings.TrimSpace(fields[1])
		if pendingSet[paneID] {
			visiblePending[paneID] = true
		}
	}

	if len(visiblePending) == 0 {
		return 0
	}

	// Acknowledge each visible pending pane.
	for paneID := range visiblePending {
		_ = store.Acknowledge(paneID)
	}

	// Sync bells once after all acknowledgements.
	syncBells(store)
	return 0
}

// ackPane tries the daemon socket first (fast path), falls back to direct file write.
// After acknowledging, syncs tmux bell options.
func ackPane(store *state.Store, pane string) error {
	socketPath := filepath.Join(defaultStateDir(), "beacon.sock")
	if err := daemon.SendAcknowledge(socketPath, pane); err == nil {
		syncBells(store)
		return nil
	}
	if err := store.Acknowledge(pane); err != nil {
		return err
	}
	syncBells(store)
	return nil
}

// cmdSyncBells synchronizes tmux user options @beacon_pane_bell (pane scope),
// @beacon_window_bell (window scope), and @beacon_session_bell (session scope)
// from the current Beacon state. Called after report/acknowledge/cleanup/reset.
// Uses three distinct option names to avoid tmux scope inheritance confusion.
func cmdSyncBells(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 1
	}
	syncBells(store)
	return 0
}

// syncBells reads state and sets/unsets tmux user options at the correct scope.
// Pane options are set per-pane with -t pane. Window options with -t window.
// Session options with -t session. This is event-driven (called only on state
// change), not per-render.
func syncBells(store *state.Store) {
	tb := tmuxBin()
	st, err := store.Load()
	if err != nil {
		return
	}

	// Build sets of panes/windows/sessions with unacknowledged notifications.
	paneBell := map[string]bool{}    // pane_id -> has bell
	windowBell := map[string]bool{}  // window_id -> has bell
	sessionBell := map[string]bool{} // session_name -> has bell

	for paneID, rec := range st.Panes {
		if state.IsNotificationStatus(rec.Status) && !rec.Acknowledged {
			paneBell[paneID] = true
			if rec.Window != "" {
				windowBell[rec.Window] = true
			}
			if rec.Session != "" {
				sessionBell[rec.Session] = true
			}
		}
	}

	// Sync pane-scope options: set bell on notified panes, unset on others.
	// We iterate all live panes to clear stale bells on panes that no longer
	// have a notification.
	livePanes := listLivePanesWithTargets()
	for _, lp := range livePanes {
		if paneBell[lp.paneID] {
			_ = exec.Command(tb, "set-option", "-p", "-t", lp.paneID, "@beacon_pane_bell", "1").Run()
		} else {
			_ = exec.Command(tb, "set-option", "-p", "-t", lp.paneID, "-u", "@beacon_pane_bell").Run()
		}
	}

	// Sync window-scope options.
	liveWindows := listLiveWindows()
	for _, wid := range liveWindows {
		if windowBell[wid] {
			_ = exec.Command(tb, "set-option", "-w", "-t", wid, "@beacon_window_bell", "1").Run()
		} else {
			_ = exec.Command(tb, "set-option", "-w", "-t", wid, "-u", "@beacon_window_bell").Run()
		}
	}

	// Sync session-scope options (no -g; -t targets the specific session).
	liveSessions := listLiveSessions()
	for _, sname := range liveSessions {
		if sessionBell[sname] {
			_ = exec.Command(tb, "set-option", "-t", sname, "@beacon_session_bell", "1").Run()
		} else {
			_ = exec.Command(tb, "set-option", "-t", sname, "-u", "@beacon_session_bell").Run()
		}
	}
}

type livePaneTarget struct {
	paneID   string
	windowID string
	session  string
}

func listLivePanesWithTargets() []livePaneTarget {
	out, err := exec.Command(tmuxBin(), "list-panes", "-a", "-F", "#{pane_id}\t#{window_id}\t#{session_name}").Output()
	if err != nil {
		return nil
	}
	var panes []livePaneTarget
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		panes = append(panes, livePaneTarget{paneID: parts[0], windowID: parts[1], session: parts[2]})
	}
	return panes
}

func listLiveWindows() []string {
	out, err := exec.Command(tmuxBin(), "list-windows", "-a", "-F", "#{window_id}").Output()
	if err != nil {
		return nil
	}
	var windows []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			windows = append(windows, line)
		}
	}
	return windows
}

func listLiveSessions() []string {
	out, err := exec.Command(tmuxBin(), "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil
	}
	var sessions []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions
}

func cmdCleanup(args []string) int {
	store, err := state.NewStore(defaultStateDir())
	if err != nil {
		return 0
	}
	livePanes := listLivePanes()
	store.Cleanup(nowSeconds(), state.CompletedTTLSeconds, livePanes)
	syncBells(store)
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
	syncBells(store)
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
			if st, err := state.NewStore(defaultStateDir()); err == nil {
				syncBells(st)
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
	syncBells(store)
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
