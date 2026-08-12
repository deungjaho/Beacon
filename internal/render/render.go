// Package render assembles the tmux status-right string from agent state and
// host metrics. It performs no subprocess calls and no file writes; it is
// purely a read-and-format operation designed to complete in under 5ms.
package render

import (
	"fmt"
	"strings"

	"beacon/internal/collector"
	"beacon/internal/state"
)

// Args holds the tmux context passed to status-tmux.
type Args struct {
	Width       int
	StatusBG    string
	SessionName string
	WindowIndex string
	PaneID      string
	WindowID    string
	PaneCommand string // from tmux display-message, for pre-hook fallback
}

// Segment is a single Powerline segment.
type Segment struct {
	FG       string
	BG       string
	Text     string
	Bold     bool
	Priority int
}

const (
	iconCPU         = "\uf4bc"
	iconPaneMem     = "\ue266"
	iconWindowMem   = "\U000F05B2"
	iconSessionMem  = "\uebc8"
	iconTotalMem    = "\U000F035B"
	iconProcCount   = "\uf46c"
	iconMemPressure = "\uf080"

	minWidth = 80

	// Priority order for narrow-width trimming. Higher priority = kept longer.
	prioAgent      = 0
	prioPressure   = 1
	prioPaneMem    = 2
	prioCPU        = 3
	prioWindowMem  = 4
	prioSessionMem = 5
	prioTotalMem   = 6
	prioProcCount  = 7
)

var agentCommands = map[string]bool{
	"codex": true, "claude": true, "devin": true, "opencode": true,
	"op": true, "agy": true, "gemini": true,
}

// Render produces the final tmux status-right string.
func Render(args Args, st *state.State, m collector.Metrics) string {
	if args.Width > 0 && args.Width < minWidth {
		return ""
	}
	statusBG := args.StatusBG
	if statusBG == "" || statusBG == "default" {
		statusBG = "black"
	}

	segments := buildSegments(args, st, m)
	if len(segments) == 0 {
		return ""
	}
	segments = trimToWidth(segments, args.Width)
	if len(segments) == 0 {
		return ""
	}
	return formatPowerline(statusBG, segments)
}

func buildSegments(args Args, st *state.State, m collector.Metrics) []Segment {
	var segs []Segment

	// Agent status (highest priority)
	agentSegs := buildAgentSegments(args, st)
	segs = append(segs, agentSegs...)

	// Memory pressure
	if m.MemPressureOK {
		segs = append(segs, Segment{
			FG:       "#1d1f21",
			BG:       collector.MemPressureColor(m.MemPressure),
			Text:     fmt.Sprintf(" %s %d%% ", iconMemPressure, m.MemPressure),
			Bold:     true,
			Priority: prioPressure,
		})
	}

	// Pane memory
	if args.PaneID != "" {
		if v, ok := m.PaneMem[args.PaneID]; ok && v != "" {
			segs = append(segs, Segment{
				FG:       "#1d1f21",
				BG:       "#7CB8BB",
				Text:     fmt.Sprintf(" %s %s ", iconPaneMem, v),
				Priority: prioPaneMem,
			})
		}
	}

	// CPU
	if m.CPUOK {
		segs = append(segs, Segment{
			FG:       "#1d1f21",
			BG:       collector.CPUBGColor(m.CPUPercent),
			Text:     fmt.Sprintf(" %s %s ", iconCPU, collector.FormatUsagePercent(m.CPUPercent)),
			Bold:     true,
			Priority: prioCPU,
		})
	}

	// Window memory
	wkey := args.SessionName + ":" + args.WindowIndex
	if v, ok := m.WindowMem[wkey]; ok && v != "" {
		segs = append(segs, Segment{
			FG:       "#F4F4E6",
			BG:       "#5A8A8A",
			Text:     fmt.Sprintf(" %s %s ", iconWindowMem, v),
			Priority: prioWindowMem,
		})
	}

	// Session memory
	if v, ok := m.SessionMem[args.SessionName]; ok && v != "" {
		segs = append(segs, Segment{
			FG:       "#F4F4E6",
			BG:       "#4A7A7A",
			Text:     fmt.Sprintf(" %s %s ", iconSessionMem, v),
			Priority: prioSessionMem,
		})
	}

	// Total tmux memory
	if m.TotalMem != "" {
		segs = append(segs, Segment{
			FG:       "#F4F4E6",
			BG:       "#3A6A6A",
			Text:     fmt.Sprintf(" %s %s ", iconTotalMem, m.TotalMem),
			Priority: prioTotalMem,
		})
	}

	// Process count
	if m.ProcCountOK {
		segs = append(segs, Segment{
			FG:       "#1d1f21",
			BG:       "#B48EAD",
			Text:     fmt.Sprintf(" %s %d ", iconProcCount, m.ProcCount),
			Bold:     true,
			Priority: prioProcCount,
		})
	}

	return segs
}

func buildAgentSegments(args Args, st *state.State) []Segment {
	var segs []Segment
	// Collect panes in the current window with explicit Beacon records.
	if st != nil && args.WindowID != "" {
		for _, rec := range st.Panes {
			if rec.Window != args.WindowID {
				continue
			}
			seg, ok := agentSegment(rec.Status, rec.Summary)
			if !ok {
				continue
			}
			seg.Priority = prioAgent
			segs = append(segs, seg)
		}
	}
	// Pre-hook fallback: if the current pane has no explicit record, infer
	// from the pane command. This mirrors e9a544b's behavior.
	if len(segs) == 0 && args.PaneID != "" && args.PaneCommand != "" {
		if st == nil || st.Panes == nil {
			return segs
		}
		if _, hasRecord := st.Panes[args.PaneID]; !hasRecord {
			cmd := baseCommand(args.PaneCommand)
			if agentCommands[cmd] {
				segs = append(segs, Segment{
					FG:       "#1d1f21",
					BG:       "#F0DFAF",
					Text:     fmt.Sprintf(" ● %s working ", cmd),
					Bold:     false,
					Priority: prioAgent,
				})
			}
		}
	}
	return segs
}

func agentSegment(status, summary string) (Segment, bool) {
	trunc := truncateSummary(summary, 30)
	switch status {
	case "working":
		return Segment{FG: "#1d1f21", BG: "#F0DFAF", Text: fmt.Sprintf(" ● %s ", trunc)}, true
	case "completed":
		return Segment{FG: "#1d1f21", BG: "#7F9F7F", Text: fmt.Sprintf(" ✓ %s ", trunc)}, true
	case "waiting":
		return Segment{FG: "#1d1f21", BG: "#CC9393", Text: fmt.Sprintf(" ⚠ %s ", trunc)}, true
	case "blocked":
		return Segment{FG: "#ffffff", BG: "#CC3333", Text: fmt.Sprintf(" ✗ %s ", trunc)}, true
	}
	return Segment{}, false
}

func baseCommand(cmd string) string {
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		return cmd[idx+1:]
	}
	return cmd
}

func truncateSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// trimToWidth drops the lowest-priority segments until the estimated display
// width fits. Agent segments are never dropped if any can be shown.
func trimToWidth(segs []Segment, width int) []Segment {
	if width <= 0 {
		return segs
	}
	// Estimate: each segment averages ~20 visible chars. This is a heuristic;
	// tmux will do the real truncation. We just need to avoid status-left
	// being pushed off.
	maxChars := width / 2 // status-right gets roughly half the width
	total := 0
	for _, s := range segs {
		total += visibleLen(s.Text)
	}
	if total <= maxChars {
		return segs
	}
	// Sort by priority (stable) and drop from the end.
	sorted := make([]Segment, len(segs))
	copy(sorted, segs)
	// Simple selection sort by priority (stable for equal priorities).
	for i := 0; i < len(sorted); i++ {
		minIdx := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Priority < sorted[minIdx].Priority {
				minIdx = j
			}
		}
		sorted[i], sorted[minIdx] = sorted[minIdx], sorted[i]
	}
	// Keep highest-priority segments until we fit.
	var kept []Segment
	total = 0
	for _, s := range sorted {
		ln := visibleLen(s.Text)
		if total+ln > maxChars && len(kept) > 0 {
			continue
		}
		kept = append(kept, s)
		total += ln
	}
	// Re-sort kept by original priority order (they were sorted by priority,
	// so they're already in priority order).
	return kept
}

func visibleLen(s string) int {
	// Count runes, ignoring tmux escape sequences.
	clean := stripTmuxEscapes(s)
	return len([]rune(clean))
}

func stripTmuxEscapes(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until ']'
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				break
			}
			i += end + 1
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func formatPowerline(statusBG string, segs []Segment) string {
	const separator = "\ue0b2"
	const rightCap = "█"
	prevBG := statusBG
	var b strings.Builder
	for _, seg := range segs {
		b.WriteString(fmt.Sprintf("#[fg=%s,bg=%s]%s#[fg=%s,bg=%s", seg.BG, prevBG, separator, seg.FG, seg.BG))
		if seg.Bold {
			b.WriteString(",bold")
		}
		b.WriteString("]")
		b.WriteString(seg.Text)
		prevBG = seg.BG
	}
	b.WriteString(fmt.Sprintf(" #[fg=%s,bg=%s]%s", prevBG, statusBG, rightCap))
	return b.String()
}
