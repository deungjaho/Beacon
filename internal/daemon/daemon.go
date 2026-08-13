// Package daemon implements the Beacon background daemon. It samples host
// metrics on a bounded interval, writes an atomic snapshot cache, and listens
// on a Unix socket for report/hook updates.
package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"beacon/internal/collector"
	"beacon/internal/state"
)

// Config controls daemon behavior.
type Config struct {
	StateDir   string // panes.json directory
	CacheDir   string // snapshot.json + socket directory
	SocketPath string // Unix socket path
	Interval   time.Duration
	OS         string
	TmuxBin    string
}

// Daemon is the background sampler and socket server.
type Daemon struct {
	cfg       Config
	store     *state.Store
	collector *collector.Collector
	mu        sync.RWMutex
	current   collector.Metrics
	sampleMu  sync.Mutex
	stop      chan struct{}
	done      chan struct{}
}

// New creates a Daemon. The directories are created if needed.
func New(cfg Config) (*Daemon, error) {
	if cfg.Interval <= 0 {
		cfg.Interval = 4 * time.Second
	}
	if cfg.OS == "" {
		cfg.OS = runtime.GOOS
	}
	if cfg.TmuxBin == "" {
		cfg.TmuxBin = "tmux"
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(cfg.CacheDir, "beacon.sock")
	}
	store, err := state.NewStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("daemon: mkdir cache: %w", err)
	}
	return &Daemon{
		cfg:       cfg,
		store:     store,
		collector: collector.NewCollector(cfg.OS, cfg.TmuxBin),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}, nil
}

// Run starts the sampling loop and socket server. Blocks until Stop is called.
func (d *Daemon) Run() error {
	// Initial sample (in goroutine so Run can proceed to socket setup).
	go d.sample()
	// Start socket listener.
	listener, err := d.listen()
	if err != nil {
		close(d.done)
		return err
	}
	defer func() {
		listener.Close()
		_ = os.Remove(d.cfg.SocketPath)
		close(d.done)
	}()

	// Sampling ticker.
	ticker := time.NewTicker(d.cfg.Interval)
	defer ticker.Stop()

	// Socket accept loop (goroutine).
	go d.acceptLoop(listener)

	// Main loop: fire samples in goroutines so Stop stays responsive.
	for {
		select {
		case <-ticker.C:
			go d.sample()
		case <-d.stop:
			return nil
		}
	}
}

// Stop signals the daemon to stop and waits for cleanup.
func (d *Daemon) Stop() {
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
	<-d.done
}

func (d *Daemon) sample() {
	d.sampleMu.Lock()
	defer d.sampleMu.Unlock()
	m := d.collector.Sample()
	d.mu.Lock()
	d.current = m
	d.mu.Unlock()
	d.writeSnapshot(m)
}

// SnapshotPath returns the path to the snapshot cache file.
func (d *Daemon) SnapshotPath() string {
	return filepath.Join(d.cfg.CacheDir, "snapshot.json")
}

func (d *Daemon) writeSnapshot(m collector.Metrics) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := d.SnapshotPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.SnapshotPath())
}

func (d *Daemon) listen() (net.Listener, error) {
	// Remove stale socket.
	if _, err := os.Stat(d.cfg.SocketPath); err == nil {
		// Try connecting; if it fails, the socket is stale.
		conn, err := net.Dial("unix", d.cfg.SocketPath)
		if err != nil {
			_ = os.Remove(d.cfg.SocketPath)
		} else {
			conn.Close()
			return nil, fmt.Errorf("daemon: already running on %s", d.cfg.SocketPath)
		}
	}
	// Ensure parent dir exists.
	if err := os.MkdirAll(filepath.Dir(d.cfg.SocketPath), 0o700); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: listen: %w", err)
	}
	// Set socket permissions.
	_ = os.Chmod(d.cfg.SocketPath, 0o600)
	return listener, nil
}

func (d *Daemon) acceptLoop(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-d.stop:
				return
			default:
				continue
			}
		}
		go d.handleConn(conn)
	}
}

// SocketRequest is the JSON protocol for the Unix socket.
type SocketRequest struct {
	Action  string `json:"action"` // "report", "set-last", "cleanup", "acknowledge", "ping"
	Pane    string `json:"pane,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
	Window  string `json:"window,omitempty"`
	Session string `json:"session,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Time    int64  `json:"time,omitempty"`
}

type socketResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	var req SocketRequest
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(socketResponse{OK: false, Error: err.Error()})
		return
	}
	resp := d.handleRequest(req)
	_ = enc.Encode(resp)
}

func (d *Daemon) handleRequest(req SocketRequest) socketResponse {
	switch req.Action {
	case "ping":
		return socketResponse{OK: true}
	case "report":
		if req.Pane == "" {
			return socketResponse{OK: false, Error: "pane required"}
		}
		rec := state.PaneRecord{
			Status:  req.Status,
			Summary: state.SanitizeSummary(req.Summary),
			Window:  req.Window,
			Session: req.Session,
			Cwd:     req.Cwd,
			Time:    req.Time,
		}
		if rec.Time == 0 {
			rec.Time = time.Now().Unix()
		}
		if err := d.store.SetPane(req.Pane, rec); err != nil {
			return socketResponse{OK: false, Error: err.Error()}
		}
		if req.Status == "completed" {
			_ = d.store.SetLast(state.LastCompleted{
				Pane:    req.Pane,
				Session: req.Session,
				Window:  req.Window,
				Summary: rec.Summary,
				Time:    rec.Time,
			})
		}
		return socketResponse{OK: true}
	case "set-last":
		if req.Pane == "" {
			return socketResponse{OK: false, Error: "pane required"}
		}
		if err := d.store.SetLast(state.LastCompleted{
			Pane:    req.Pane,
			Session: req.Session,
			Window:  req.Window,
			Summary: state.SanitizeSummary(req.Summary),
			Time:    req.Time,
		}); err != nil {
			return socketResponse{OK: false, Error: err.Error()}
		}
		return socketResponse{OK: true}
	case "cleanup":
		livePanes := d.livePanes()
		d.store.Cleanup(time.Now().Unix(), state.CompletedTTLSeconds, livePanes)
		return socketResponse{OK: true}
	case "acknowledge":
		if req.Pane == "" {
			return socketResponse{OK: false, Error: "pane required"}
		}
		if err := d.store.Acknowledge(req.Pane); err != nil {
			return socketResponse{OK: false, Error: err.Error()}
		}
		return socketResponse{OK: true}
	default:
		return socketResponse{OK: false, Error: "unknown action: " + req.Action}
	}
}

func (d *Daemon) livePanes() []string {
	out, err := exec.Command(d.cfg.TmuxBin, "list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		return nil
	}
	var panes []string
	line := ""
	for _, b := range out {
		if b == '\n' {
			if line != "" {
				panes = append(panes, line)
			}
			line = ""
		} else {
			line += string(b)
		}
	}
	if line != "" {
		panes = append(panes, line)
	}
	return panes
}

// CurrentMetrics returns the latest sampled metrics (thread-safe).
func (d *Daemon) CurrentMetrics() collector.Metrics {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.current
}

// IsRunning checks if a daemon is listening on the given socket path.
func IsRunning(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(SocketRequest{Action: "ping"}); err != nil {
		return false
	}
	var resp socketResponse
	if err := dec.Decode(&resp); err != nil {
		return false
	}
	return resp.OK
}

// SendReport sends a report request to the daemon via Unix socket.
// Returns an error if the daemon is unreachable.
func SendReport(socketPath string, req SocketRequest) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(req); err != nil {
		return err
	}
	var resp socketResponse
	if err := dec.Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}
	return nil
}

// SendAcknowledge sends an acknowledge request to the daemon via socket.
// Falls back silently if the daemon is not running (caller should do file-based ack).
func SendAcknowledge(socketPath, pane string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(SocketRequest{Action: "acknowledge", Pane: pane}); err != nil {
		return err
	}
	var resp socketResponse
	if err := dec.Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}
	return nil
}

// KillDaemon sends SIGTERM to the daemon process listening on socketPath.
// This is used by `beacon daemon stop`.
func KillDaemon(socketPath string) error {
	// Read the pid from the socket path's sibling pid file.
	pidFile := socketPath + ".pid"
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("daemon: no pid file: %w", err)
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return fmt.Errorf("daemon: bad pid file: %w", err)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("daemon: kill: %w", err)
	}
	_ = os.Remove(pidFile)
	_ = os.Remove(socketPath)
	return nil
}
