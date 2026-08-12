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
		PaneMem:  map[string]string{"%1": "120M"},
		WindowMem: map[string]string{"s:1": "340M"},
		SessionMem: map[string]string{"s": "1.2G"},
		TotalMem: "3.4G",
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
	out := Render(args, st, collector.Metrics{})
	// Should end with the right cap
	if !strings.HasSuffix(out, "█") {
		t.Fatalf("expected right cap suffix: %q", out)
	}
	// Should contain the separator
	if !strings.Contains(out, "\ue0b2") {
		t.Fatalf("expected separator: %q", out)
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
