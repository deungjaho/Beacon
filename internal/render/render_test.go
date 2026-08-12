package render

import (
	"strings"
	"testing"

	"beacon/internal/collector"
	"beacon/internal/state"
)

func TestRenderEmptyStateNoMetrics(t *testing.T) {
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, state.NewState(), collector.Metrics{})
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestRenderAgentWorking(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "running tests", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "running tests") {
		t.Fatalf("missing summary: %q", out)
	}
	if !strings.Contains(out, "#F0DFAF") {
		t.Fatalf("missing working color: %q", out)
	}
	if !strings.Contains(out, "●") {
		t.Fatalf("missing working icon: %q", out)
	}
}

func TestRenderAgentCompleted(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "completed", Summary: "all tests passed", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "all tests passed") {
		t.Fatalf("missing summary: %q", out)
	}
	if !strings.Contains(out, "#7F9F7F") {
		t.Fatalf("missing completed color: %q", out)
	}
	if !strings.Contains(out, "✓") {
		t.Fatalf("missing completed icon: %q", out)
	}
}

func TestRenderAgentWaiting(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "waiting", Summary: "needs input", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "needs input") {
		t.Fatalf("missing summary: %q", out)
	}
	if !strings.Contains(out, "#CC9393") {
		t.Fatalf("missing waiting color: %q", out)
	}
	if !strings.Contains(out, "⚠") {
		t.Fatalf("missing waiting icon: %q", out)
	}
}

func TestRenderAgentBlocked(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "blocked", Summary: "dep down", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "dep down") {
		t.Fatalf("missing summary: %q", out)
	}
	if !strings.Contains(out, "#CC3333") {
		t.Fatalf("missing blocked color: %q", out)
	}
	if !strings.Contains(out, "✗") {
		t.Fatalf("missing blocked icon: %q", out)
	}
}

func TestRenderPreHookFallback(t *testing.T) {
	st := state.NewState()
	args := Args{Width: 160, StatusBG: "black", PaneID: "%9", WindowID: "@9", SessionName: "s", WindowIndex: "1", PaneCommand: "codex"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "codex working") {
		t.Fatalf("missing fallback: %q", out)
	}
	if !strings.Contains(out, "#F0DFAF") {
		t.Fatalf("missing fallback color: %q", out)
	}
}

func TestRenderExplicitStateOverridesFallback(t *testing.T) {
	st := state.NewState()
	st.Panes["%9"] = state.PaneRecord{Status: "waiting", Summary: "needs input", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%9", WindowID: "@1", SessionName: "s", WindowIndex: "1", PaneCommand: "codex"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "needs input") {
		t.Fatalf("missing explicit state: %q", out)
	}
	if strings.Contains(out, "codex working") {
		t.Fatalf("fallback should be replaced: %q", out)
	}
}

func TestRenderCPU(t *testing.T) {
	st := state.NewState()
	m := collector.Metrics{CPUPercent: 45, CPUOK: true}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	if !strings.Contains(out, "\uf4bc") {
		t.Fatalf("missing CPU icon: %q", out)
	}
	if !strings.Contains(out, "45%") {
		t.Fatalf("missing CPU value: %q", out)
	}
	if !strings.Contains(out, "#F0DFAF") {
		t.Fatalf("missing CPU color for 45%%: %q", out)
	}
}

func TestRenderMemPressure(t *testing.T) {
	st := state.NewState()
	m := collector.Metrics{MemPressure: 70, MemPressureOK: true}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	if !strings.Contains(out, "\uf080") {
		t.Fatalf("missing pressure icon: %q", out)
	}
	if !strings.Contains(out, "70%") {
		t.Fatalf("missing pressure value: %q", out)
	}
	if !strings.Contains(out, "#CC9393") {
		t.Fatalf("missing pressure color for 70%%: %q", out)
	}
}

func TestRenderProcCount(t *testing.T) {
	st := state.NewState()
	m := collector.Metrics{ProcCount: 230, ProcCountOK: true}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	if !strings.Contains(out, "\uf46c") {
		t.Fatalf("missing proc icon: %q", out)
	}
	if !strings.Contains(out, "230") {
		t.Fatalf("missing proc count: %q", out)
	}
}

func TestRenderPaneMemory(t *testing.T) {
	st := state.NewState()
	m := collector.Metrics{
		PaneMem:    map[string]string{"%1": "120M"},
		WindowMem:  map[string]string{"s:1": "340M"},
		SessionMem: map[string]string{"s": "1.2G"},
		TotalMem:   "3.4G",
	}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	if !strings.Contains(out, "\ue266") {
		t.Fatalf("missing pane mem icon: %q", out)
	}
	if !strings.Contains(out, "120M") {
		t.Fatalf("missing pane mem value: %q", out)
	}
}

func TestRenderWindowSessionTotalMemory(t *testing.T) {
	st := state.NewState()
	m := collector.Metrics{
		WindowMem:  map[string]string{"s:1": "340M"},
		SessionMem: map[string]string{"s": "1.2G"},
		TotalMem:   "3.4G",
	}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	if !strings.Contains(out, "\U000F05B2") {
		t.Fatalf("missing window mem icon: %q", out)
	}
	if !strings.Contains(out, "340M") {
		t.Fatalf("missing window mem value: %q", out)
	}
	if !strings.Contains(out, "\uebc8") {
		t.Fatalf("missing session mem icon: %q", out)
	}
	if !strings.Contains(out, "1.2G") {
		t.Fatalf("missing session mem value: %q", out)
	}
	if !strings.Contains(out, "\U000F035B") {
		t.Fatalf("missing total mem icon: %q", out)
	}
	if !strings.Contains(out, "3.4G") {
		t.Fatalf("missing total mem value: %q", out)
	}
}

func TestRenderNarrowWidthDropsNonPriority(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		CPUPercent:    50,
		CPUOK:         true,
		MemPressure:   40,
		MemPressureOK: true,
		ProcCount:     200,
		ProcCountOK:   true,
		PaneMem:       map[string]string{"%1": "100M"},
		WindowMem:     map[string]string{"s:1": "200M"},
		SessionMem:    map[string]string{"s": "500M"},
		TotalMem:      "1G",
	}
	// At width 90, agent + pressure + pane mem should be present.
	// CPU, window, session, total, procs may be dropped.
	args := Args{Width: 90, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	if !strings.Contains(out, "busy") {
		t.Fatalf("narrow: missing agent: %q", out)
	}
	if !strings.Contains(out, "\uf080") {
		t.Fatalf("narrow: missing pressure: %q", out)
	}
	if !strings.Contains(out, "100M") {
		t.Fatalf("narrow: missing pane mem: %q", out)
	}
}

func TestRenderMinWidthExitsEmpty(t *testing.T) {
	args := Args{Width: 50, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, state.NewState(), collector.Metrics{})
	if out != "" {
		t.Fatalf("expected empty for narrow width, got %q", out)
	}
}

func TestRenderStatusBGDefault(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "x", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "default", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, collector.Metrics{})
	if !strings.Contains(out, "bg=black") {
		t.Fatalf("expected bg=black for default: %q", out)
	}
}

func TestRenderPowerlineFormat(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "x", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, collector.Metrics{}))

	// Right cap: U+2588 (█) must be the last byte sequence.
	// Use \u escape so the assertion cannot be stripped to an empty string
	// by editors that remove PUA codepoints — the original bug that made
	// this test a no-op.
	capBytes := exactBytes('\u2588')
	if !bytesHasSuffix(out, capBytes) {
		t.Fatalf("expected right cap bytes %x at end, got %x in %q", capBytes, out[len(out)-len(capBytes):], out)
	}

	// Powerline separator: U+E0B2 () must appear at least once.
	// The original test used a literal PUA character that was stripped to
	// "", and strings.Contains(s, "") always returns true, so the check
	// passed even when the renderer emitted no separator at all.  Use \u
	// escape and count occurrences to guard against this.
	sepBytes := exactBytes('\uE0B2')
	sepCount := countBytesOccurrences(out, sepBytes)
	if sepCount < 1 {
		t.Fatalf("expected at least 1 Powerline separator (U+E0B2), got %d in %q", sepCount, out)
	}

	// The separator must sit between two different background colors,
	// not just appear anywhere.  With a single agent segment (bg #F0DFAF)
	// and status-bg black, the first separator transitions black→#F0DFAF.
	boundary := []byte("#[fg=#F0DFAF,bg=black]")
	boundary = append(boundary, sepBytes...)
	boundary = append(boundary, []byte("#[fg=#1d1f21,bg=#F0DFAF")...)
	assertBytesIn(t, "separator-from-statusbg-to-agent", out, boundary)

	// The right cap must transition from the last segment bg back to
	// status-bg.
	capBoundary := []byte("#[fg=#F0DFAF,bg=black]")
	capBoundary = append(capBoundary, capBytes...)
	if !bytesHasSuffix(out, capBoundary) {
		t.Fatalf("expected right cap boundary %q at end, got %q", capBoundary, out[len(out)-len(capBoundary):])
	}
}

func TestRenderAllSegmentsTogether(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		CPUPercent:    45,
		CPUOK:         true,
		MemPressure:   40,
		MemPressureOK: true,
		ProcCount:     230,
		ProcCountOK:   true,
		PaneMem:       map[string]string{"%1": "120M"},
		WindowMem:     map[string]string{"s:1": "340M"},
		SessionMem:    map[string]string{"s": "1.2G"},
		TotalMem:      "3.4G",
	}
	args := Args{Width: 200, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := Render(args, st, m)
	// All segments should be present at wide width
	for _, want := range []string{"busy", "45%", "\uf4bc", "\uf080", "120M", "\U000F05B2", "340M", "\uebc8", "1.2G", "\U000F035B", "3.4G", "\uf46c", "230"} {
		if !strings.Contains(out, want) {
			t.Fatalf("wide render missing %q: %q", want, out)
		}
	}
}

// ---- Exact byte-level icon and Powerline structure tests ----
//
// These tests verify that the renderer emits the exact UTF-8 byte sequences
// for each icon, the Powerline separator between segments with different
// background colors, and the terminal right cap.  They use raw byte
// comparisons rather than string Contains to guard against codepoint
// corruption during file writes.

// exactBytes returns the UTF-8 encoding of a rune.
func exactBytes(r rune) []byte { return []byte(string(r)) }

// assertBytesIn checks that needle appears as a substring of haystack at the
// byte level.
func assertBytesIn(t *testing.T, name string, haystack []byte, needle []byte) {
	t.Helper()
	if !bytesContains(haystack, needle) {
		t.Errorf("%s: expected bytes %x (%q) in output, not found", name, needle, needle)
	}
}

// bytesContains is a byte-level substring search.
func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if bytesEqual(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// countBytesOccurrences counts non-overlapping occurrences of needle in haystack.
func countBytesOccurrences(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	count := 0
	i := 0
	for i <= len(haystack)-len(needle) {
		if bytesEqual(haystack[i:i+len(needle)], needle) {
			count++
			i += len(needle)
		} else {
			i++
		}
	}
	return count
}

func TestRenderExactIconBytes(t *testing.T) {
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		CPUPercent:    45,
		CPUOK:         true,
		MemPressure:   40,
		MemPressureOK: true,
		ProcCount:     230,
		ProcCountOK:   true,
		PaneMem:       map[string]string{"%1": "120M"},
		WindowMem:     map[string]string{"s:1": "340M"},
		SessionMem:    map[string]string{"s": "1.2G"},
		TotalMem:      "3.4G",
	}
	args := Args{Width: 200, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, m))

	// Each icon must appear as its exact UTF-8 byte sequence.
	// Agent working: ● U+25CF = e2 97 8f
	assertBytesIn(t, "agent-working-icon", out, exactBytes('\u25CF'))
	// CPU: U+F4BC = ef 92 bc
	assertBytesIn(t, "cpu-icon", out, exactBytes('\uF4BC'))
	// Memory pressure: U+F080 = ef 82 80
	assertBytesIn(t, "pressure-icon", out, exactBytes('\uF080'))
	// Pane memory: U+E266 = ee 89 a6
	assertBytesIn(t, "pane-mem-icon", out, exactBytes('\uE266'))
	// Window memory: U+F05B2 = f3 b0 96 b2
	assertBytesIn(t, "window-mem-icon", out, exactBytes('\U000F05B2'))
	// Session memory: U+EBC8 = ee af 88
	assertBytesIn(t, "session-mem-icon", out, exactBytes('\uEBC8'))
	// Total tmux memory: U+F035B = f3 b0 8d 9b
	assertBytesIn(t, "total-mem-icon", out, exactBytes('\U000F035B'))
	// Process count: U+F46C = ef 91 ac
	assertBytesIn(t, "proc-count-icon", out, exactBytes('\uF46C'))
}

func TestRenderExactAgentStateIcons(t *testing.T) {
	cases := []struct {
		name   string
		status string
		icon   rune
		bg     string
	}{
		{"working", "working", '\u25CF', "#F0DFAF"},
		{"completed", "completed", '\u2713', "#7F9F7F"},
		{"waiting", "waiting", '\u26A0', "#CC9393"},
		{"blocked", "blocked", '\u2717', "#CC3333"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := state.NewState()
			st.Panes["%1"] = state.PaneRecord{Status: tc.status, Summary: "x", Window: "@1", Session: "s", Time: 100}
			args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
			out := []byte(Render(args, st, collector.Metrics{}))
			assertBytesIn(t, tc.name+"-icon", out, exactBytes(tc.icon))
			if !strings.Contains(string(out), tc.bg) {
				t.Errorf("%s: missing bg color %s in %q", tc.name, tc.bg, out)
			}
		})
	}
}

func TestRenderExactPowerlineSeparatorBytes(t *testing.T) {
	// The Powerline separator  is U+E0B2 = ee 82 b2.
	// It must appear before every segment, transitioning from the previous
	// segment's bg to the current segment's bg.
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		CPUPercent:    45,
		CPUOK:         true,
		MemPressure:   40,
		MemPressureOK: true,
		ProcCount:     230,
		ProcCountOK:   true,
		PaneMem:       map[string]string{"%1": "120M"},
		WindowMem:     map[string]string{"s:1": "340M"},
		SessionMem:    map[string]string{"s": "1.2G"},
		TotalMem:      "3.4G",
	}
	args := Args{Width: 200, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, m))

	sep := exactBytes('\uE0B2')
	// With 8 segments (agent, pressure, pane-mem, cpu, window-mem,
	// session-mem, total-mem, proc-count), the separator must appear at
	// least 8 times — once before each segment.
	count := countBytesOccurrences(out, sep)
	if count < 8 {
		t.Errorf("expected at least 8 Powerline separators, got %d in %q", count, out)
	}
}

func TestRenderPowerlineSeparatorBetweenDifferentBackgrounds(t *testing.T) {
	// Verify that the separator appears at the boundary between two segments
	// with different background colors.  We construct a scenario with an
	// agent segment (bg #F0DFAF) followed by a pressure segment at 20%
	// (bg #7F9F7F green) and check that the separator bytes sit between
	// the two bg values in the tmux format string.
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		MemPressure:   20,
		MemPressureOK: true,
	}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, m))

	// The format is: #[fg=<curBG>,bg=<prevBG>]<sep>#[fg=<FG>,bg=<curBG>...
	// Between agent (#F0DFAF) and pressure at 20% (#7F9F7F):
	//   ...#[fg=#7F9F7F,bg=#F0DFAF]<sep>#[fg=#1d1f21,bg=#7F9F7F...
	sep := exactBytes('\uE0B2')
	boundary := []byte("#7F9F7F,bg=#F0DFAF]")
	boundary = append(boundary, sep...)
	boundary = append(boundary, []byte("#[fg=#1d1f21,bg=#7F9F7F")...)
	assertBytesIn(t, "separator-between-agent-and-pressure", out, boundary)

	// Also verify the first separator: from status-bg (black) to agent (#F0DFAF).
	firstBoundary := []byte("#[fg=#F0DFAF,bg=black]")
	firstBoundary = append(firstBoundary, sep...)
	firstBoundary = append(firstBoundary, []byte("#[fg=#1d1f21,bg=#F0DFAF")...)
	assertBytesIn(t, "separator-from-statusbg-to-agent", out, firstBoundary)
}

func TestRenderExactRightCapBytes(t *testing.T) {
	// The terminal right cap █ is U+2588 = e2 96 88.
	// It must be the last non-format-string bytes of the output.
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "x", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, collector.Metrics{}))

	cap := exactBytes('\u2588')
	if !bytesHasSuffix(out, cap) {
		t.Errorf("expected output to end with right cap bytes %x, got suffix %x", cap, out[len(out)-len(cap):])
	}
}

func bytesHasSuffix(s, suffix []byte) bool {
	if len(suffix) > len(s) {
		return false
	}
	return bytesEqual(s[len(s)-len(suffix):], suffix)
}

func TestRenderPowerlineSeparatorBetweenPressureAndPaneMem(t *testing.T) {
	// Verify separator between pressure at 20% (bg #7F9F7F green) and
	// pane-mem (bg #7CB8BB).
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		MemPressure:   20,
		MemPressureOK: true,
		PaneMem:       map[string]string{"%1": "120M"},
	}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, m))

	sep := exactBytes('\uE0B2')
	// #[fg=#7CB8BB,bg=#7F9F7F]<sep>#[fg=#1d1f21,bg=#7CB8BB
	boundary := []byte("#7CB8BB,bg=#7F9F7F]")
	boundary = append(boundary, sep...)
	boundary = append(boundary, []byte("#[fg=#1d1f21,bg=#7CB8BB")...)
	assertBytesIn(t, "separator-between-pressure-and-panemem", out, boundary)
}

func TestRenderPowerlineSeparatorBetweenCPUAndWindowMem(t *testing.T) {
	// Verify separator between CPU (bg #F0DFAF for 45%) and window-mem (bg #5A8A8A).
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		CPUPercent: 45,
		CPUOK:      true,
		WindowMem:  map[string]string{"s:1": "340M"},
	}
	args := Args{Width: 200, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, m))

	sep := exactBytes('\uE0B2')
	// CPU at 45% uses bg #F0DFAF.  Window mem uses bg #5A8A8A.
	// #[fg=#5A8A8A,bg=#F0DFAF]<sep>#[fg=#F4F4E6,bg=#5A8A8A
	boundary := []byte("#5A8A8A,bg=#F0DFAF]")
	boundary = append(boundary, sep...)
	boundary = append(boundary, []byte("#[fg=#F4F4E6,bg=#5A8A8A")...)
	assertBytesIn(t, "separator-between-cpu-and-windowmem", out, boundary)
}

func TestRenderPowerlineSeparatorBetweenTotalMemAndProcCount(t *testing.T) {
	// Verify separator between total-mem (bg #3A6A6A) and proc-count (bg #B48EAD).
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "busy", Window: "@1", Session: "s", Time: 100}
	m := collector.Metrics{
		TotalMem:    "3.4G",
		ProcCount:   230,
		ProcCountOK: true,
	}
	args := Args{Width: 200, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, m))

	sep := exactBytes('\uE0B2')
	// #[fg=#B48EAD,bg=#3A6A6A]<sep>#[fg=#1d1f21,bg=#B48EAD
	boundary := []byte("#B48EAD,bg=#3A6A6A]")
	boundary = append(boundary, sep...)
	boundary = append(boundary, []byte("#[fg=#1d1f21,bg=#B48EAD")...)
	assertBytesIn(t, "separator-between-totalmem-and-proccount", out, boundary)
}

func TestRenderRightCapFromLastSegmentToStatusBG(t *testing.T) {
	// The right cap transitions from the last segment's bg to the status bg.
	// With only an agent segment (bg #F0DFAF) and status-bg black:
	//   #[fg=#F0DFAF,bg=black]<cap>
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "x", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, collector.Metrics{}))

	cap := exactBytes('\u2588')
	suffix := []byte("#[fg=#F0DFAF,bg=black]")
	suffix = append(suffix, cap...)
	if !bytesHasSuffix(out, suffix) {
		t.Errorf("expected output to end with %q, got suffix %q", suffix, out[len(out)-len(suffix):])
	}
}

func TestRenderNoSeparatorWhenSingleSegment(t *testing.T) {
	// With only one segment, there should be exactly one separator (before
	// the segment, transitioning from status-bg to the segment bg) and one
	// right cap (transitioning back to status-bg).  No inter-segment
	// separators.
	st := state.NewState()
	st.Panes["%1"] = state.PaneRecord{Status: "working", Summary: "x", Window: "@1", Session: "s", Time: 100}
	args := Args{Width: 160, StatusBG: "black", PaneID: "%1", WindowID: "@1", SessionName: "s", WindowIndex: "1"}
	out := []byte(Render(args, st, collector.Metrics{}))

	sep := exactBytes('\uE0B2')
	count := countBytesOccurrences(out, sep)
	if count != 1 {
		t.Errorf("single segment: expected exactly 1 separator, got %d in %q", count, out)
	}
	cap := exactBytes('\u2588')
	capCount := countBytesOccurrences(out, cap)
	if capCount != 1 {
		t.Errorf("single segment: expected exactly 1 right cap, got %d in %q", capCount, out)
	}
}
