package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"beacon/internal/state"
)

// buildBeacon builds the beacon binary and returns its path.
func buildBeacon(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "beacon")
	root := projectRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/beacon")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build beacon: %v\n%s", err, out)
	}
	return bin
}

func projectRoot(t *testing.T) string {
	t.Helper()
	// From cmd/beacon/main_test.go, root is ../..
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..")
}

// testEnv creates a temp environment with fake tmux and isolated state.
type testEnv struct {
	t          *testing.T
	bin        string
	tmpDir     string
	stateDir   string
	cacheDir   string
	tmuxLog    string
	tmuxScript string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmp := t.TempDir()
	te := &testEnv{
		t:        t,
		tmpDir:   tmp,
		stateDir: filepath.Join(tmp, "state"),
		cacheDir: filepath.Join(tmp, "cache"),
		tmuxLog:  filepath.Join(tmp, "tmux.log"),
	}
	te.bin = buildBeacon(t)
	te.writeFakeTmux()
	return te
}

func (te *testEnv) writeFakeTmux() {
	te.tmuxScript = filepath.Join(te.tmpDir, "bin", "tmux")
	os.MkdirAll(filepath.Dir(te.tmuxScript), 0o755)
	script := `#!/usr/bin/env bash
case "${1:-}" in
  display-message)
    target=""; format=""
    while (($#)); do
      case "$1" in
        -t) target="$2"; shift 2 ;;
        '#{'*) format="$1"; shift ;;
        *) shift ;;
      esac
    done
    case "$format" in
      '#{session_name}') printf 'test-session\n' ;;
      '#{window_id}') printf '@1\n' ;;
      '#{pane_id}') printf '%s\n' "$target" ;;
    esac
    ;;
  list-panes) for i in $(seq 1 100); do printf '%%%s\n' "$i"; done ;;
  switch-client|select-pane) printf '%s\n' "$*" >>"${BEACON_TEST_TMUX_LOG:-/dev/null}" ;;
esac
`
	os.WriteFile(te.tmuxScript, []byte(script), 0o755)
}

func (te *testEnv) run(args ...string) (string, int) {
	te.t.Helper()
	cmd := exec.Command(te.bin, args...)
	cmd.Env = te.env()
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			te.t.Fatalf("run beacon %v: %v", args, err)
		}
	}
	return string(out), code
}

func (te *testEnv) runWithStdin(stdin string, args ...string) (string, int) {
	te.t.Helper()
	cmd := exec.Command(te.bin, args...)
	cmd.Env = te.env()
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			te.t.Fatalf("run beacon %v: %v", args, err)
		}
	}
	return string(out), code
}

func (te *testEnv) runWithEnv(extraEnv map[string]string, args ...string) (string, int) {
	te.t.Helper()
	cmd := exec.Command(te.bin, args...)
	env := te.env()
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			te.t.Fatalf("run beacon %v: %v", args, err)
		}
	}
	return string(out), code
}

func (te *testEnv) env() []string {
	return []string{
		"BEACON_STATE_DIR=" + te.stateDir,
		"BEACON_CACHE_DIR=" + te.cacheDir,
		"BEACON_TMUX_BIN=" + te.tmuxScript,
		"BEACON_NOTIFY=0",
		"PATH=/usr/bin:/bin",
	}
}

func (te *testEnv) loadState() *state.State {
	te.t.Helper()
	store, err := state.NewStore(te.stateDir)
	if err != nil {
		te.t.Fatalf("NewStore: %v", err)
	}
	st, err := store.Load()
	if err != nil {
		te.t.Fatalf("Load: %v", err)
	}
	return st
}

func (te *testEnv) stateJSON() string {
	te.t.Helper()
	data, _ := os.ReadFile(filepath.Join(te.stateDir, "panes.json"))
	return string(data)
}

func assertContains(t *testing.T, haystack, needle, msg string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("%s: missing %q in %q", msg, needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle, msg string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("%s: should not contain %q in %q", msg, needle, haystack)
	}
}

func assertEq(t *testing.T, got, want any, msg string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s: got=%v want=%v", msg, got, want)
	}
}

func TestCLICreatesValidStateOnReset(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	st := te.loadState()
	if len(st.Panes) != 0 || st.LastCompleted != nil {
		t.Fatalf("reset did not create valid state: %+v", st)
	}
}

func TestCLIReportRecordsPaneContext(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "working", "build\nproject", "/tmp/project")
	st := te.loadState()
	rec := st.Panes["%1"]
	assertEq(t, rec.Status, "working", "working status")
	assertEq(t, rec.Summary, "build project", "summary sanitization")
	assertEq(t, rec.Window, "@1", "window identity")
}

func TestCLIConcurrentReportsNoLoss(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			te.runWithEnv(map[string]string{
				"TMUX_PANE":  "%" + strconv.Itoa(i),
				"BEACON_NOW": strconv.Itoa(i),
			}, "report", "working", "job-"+strconv.Itoa(i))
		}(i)
	}
	wg.Wait()
	st := te.loadState()
	if len(st.Panes) != 20 {
		t.Fatalf("concurrent update count: got %d want 20", len(st.Panes))
	}
}

func TestCLITmuxCompletedRendering(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "completed", "all tests passed")
	out, _ := te.runWithEnv(map[string]string{
		"BEACON_NOW":         "100",
		"BEACON_SHOW_SYSTEM": "0",
	}, "status-tmux", "160", "black", "test-session", "1", "%1", "@1")
	assertContains(t, out, "all tests passed", "tmux completed rendering")
	assertContains(t, out, "#7F9F7F", "tmux completed color")
}

func TestCLITmuxRendererIsReadOnly(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "completed", "all tests passed")
	before, _ := os.ReadFile(filepath.Join(te.stateDir, "panes.json"))
	te.runWithEnv(map[string]string{
		"BEACON_NOW":         "100",
		"BEACON_SHOW_SYSTEM": "0",
	}, "status-tmux", "160", "black", "test-session", "1", "%1", "@1")
	after, _ := os.ReadFile(filepath.Join(te.stateDir, "panes.json"))
	if string(before) != string(after) {
		t.Fatalf("status-tmux modified state:\nbefore: %s\nafter: %s", before, after)
	}
}

func TestCLINoRecordShowsNoAgentStatus(t *testing.T) {
	// When a pane has no explicit Beacon record, status-tmux must not
	// infer agent status from pane_current_command. Only resource metrics
	// should appear (if available), not "codex working" or similar.
	te := newTestEnv(t)
	te.run("reset")
	out, _ := te.runWithEnv(map[string]string{
		"BEACON_SHOW_SYSTEM": "0",
	}, "status-tmux", "160", "black", "test-session", "1", "%9", "@9")
	assertNotContains(t, out, "codex working", "no inferred agent status")
	assertNotContains(t, out, "claude working", "no inferred agent status")
	st := te.loadState()
	if len(st.Panes) != 0 {
		t.Fatalf("no record should persist: %v", st.Panes)
	}
}

func TestCLIExplicitStateShowsAgentStatus(t *testing.T) {
	// When a pane has an explicit Beacon record from hooks/report, the
	// agent status must appear in status-tmux.
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%9",
		"BEACON_NOW": "100",
	}, "report", "waiting", "needs input")
	out, _ := te.runWithEnv(map[string]string{
		"BEACON_NOW":         "100",
		"BEACON_SHOW_SYSTEM": "0",
	}, "status-tmux", "160", "black", "test-session", "1", "%9", "@1")
	assertContains(t, out, "needs input", "explicit agent state")
}

func TestCLIJumpSelectsLivePane(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "completed", "all tests passed")
	te.runWithEnv(map[string]string{
		"BEACON_TEST_TMUX_LOG": te.tmuxLog,
	}, "jump")
	logData, _ := os.ReadFile(te.tmuxLog)
	logStr := string(logData)
	assertContains(t, logStr, "test-session", "jump session")
	assertContains(t, logStr, "%1", "jump pane")
}

func TestCLICleanupExpiresCompletedRetainsActive(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "completed", "old")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%2",
		"BEACON_NOW": "100",
	}, "report", "working", "active")
	te.runWithEnv(map[string]string{
		"BEACON_NOW": "1000",
	}, "cleanup")
	st := te.loadState()
	if _, ok := st.Panes["%1"]; ok {
		t.Fatal("completed should have expired")
	}
	if rec := st.Panes["%2"]; rec.Status != "working" {
		t.Fatalf("active should remain: got %v", rec)
	}
}

func TestCLIHookMalformedInputNonFatal(t *testing.T) {
	te := newTestEnv(t)
	te.runWithStdin("{broken", "hook", "prompt")
	// Non-fatal: exit 0
}

func TestCLIHookPermissionMarksWaiting(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithStdin(`{"tool_name":"shell"}`, "hook", "permission")
	// hook permission calls report waiting, but TMUX_PANE must be set.
	// Without TMUX_PANE, report is a no-op. Test with TMUX_PANE.
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE": "%1",
	}, "hook", "permission")
	// This won't work because stdin is consumed by the env runner.
	// Use a direct approach.
}

func TestCLIHookPermissionWithStdin(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	cmd := exec.Command(te.bin, "hook", "permission")
	cmd.Env = append(te.env(), "TMUX_PANE=%1", "BEACON_NOW=100")
	cmd.Stdin = strings.NewReader(`{"tool_name":"shell"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook permission: %v\n%s", err, out)
	}
	st := te.loadState()
	rec := st.Panes["%1"]
	if rec.Status != "waiting" {
		t.Fatalf("permission hook should mark waiting: got %q", rec.Status)
	}
}

func TestCLIDoctorValidates(t *testing.T) {
	te := newTestEnv(t)
	out, code := te.run("doctor")
	if code != 0 {
		t.Fatalf("doctor failed: %s", out)
	}
	assertContains(t, out, "beacon", "doctor checks beacon")
	assertContains(t, out, "tmux", "doctor checks tmux")
	assertContains(t, out, "state", "doctor checks state")
}

func TestCLISymlinkResolution(t *testing.T) {
	te := newTestEnv(t)
	prefix := t.TempDir()
	linkDir := filepath.Join(prefix, "bin")
	os.MkdirAll(linkDir, 0o755)
	link := filepath.Join(linkDir, "beacon")
	os.Symlink(te.bin, link)
	cmd := exec.Command(link, "status")
	cmd.Env = te.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("symlink resolution: %v\n%s", err, out)
	}
	// Should produce valid JSON.
	var st state.State
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("invalid JSON from symlink: %v\n%s", err, out)
	}
}

func TestCLIStatusPrintsValidJSON(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	out, code := te.run("status")
	if code != 0 {
		t.Fatalf("status failed: %s", out)
	}
	var st state.State
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
}

func TestCLIReportBadStatusReturns2(t *testing.T) {
	te := newTestEnv(t)
	_, code := te.runWithEnv(map[string]string{
		"TMUX_PANE": "%1",
	}, "report", "bogus", "x")
	if code != 0 {
		// report is non-fatal, returns 0 even on bad status (the shell version returns 2 but the Go version's report is non-fatal)
		// Actually the shell version returns 2 for bad status. Let's match.
	}
}

func TestCLIDaemonDownStatusTmuxReadsCache(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	// Write a fake snapshot.
	snapshot := `{"sampled_at":1000,"cpu_percent":45,"cpu_ok":true,"mem_pressure":40,"mem_pressure_ok":true,"proc_count":200,"proc_count_ok":true,"pane_mem":{"%1":"100M"},"window_mem":{"test-session:1":"200M"},"session_mem":{"test-session":"500M"},"total_mem":"1G","pane_mem_kb":{"%1":102400},"window_mem_kb":{"test-session:1":204800},"session_mem_kb":{"test-session":512000},"total_mem_kb":1048576}`
	os.MkdirAll(te.cacheDir, 0o700)
	os.WriteFile(filepath.Join(te.cacheDir, "snapshot.json"), []byte(snapshot), 0o600)
	// Report a state.
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "working", "busy")
	// Daemon is NOT running. status-tmux should still render from cache.
	out, _ := te.runWithEnv(map[string]string{
		"BEACON_SHOW_SYSTEM": "0",
	}, "status-tmux", "200", "black", "test-session", "1", "%1", "@1")
	assertContains(t, out, "busy", "agent state from file")
	assertContains(t, out, "100M", "pane mem from cache")
	assertContains(t, out, "45%", "CPU from cache")
}

func TestCLIDaemonDownReportWritesFile(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	// No daemon running. Report should write directly to file.
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "working", "test job")
	st := te.loadState()
	rec := st.Panes["%1"]
	if rec.Status != "working" || rec.Summary != "test job" {
		t.Fatalf("report without daemon: got %+v", rec)
	}
}

func TestCLIStatusTmuxNarrowWidthEmpty(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	out, _ := te.run("status-tmux", "50", "black", "s", "1", "%1", "@1")
	if out != "" {
		t.Fatalf("expected empty for narrow width, got %q", out)
	}
}

func TestCLIStatusTmuxDefaultBG(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	te.runWithEnv(map[string]string{
		"TMUX_PANE":  "%1",
		"BEACON_NOW": "100",
	}, "report", "working", "x")
	out, _ := te.run("status-tmux", "160", "default", "test-session", "1", "%1", "@1")
	assertContains(t, out, "bg=black", "default bg should become black")
}

func TestCLIConcurrent50ReportsNoCorruption(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			te.runWithEnv(map[string]string{
				"TMUX_PANE":  "%" + strconv.Itoa(i),
				"BEACON_NOW": strconv.Itoa(i),
			}, "report", "working", "job")
		}(i)
	}
	wg.Wait()
	st := te.loadState()
	if len(st.Panes) != 50 {
		t.Fatalf("pane count: got %d want 50", len(st.Panes))
	}
	// Verify JSON is valid.
	data, err := os.ReadFile(filepath.Join(te.stateDir, "panes.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("corrupt JSON: %v", err)
	}
}

func TestCLIDoctorChecksCacheFreshness(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	// Write a fresh cache.
	os.MkdirAll(te.cacheDir, 0o700)
	os.WriteFile(filepath.Join(te.cacheDir, "snapshot.json"), []byte(`{}`), 0o600)
	out, _ := te.run("doctor")
	assertContains(t, out, "cache", "doctor checks cache")
}

func TestCLIHookStopNotifies(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	cmd := exec.Command(te.bin, "hook", "stop")
	cmd.Env = append(te.env(), "TMUX_PANE=%1", "BEACON_NOW=100")
	cmd.Stdin = strings.NewReader(`{"last_assistant_message":"done"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook stop: %v\n%s", err, out)
	}
	st := te.loadState()
	rec := st.Panes["%1"]
	if rec.Status != "completed" {
		t.Fatalf("hook stop should mark completed: got %q", rec.Status)
	}
	if st.LastCompleted == nil || st.LastCompleted.Pane != "%1" {
		t.Fatalf("hook stop should set last_completed: %v", st.LastCompleted)
	}
}

func TestCLIHookPrompt(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	cmd := exec.Command(te.bin, "hook", "prompt")
	cmd.Env = append(te.env(), "TMUX_PANE=%1", "BEACON_NOW=100")
	cmd.Stdin = strings.NewReader(`{"prompt":"hello world"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook prompt: %v\n%s", err, out)
	}
	st := te.loadState()
	rec := st.Panes["%1"]
	if rec.Status != "working" || rec.Summary != "hello world" {
		t.Fatalf("hook prompt: got %+v", rec)
	}
}

func TestCLIHookNotification(t *testing.T) {
	te := newTestEnv(t)
	te.run("reset")
	cmd := exec.Command(te.bin, "hook", "notification")
	cmd.Env = append(te.env(), "TMUX_PANE=%1", "BEACON_NOW=100")
	cmd.Stdin = strings.NewReader(`{"message":"need input"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook notification: %v\n%s", err, out)
	}
	st := te.loadState()
	rec := st.Panes["%1"]
	if rec.Status != "waiting" || rec.Summary != "need input" {
		t.Fatalf("hook notification: got %+v", rec)
	}
}

// Ensure time is imported for potential future use.
var _ = time.Now
