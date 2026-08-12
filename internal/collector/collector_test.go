package collector

import (
	"testing"
)

func TestParseMacOSCPUUsage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantOK  bool
	}{
		{
			name:   "normal",
			input:  "CPU usage: 65.76% user, 31.30% sys, 2.92% idle",
			want:   97.06,
			wantOK: true,
		},
		{
			name:   "low cpu",
			input:  "CPU usage: 3.50% user, 1.20% sys, 95.30% idle",
			want:   4.70,
			wantOK: true,
		},
		{
			name:   "no match",
			input:  "no cpu line here",
			wantOK: false,
		},
		{
			name:   "capped at 100",
			input:  "CPU usage: 80.0% user, 40.0% sys, -20.0% idle",
			want:   100,
			wantOK: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseMacOSCPUUsageTotal(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got != tc.want {
				t.Fatalf("cpu: got %.2f want %.2f", got, tc.want)
			}
		})
	}
}

func TestParseMemPressure(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   int
		wantOK bool
	}{
		{name: "29 free", input: "System-wide memory free percentage: 29%", want: 71, wantOK: true},
		{name: "60 free", input: "System-wide memory free percentage: 60%", want: 40, wantOK: true},
		{name: "no match", input: "nothing here", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseMacOSMemPressure(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOK)
			}
			if tc.wantOK && got != tc.want {
				t.Fatalf("pressure: got %d want %d", got, tc.want)
			}
		})
	}
}

func TestParseProcCount(t *testing.T) {
	input := "USER PID %CPU %MEM VSZ RSS TT STAT STARTED TIME COMMAND\nroot 1 0.0 0.0 408 8960 ?? Ss 10AM 0:00.01 /sbin/launchd\nuser 500 0.1 0.2 8000 18000 s001 S 10AM 0:00.05 -zsh\n"
	got := parseProcCount(input)
	if got != 2 {
		t.Fatalf("proc count: got %d want 2", got)
	}
}

func TestParsePsPidPpidRss(t *testing.T) {
	input := "  PID  PPID    RSS\n    1     0   8960\n  410     1  18000\n  516     1   9120\n  517   410   6192\n"
	procs := parsePsPidPpidRss(input)
	if len(procs) != 4 {
		t.Fatalf("proc count: got %d want 4", len(procs))
	}
	if procs[1].PID != 410 || procs[1].PPID != 1 || procs[1].RSS != 18000 {
		t.Fatalf("proc[1]: %+v", procs[1])
	}
	if procs[3].PID != 517 || procs[3].PPID != 410 || procs[3].RSS != 6192 {
		t.Fatalf("proc[3]: %+v", procs[3])
	}
}

func TestBuildChildrenMap(t *testing.T) {
	procs := []procInfo{
		{PID: 1, PPID: 0, RSS: 100},
		{PID: 410, PPID: 1, RSS: 200},
		{PID: 516, PPID: 1, RSS: 300},
		{PID: 517, PPID: 410, RSS: 400},
		{PID: 518, PPID: 410, RSS: 500},
	}
	children := buildChildrenMap(procs)
	if len(children[1]) != 2 {
		t.Fatalf("children of 1: got %d want 2", len(children[1]))
	}
	if len(children[410]) != 2 {
		t.Fatalf("children of 410: got %d want 2", len(children[410]))
	}
}

func TestDescendantRSSSum(t *testing.T) {
	procs := []procInfo{
		{PID: 1, PPID: 0, RSS: 100},
		{PID: 410, PPID: 1, RSS: 200},
		{PID: 516, PPID: 1, RSS: 300},
		{PID: 517, PPID: 410, RSS: 400},
		{PID: 518, PPID: 410, RSS: 500},
		{PID: 600, PPID: 517, RSS: 600},
	}
	children := buildChildrenMap(procs)
	rssByPid := map[int]uint64{}
	for _, p := range procs {
		rssByPid[p.PID] = p.RSS
	}
	// Pane with shell PID 410: should include 410 + 517 + 518 + 600
	got := sumDescendantRSS(410, children, rssByPid)
	want := uint64(200 + 400 + 500 + 600)
	if got != want {
		t.Fatalf("descendant RSS for 410: got %d want %d", got, want)
	}
	// Pane with shell PID 516: only itself
	got = sumDescendantRSS(516, children, rssByPid)
	if got != 300 {
		t.Fatalf("descendant RSS for 516: got %d want 300", got)
	}
}

func TestParseTmuxPanes(t *testing.T) {
	input := "1-lanqiao|1|@1|%1|8501\n1-lanqiao|2|@2|%2|8532\n3-yinchen|1|@6|%8|8855\n"
	panes := parseTmuxPanes(input)
	if len(panes) != 3 {
		t.Fatalf("pane count: got %d want 3", len(panes))
	}
	if panes[0].Session != "1-lanqiao" || panes[0].WindowID != "@1" || panes[0].PaneID != "%1" || panes[0].PanePID != 8501 {
		t.Fatalf("pane[0]: %+v", panes[0])
	}

}

func TestAggregatePaneMemory(t *testing.T) {
	panes := []tmuxPane{
		{Session: "s1", WindowIndex: "1", WindowID: "@1", PaneID: "%1", PanePID: 100},
		{Session: "s1", WindowIndex: "1", WindowID: "@1", PaneID: "%2", PanePID: 200},
		{Session: "s1", WindowIndex: "2", WindowID: "@2", PaneID: "%3", PanePID: 300},
		{Session: "s2", WindowIndex: "1", WindowID: "@3", PaneID: "%4", PanePID: 400},
	}
	procs := []procInfo{
		{PID: 100, PPID: 0, RSS: 1000},
		{PID: 200, PPID: 0, RSS: 2000},
		{PID: 300, PPID: 0, RSS: 3000},
		{PID: 400, PPID: 0, RSS: 4000},
	}
	children := buildChildrenMap(procs)
	rssByPid := map[int]uint64{}
	for _, p := range procs {
		rssByPid[p.PID] = p.RSS
	}
	result := aggregatePaneMemory(panes, children, rssByPid)
	if result.PaneMem["%1"] != 1000 {
		t.Fatalf("pane %%1: got %d want 1000", result.PaneMem["%1"])
	}
	if result.WindowMem["s1:1"] != 3000 {
		t.Fatalf("window s1:1: got %d want 3000", result.WindowMem["s1:1"])
	}
	if result.SessionMem["s1"] != 6000 {
		t.Fatalf("session s1: got %d want 6000", result.SessionMem["s1"])
	}
	if result.TotalMem != 10000 {
		t.Fatalf("total: got %d want 10000", result.TotalMem)
	}
}

func TestFormatMemoryMB(t *testing.T) {
	tests := []struct {
		kb    uint64
		want  string
	}{
		{0, "0M"},
		{512 * 1024, "512M"},
		{1024 * 1024, "1.0G"},
		{2048 * 1024, "2.0G"},
		{1536 * 1024, "1.5G"},
		{500 * 1024, "500M"},
	}
	for _, tc := range tests {
		got := formatMemoryMB(tc.kb)
		if got != tc.want {
			t.Errorf("formatMemoryMB(%d): got %q want %q", tc.kb, got, tc.want)
		}
	}
}

func TestFormatUsagePercent(t *testing.T) {
	tests := []struct {
		val   float64
		want  string
	}{
		{0, "0%"},
		{15, "15%"},
		{97.06, "97%"},
		{3.50, "4%"},
		{3.55, "4%"},
		{65.76, "66%"},
	}
	for _, tc := range tests {
		got := formatUsagePercent(tc.val)
		if got != tc.want {
			t.Errorf("formatUsagePercent(%.2f): got %q want %q", tc.val, got, tc.want)
		}
	}
}

func TestParseLinuxCPUStat(t *testing.T) {
	input := "cpu  3357 0 4313 1362393 234 0 12 0 0 0\ncpu0 1135 0 1578 681196 117 0 6 0 0 0\n"
	total, idle, ok := parseLinuxCPUStat(input)
	if !ok {
		t.Fatal("expected ok")
	}
	// user=3357 nice=0 system=4313 idle=1362393 iowait=234 irq=0 softirq=12 steal=0
	// total = 3357+0+4313+1362393+234+0+12+0 = 1370309
	// idle = idle+iowait = 1362393+234 = 1362627
	if total != 1370309 {
		t.Fatalf("total: got %d want 1370309", total)
	}
	if idle != 1362627 {
		t.Fatalf("idle: got %d want 1362627", idle)
	}
}

func TestComputeLinuxCPUDelta(t *testing.T) {
	// First sample: total=1000, idle=800
	// Second sample: total=1100, idle=850
	// delta_total=100, delta_idle=50
	// usage = (100-50)/100 = 50%
	prevTotal := uint64(1000)
	prevIdle := uint64(800)
	currTotal := uint64(1100)
	currIdle := uint64(850)
	got := computeLinuxCPUPercent(prevTotal, prevIdle, currTotal, currIdle)
	if got != 50 {
		t.Fatalf("cpu delta: got %.2f want 50", got)
	}
}

func TestParseLinuxMemInfo(t *testing.T) {
	input := "MemTotal:       16384000 kB\nMemFree:         1234567 kB\nMemAvailable:    8765432 kB\nBuffers:         123456 kB\n"
	total, avail, ok := parseLinuxMemInfo(input)
	if !ok {
		t.Fatal("expected ok")
	}
	if total != 16384000*1024 {
		t.Fatalf("total: got %d want %d", total, 16384000*1024)
	}
	if avail != 8765432*1024 {
		t.Fatalf("avail: got %d want %d", avail, 8765432*1024)
	}
}

func TestLinuxMemPressurePercent(t *testing.T) {
	// total=16GB, avail=8GB → pressure = 50%
	pressure := linuxMemPressurePercent(16*1024*1024*1024, 8*1024*1024*1024)
	if pressure != 50 {
		t.Fatalf("pressure: got %d want 50", pressure)
	}
	// avail > total → pressure 0
	pressure = linuxMemPressurePercent(16*1024*1024*1024, 20*1024*1024*1024)
	if pressure != 0 {
		t.Fatalf("pressure: got %d want 0", pressure)
	}
}

func TestMemPressureColor(t *testing.T) {
	tests := []struct {
		pressure int
		want     string
	}{
		{20, "#7F9F7F"},
		{30, "#F0DFAF"},
		{59, "#F0DFAF"},
		{60, "#CC9393"},
		{90, "#CC9393"},
	}
	for _, tc := range tests {
		got := MemPressureColor(tc.pressure)
		if got != tc.want {
			t.Errorf("MemPressureColor(%d): got %q want %q", tc.pressure, got, tc.want)
		}
	}
}

func TestCPUBGColor(t *testing.T) {
	tests := []struct {
		val  float64
		want string
	}{
		{20, "#7F9F7F"},
		{30, "#F0DFAF"},
		{59, "#F0DFAF"},
		{60, "#CC9393"},
		{90, "#CC9393"},
	}
	for _, tc := range tests {
		got := CPUBGColor(tc.val)
		if got != tc.want {
			t.Errorf("CPUBGColor(%.0f): got %q want %q", tc.val, got, tc.want)
		}
	}
}
