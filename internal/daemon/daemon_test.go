package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"beacon/internal/collector"
	"beacon/internal/state"
)

func newTestDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	dir := t.TempDir()
	// macOS limits Unix socket paths to ~104 chars; t.TempDir() is too long.
	// Use a short unique socket path in /tmp.
	sockPath := filepath.Join("/tmp", "beacon-test-"+strconv.Itoa(int(time.Now().UnixNano()))+".sock")
	t.Cleanup(func() { os.Remove(sockPath) })
	cfg := Config{
		StateDir:   filepath.Join(dir, "state"),
		CacheDir:   filepath.Join(dir, "cache"),
		SocketPath: sockPath,
		Interval:   10 * time.Second, // long interval; we trigger samples manually
		OS:         "darwin",
		TmuxBin:    "tmux",
	}
	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d, dir
}

func TestDaemonWritesSnapshot(t *testing.T) {
	d, _ := newTestDaemon(t)
	// Manually trigger one sample to avoid long waits.
	d.sample()
	data, err := os.ReadFile(d.SnapshotPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("snapshot is empty")
	}
}

func TestDaemonSocketReport(t *testing.T) {
	d, dir := newTestDaemon(t)
	// Start daemon in background.
	go d.Run()
	defer d.Stop()
	// Wait for socket to be ready.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if IsRunning(d.cfg.SocketPath) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !IsRunning(d.cfg.SocketPath) {
		t.Fatal("daemon did not start")
	}
	// Send a report.
	err := SendReport(d.cfg.SocketPath, SocketRequest{
		Action:  "report",
		Pane:    "%1",
		Status:  "working",
		Summary: "test job",
		Window:  "@1",
		Session: "s",
		Time:    100,
	})
	if err != nil {
		t.Fatalf("SendReport: %v", err)
	}
	// Verify state was written.
	store, _ := state.NewStore(filepath.Join(dir, "state"))
	st, _ := store.Load()
	rec, ok := st.Panes["%1"]
	if !ok {
		t.Fatal("pane not found")
	}
	if rec.Status != "working" {
		t.Fatalf("status: got %q want working", rec.Status)
	}
}

func TestDaemonConcurrentReportsNoLoss(t *testing.T) {
	d, dir := newTestDaemon(t)
	go d.Run()
	defer d.Stop()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if IsRunning(d.cfg.SocketPath) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !IsRunning(d.cfg.SocketPath) {
		t.Fatal("daemon did not start")
	}
	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pane := "%" + itoa(i)
			_ = SendReport(d.cfg.SocketPath, SocketRequest{
				Action:  "report",
				Pane:    pane,
				Status:  "working",
				Summary: "job",
				Window:  "@1",
				Session: "s",
				Time:    int64(i),
			})
		}(i)
	}
	wg.Wait()
	store, _ := state.NewStore(filepath.Join(dir, "state"))
	st, _ := store.Load()
	if len(st.Panes) != 50 {
		t.Fatalf("pane count: got %d want 50", len(st.Panes))
	}
}

func TestDaemonKillCLIDoesNotHang(t *testing.T) {
	d, _ := newTestDaemon(t)
	go d.Run()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if IsRunning(d.cfg.SocketPath) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !IsRunning(d.cfg.SocketPath) {
		t.Fatal("daemon did not start")
	}
	d.Stop()
	// After stop, IsRunning should return false.
	if IsRunning(d.cfg.SocketPath) {
		t.Fatal("daemon should not be running after Stop")
	}
}

func TestDaemonSnapshotFallback(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.sample()
	// Simulate daemon down: snapshot file should still be readable.
	data, err := os.ReadFile(d.SnapshotPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m collector.Metrics
	if err := jsonUnmarshal(data, &m); err != nil {
		t.Fatalf("snapshot corrupt: %v", err)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
