// Package collector implements bounded host and tmux resource sampling.
// It never blocks the caller: all commands have timeouts and all parsers
// degrade safely on missing or malformed input.
package collector

import (
	"bufio"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Metrics is the full resource snapshot written by the daemon.
type Metrics struct {
	SampledAt     int64             `json:"sampled_at"`
	CPUPercent    float64           `json:"cpu_percent"`
	CPUOK         bool              `json:"cpu_ok"`
	MemPressure   int               `json:"mem_pressure"`
	MemPressureOK bool              `json:"mem_pressure_ok"`
	ProcCount     int               `json:"proc_count"`
	ProcCountOK   bool              `json:"proc_count_ok"`
	PaneMemKB     map[string]uint64 `json:"pane_mem_kb"`
	WindowMemKB   map[string]uint64 `json:"window_mem_kb"`
	SessionMemKB  map[string]uint64 `json:"session_mem_kb"`
	TotalMemKB    uint64            `json:"total_mem_kb"`
	PaneMem       map[string]string `json:"pane_mem"`
	WindowMem     map[string]string `json:"window_mem"`
	SessionMem    map[string]string `json:"session_mem"`
	TotalMem      string            `json:"total_mem"`
	PaneCommands  map[string]string `json:"pane_commands"`
}

// tmuxPane is a single pane entry from tmux list-panes.
type tmuxPane struct {
	Session     string
	WindowIndex string
	WindowID    string
	PaneID      string
	PanePID     int
	Command     string
}

// procInfo is a single process entry from ps.
type procInfo struct {
	PID  int
	PPID int
	RSS  uint64 // KB
}

// PaneMemoryResult holds the aggregated memory maps.
type paneMemoryResult struct {
	PaneMem    map[string]uint64
	WindowMem  map[string]uint64
	SessionMem map[string]uint64
	TotalMem   uint64
}

var cpuUsagePattern = regexp.MustCompile(`CPU usage:\s*([0-9.]+)% user,\s*([0-9.]+)% sys,`)
var memPressurePattern = regexp.MustCompile(`System-wide memory free percentage:\s*(\d+)%`)

// commandTimeout is the max wait for any subprocess.
const commandTimeout = 5 * time.Second

// runCommand runs a command with a bounded timeout and returns its stdout.
func runCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	timer := time.AfterFunc(commandTimeout, func() {
		_ = cmd.Process.Kill()
	})
	defer timer.Stop()
	return cmd.Output()
}

// parseMacOSCPUUsageTotal extracts user+sys from top output.
func parseMacOSCPUUsageTotal(output string) (float64, bool) {
	matches := cpuUsagePattern.FindStringSubmatch(output)
	if len(matches) != 3 {
		return 0, false
	}
	user, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}
	system, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return 0, false
	}
	total := user + system
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}
	return total, true
}

// parseMacOSMemPressure extracts the free percentage and returns pressure (100 - free).
func parseMacOSMemPressure(output string) (int, bool) {
	matches := memPressurePattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return 0, false
	}
	free, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}
	pressure := 100 - free
	if pressure < 0 {
		pressure = 0
	}
	if pressure > 100 {
		pressure = 100
	}
	return pressure, true
}

// parseProcCount counts non-header lines from ps aux.
func parseProcCount(output string) int {
	scanner := bufio.NewScanner(strings.NewReader(output))
	count := 0
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			count++
		}
	}
	return count
}

// parsePsPidPpidRss parses `ps -eo pid,ppid,rss` output.
func parsePsPidPpidRss(output string) []procInfo {
	var procs []procInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		rss, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			continue
		}
		procs = append(procs, procInfo{PID: pid, PPID: ppid, RSS: rss})
	}
	return procs
}

// buildChildrenMap builds a parent → children map from process list.
func buildChildrenMap(procs []procInfo) map[int][]int {
	children := make(map[int][]int, len(procs))
	for _, p := range procs {
		children[p.PPID] = append(children[p.PPID], p.PID)
	}
	return children
}

// sumDescendantRSS sums RSS of the given root PID and all its descendants.
// Each process is counted at most once.
func sumDescendantRSS(rootPID int, children map[int][]int, rssByPid map[int]uint64) uint64 {
	visited := map[int]bool{rootPID: true}
	var total uint64
	if rss, ok := rssByPid[rootPID]; ok {
		total += rss
	}
	stack := []int{rootPID}
	for len(stack) > 0 {
		parent := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range children[parent] {
			if visited[child] {
				continue
			}
			visited[child] = true
			total += rssByPid[child]
			stack = append(stack, child)
		}
	}
	return total
}

// parseTmuxPanes parses `tmux list-panes -a -F` output with `|` separator.
func parseTmuxPanes(output string) []tmuxPane {
	var panes []tmuxPane
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		pid, err := strconv.Atoi(parts[4])
		if err != nil {
			continue
		}
		panes = append(panes, tmuxPane{
			Session:     parts[0],
			WindowIndex: parts[1],
			WindowID:    parts[2],
			PaneID:      parts[3],
			PanePID:     pid,
		})
	}
	return panes
}

// parseTmuxPanesWithCommand parses list-panes output that includes pane_current_command.
func parseTmuxPanesWithCommand(output string) []tmuxPane {
	var panes []tmuxPane
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		pid, err := strconv.Atoi(parts[4])
		if err != nil {
			continue
		}
		cmd := ""
		if len(parts) >= 6 {
			cmd = parts[5]
		}
		panes = append(panes, tmuxPane{
			Session:     parts[0],
			WindowIndex: parts[1],
			WindowID:    parts[2],
			PaneID:      parts[3],
			PanePID:     pid,
			Command:     cmd,
		})
	}
	return panes
}

// aggregatePaneMemory walks each pane's process tree and aggregates RSS.
// Each process is counted under exactly one pane (the one whose tree contains it).
func aggregatePaneMemory(panes []tmuxPane, children map[int][]int, rssByPid map[int]uint64) paneMemoryResult {
	result := paneMemoryResult{
		PaneMem:    map[string]uint64{},
		WindowMem:  map[string]uint64{},
		SessionMem: map[string]uint64{},
	}
	// Track which processes have been claimed by a pane to avoid double-counting
	// in case of PID reuse or shared process trees.
	claimed := map[int]bool{}
	for _, pane := range panes {
		total := sumDescendantRSSClaimed(pane.PanePID, children, rssByPid, claimed)
		result.PaneMem[pane.PaneID] = total
		wkey := pane.Session + ":" + pane.WindowIndex
		result.WindowMem[wkey] += total
		result.SessionMem[pane.Session] += total
		result.TotalMem += total
	}
	return result
}

// sumDescendantRSSClaimed is like sumDescendantRSS but skips processes already
// claimed by another pane.
func sumDescendantRSSClaimed(rootPID int, children map[int][]int, rssByPid map[int]uint64, claimed map[int]bool) uint64 {
	if claimed[rootPID] {
		return 0
	}
	visited := map[int]bool{rootPID: true}
	claimed[rootPID] = true
	var total uint64
	if rss, ok := rssByPid[rootPID]; ok {
		total += rss
	}
	stack := []int{rootPID}
	for len(stack) > 0 {
		parent := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range children[parent] {
			if visited[child] || claimed[child] {
				continue
			}
			visited[child] = true
			claimed[child] = true
			total += rssByPid[child]
			stack = append(stack, child)
		}
	}
	return total
}

// formatMemoryMB converts KB to a human-readable string (M or G).
func formatMemoryMB(kb uint64) string {
	mb := float64(kb) / 1024.0
	if mb >= 1024 {
		return strconv.FormatFloat(mb/1024.0, 'f', 1, 64) + "G"
	}
	return strconv.FormatFloat(mb, 'f', 0, 64) + "M"
}

// formatUsagePercent formats a CPU percentage, rounding to whole unless < 10
// and has a fractional part.
func formatUsagePercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 0, 64) + "%"
}

// CPUBGColor returns the background color for a CPU percentage.
func CPUBGColor(val float64) string {
	if val >= 60 {
		return "#CC9393"
	}
	if val >= 30 {
		return "#F0DFAF"
	}
	return "#7F9F7F"
}

// MemPressureColor returns the background color for a memory pressure percentage.
func MemPressureColor(pressure int) string {
	if pressure >= 60 {
		return "#CC9393"
	}
	if pressure >= 30 {
		return "#F0DFAF"
	}
	return "#7F9F7F"
}

// FormatUsagePercent formats a CPU percentage string.
func FormatUsagePercent(value float64) string {
	return formatUsagePercent(value)
}

// --- Linux parsers ---

// parseLinuxCPUStat parses /proc/stat first cpu line.
// Returns (total, idle, ok).
func parseLinuxCPUStat(output string) (uint64, uint64, bool) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, false
		}
		// fields[0] = "cpu", fields[1..] = user nice system idle iowait irq softirq steal
		var total, idle uint64
		for i, f := range fields[1:] {
			val, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			total += val
			if i == 3 || i == 4 { // idle, iowait
				idle += val
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

// computeLinuxCPUPercent computes CPU usage percentage between two samples.
func computeLinuxCPUPercent(prevTotal, prevIdle, currTotal, currIdle uint64) float64 {
	dt := currTotal - prevTotal
	di := currIdle - prevIdle
	if dt == 0 {
		return 0
	}
	pct := float64(dt-di) / float64(dt) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// parseLinuxMemInfo parses /proc/meminfo and returns (totalBytes, availableBytes, ok).
func parseLinuxMemInfo(output string) (uint64, uint64, bool) {
	var total, avail uint64
	var totalOK, availOK bool
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMemInfoKB(line)
			totalOK = total > 0
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = parseMemInfoKB(line)
			availOK = true
		}
	}
	if !totalOK || !availOK {
		return 0, 0, false
	}
	return total, avail, true
}

func parseMemInfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	val, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return val * 1024
}

// linuxMemPressurePercent computes pressure from total and available bytes.
func linuxMemPressurePercent(total, avail uint64) int {
	if total == 0 {
		return 0
	}
	if avail > total {
		avail = total
	}
	used := total - avail
	pct := used * 100 / total
	return int(pct)
}

// --- Collector ---

// Collector samples host and tmux metrics. It is safe for concurrent use.
type Collector struct {
	tmuxBin       string
	os            string
	prevLinuxCPU  uint64
	prevLinuxIdle uint64
	mu            sync.Mutex
}

// NewCollector creates a Collector for the given OS ("darwin" or "linux").
func NewCollector(osName, tmuxBin string) *Collector {
	return &Collector{os: osName, tmuxBin: tmuxBin}
}

// Sample collects all metrics in a single pass. It never panics and never
// blocks longer than commandTimeout per command.
func (c *Collector) Sample() Metrics {
	m := Metrics{
		SampledAt:    time.Now().Unix(),
		PaneMemKB:    map[string]uint64{},
		WindowMemKB:  map[string]uint64{},
		SessionMemKB: map[string]uint64{},
		PaneMem:      map[string]string{},
		WindowMem:    map[string]string{},
		SessionMem:   map[string]string{},
	}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); c.sampleCPU(&m) }()
	go func() { defer wg.Done(); c.sampleMemPressure(&m) }()
	go func() { defer wg.Done(); c.sampleProcCount(&m) }()
	go func() { defer wg.Done(); c.samplePaneMemory(&m) }()
	wg.Wait()
	return m
}

func (c *Collector) sampleCPU(m *Metrics) {
	switch c.os {
	case "darwin":
		out, err := runCommand("top", "-l", "1", "-n", "0")
		if err != nil {
			return
		}
		val, ok := parseMacOSCPUUsageTotal(string(out))
		if ok {
			m.CPUPercent = val
			m.CPUOK = true
		}
	case "linux":
		out, err := runCommand("cat", "/proc/stat")
		if err != nil {
			return
		}
		total, idle, ok := parseLinuxCPUStat(string(out))
		if !ok {
			return
		}
		c.mu.Lock()
		prevTotal, prevIdle := c.prevLinuxCPU, c.prevLinuxIdle
		c.prevLinuxCPU = total
		c.prevLinuxIdle = idle
		c.mu.Unlock()
		if prevTotal > 0 {
			m.CPUPercent = computeLinuxCPUPercent(prevTotal, prevIdle, total, idle)
			m.CPUOK = true
		}
	}
}

func (c *Collector) sampleMemPressure(m *Metrics) {
	switch c.os {
	case "darwin":
		out, err := runCommand("memory_pressure")
		if err != nil {
			return
		}
		val, ok := parseMacOSMemPressure(string(out))
		if ok {
			m.MemPressure = val
			m.MemPressureOK = true
		}
	case "linux":
		out, err := runCommand("cat", "/proc/meminfo")
		if err != nil {
			return
		}
		total, avail, ok := parseLinuxMemInfo(string(out))
		if ok {
			m.MemPressure = linuxMemPressurePercent(total, avail)
			m.MemPressureOK = true
		}
	}
}

func (c *Collector) sampleProcCount(m *Metrics) {
	out, err := runCommand("ps", "aux")
	if err != nil {
		return
	}
	m.ProcCount = parseProcCount(string(out))
	m.ProcCountOK = true
}

func (c *Collector) samplePaneMemory(m *Metrics) {
	panesOut, err := runCommand(c.tmuxBin, "list-panes", "-a", "-F",
		"#{session_name}|#{window_index}|#{window_id}|#{pane_id}|#{pane_pid}|#{pane_current_command}")
	if err != nil {
		return
	}
	panes := parseTmuxPanesWithCommand(string(panesOut))
	if len(panes) == 0 {
		return
	}
	psOut, err := runCommand("ps", "-eo", "pid,ppid,rss")
	if err != nil {
		return
	}
	procs := parsePsPidPpidRss(string(psOut))
	children := buildChildrenMap(procs)
	rssByPid := make(map[int]uint64, len(procs))
	for _, p := range procs {
		rssByPid[p.PID] = p.RSS
	}
	result := aggregatePaneMemory(panes, children, rssByPid)
	m.PaneMemKB = result.PaneMem
	m.WindowMemKB = result.WindowMem
	m.SessionMemKB = result.SessionMem
	m.TotalMemKB = result.TotalMem
	for k, v := range result.PaneMem {
		m.PaneMem[k] = formatMemoryMB(v)
	}
	for k, v := range result.WindowMem {
		m.WindowMem[k] = formatMemoryMB(v)
	}
	for k, v := range result.SessionMem {
		m.SessionMem[k] = formatMemoryMB(v)
	}
	m.TotalMem = formatMemoryMB(result.TotalMem)
	// Collect pane commands for pre-hook fallback (avoids tmux call in status-tmux).
	m.PaneCommands = make(map[string]string, len(panes))
	for _, p := range panes {
		if p.Command != "" {
			m.PaneCommands[p.PaneID] = p.Command
		}
	}
}
