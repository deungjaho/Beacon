// Package render assembles the tmux status-right string from host metrics.
// It performs no subprocess calls and no file writes; it is purely a
// read-and-format operation designed to complete in under 5ms.
//
// Agent status (working/waiting/blocked/completed) is NOT shown in
// status-right. Instead, unacknowledged notifications display as a 🔔
// bell in the session/window/pane name (handled by the dotfile tmux
// status scripts). This renderer shows only resource metrics in a
// strict order: CPU, memory pressure, process count, pane memory,
// window memory, session memory, total tmux memory.
package render

import (
	"fmt"
	"strings"

	"beacon/internal/collector"
)

// Args holds the tmux context passed to status-tmux.
type Args struct {
	Width       int
	StatusBG    string
	SessionName string
	WindowIndex string
	PaneID      string
	WindowID    string
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
	// Strict display order is CPU, pressure, proc count, pane mem, window mem,
	// session mem, total mem. Priority determines what gets dropped first when
	// width is narrow — total mem drops first, CPU drops last.
	prioCPU        = 0
	prioPressure   = 1
	prioProcCount  = 2
	prioPaneMem    = 3
	prioWindowMem  = 4
	prioSessionMem = 5
	prioTotalMem   = 6
)

// Render produces the final tmux status-right string.
func Render(args Args, m collector.Metrics) string {
	if args.Width > 0 && args.Width < minWidth {
		return ""
	}
	statusBG := args.StatusBG
	if statusBG == "" || statusBG == "default" {
		statusBG = "black"
	}

	segments := buildSegments(args, m)
	if len(segments) == 0 {
		return ""
	}
	segments = trimToWidth(segments, args.Width)
	if len(segments) == 0 {
		return ""
	}
	return formatPowerline(statusBG, segments)
}

// buildSegments builds resource-only segments in strict order:
// CPU, memory pressure, process count, pane memory, window memory,
// session memory, total tmux memory.
func buildSegments(args Args, m collector.Metrics) []Segment {
	var segs []Segment

	// 1. CPU
	if m.CPUOK {
		segs = append(segs, Segment{
			FG:       "#1d1f21",
			BG:       collector.CPUBGColor(m.CPUPercent),
			Text:     fmt.Sprintf(" %s %s ", iconCPU, collector.FormatUsagePercent(m.CPUPercent)),
			Bold:     true,
			Priority: prioCPU,
		})
	}

	// 2. Memory pressure
	if m.MemPressureOK {
		segs = append(segs, Segment{
			FG:       "#1d1f21",
			BG:       collector.MemPressureColor(m.MemPressure),
			Text:     fmt.Sprintf(" %s %d%% ", iconMemPressure, m.MemPressure),
			Bold:     true,
			Priority: prioPressure,
		})
	}

	// 3. Process count
	if m.ProcCountOK {
		segs = append(segs, Segment{
			FG:       "#1d1f21",
			BG:       "#B48EAD",
			Text:     fmt.Sprintf(" %s %d ", iconProcCount, m.ProcCount),
			Bold:     true,
			Priority: prioProcCount,
		})
	}

	// 4. Pane memory
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

	// 5. Window memory
	wkey := args.SessionName + ":" + args.WindowIndex
	if v, ok := m.WindowMem[wkey]; ok && v != "" {
		segs = append(segs, Segment{
			FG:       "#F4F4E6",
			BG:       "#5A8A8A",
			Text:     fmt.Sprintf(" %s %s ", iconWindowMem, v),
			Priority: prioWindowMem,
		})
	}

	// 6. Session memory
	if v, ok := m.SessionMem[args.SessionName]; ok && v != "" {
		segs = append(segs, Segment{
			FG:       "#F4F4E6",
			BG:       "#4A7A7A",
			Text:     fmt.Sprintf(" %s %s ", iconSessionMem, v),
			Priority: prioSessionMem,
		})
	}

	// 7. Total tmux memory
	if m.TotalMem != "" {
		segs = append(segs, Segment{
			FG:       "#F4F4E6",
			BG:       "#3A6A6A",
			Text:     fmt.Sprintf(" %s %s ", iconTotalMem, m.TotalMem),
			Priority: prioTotalMem,
		})
	}

	return segs
}

// trimToWidth drops the lowest-priority segments until the estimated display
// width fits.
func trimToWidth(segs []Segment, width int) []Segment {
	if width <= 0 {
		return segs
	}
	maxChars := width / 2
	total := 0
	for _, s := range segs {
		total += visibleLen(s.Text)
	}
	if total <= maxChars {
		return segs
	}
	sorted := make([]Segment, len(segs))
	copy(sorted, segs)
	for i := 0; i < len(sorted); i++ {
		minIdx := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Priority < sorted[minIdx].Priority {
				minIdx = j
			}
		}
		sorted[i], sorted[minIdx] = sorted[minIdx], sorted[i]
	}
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
	return kept
}

func visibleLen(s string) int {
	clean := stripTmuxEscapes(s)
	return len([]rune(clean))
}

func stripTmuxEscapes(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '[' {
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
