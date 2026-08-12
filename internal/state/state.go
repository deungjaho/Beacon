// Package state provides concurrent-safe, atomic local state management for
// Beacon pane records. It is the Go equivalent of lib/state.sh.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	CompletedTTLSeconds = 300
	LockTimeout         = 1500 * time.Millisecond
	LockStaleAge        = 30 * time.Second
)

// PaneRecord is the per-pane agent state written by hooks and report.
type PaneRecord struct {
	Session string `json:"session"`
	Window  string `json:"window"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Time    int64  `json:"time"`
	Cwd     string `json:"cwd"`
}

// LastCompleted tracks the most recently completed pane for jump.
type LastCompleted struct {
	Pane    string `json:"pane"`
	Session string `json:"session"`
	Window  string `json:"window"`
	Summary string `json:"summary"`
	Time    int64  `json:"time"`
}

// State is the full on-disk state document.
type State struct {
	Panes         map[string]PaneRecord `json:"panes"`
	LastCompleted *LastCompleted        `json:"last_completed"`
}

func defaultState() *State {
	return &State{Panes: map[string]PaneRecord{}, LastCompleted: nil}
}

// NewState returns a fresh empty State.
func NewState() *State {
	return defaultState()
}

func validStatus(s string) bool {
	switch s {
	case "working", "waiting", "blocked", "completed":
		return true
	}
	return false
}

// Store manages the local panes.json file with a mkdir-based mutex lock.
type Store struct {
	dir     string
	file    string
	lockDir string
	mu      sync.Mutex // serializes in-process callers; cross-process uses mkdir lock
	now     func() int64
}

// NewStore creates a Store rooted at dir. The directory is created if needed.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("state: mkdir: %w", err)
	}
	return &Store{
		dir:     dir,
		file:    filepath.Join(dir, "panes.json"),
		lockDir: filepath.Join(dir, ".state.lock"),
		now:     func() int64 { return time.Now().Unix() },
	}, nil
}

// SetNow overrides the clock (for testing).
func (s *Store) SetNow(fn func() int64) { s.now = fn }

// Load reads and validates panes.json, falling back to default on missing or
// corrupt files.
func (s *Store) Load() (*State, error) {
	data, err := os.ReadFile(s.file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultState(), nil
		}
		return nil, fmt.Errorf("state: read: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return defaultState(), nil
	}
	if st.Panes == nil {
		st.Panes = map[string]PaneRecord{}
	}
	return &st, nil
}

func (s *Store) acquireLock() error {
	s.mu.Lock()
	deadline := time.Now().Add(LockTimeout)
	for {
		if err := os.Mkdir(s.lockDir, 0o700); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("state: lock timeout")
		}
		// Check for stale lock.
		if info, err := os.Stat(s.lockDir); err == nil {
			if time.Since(info.ModTime()) > LockStaleAge {
				_ = os.RemoveAll(s.lockDir)
				continue
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Store) releaseLock() {
	_ = os.RemoveAll(s.lockDir)
	s.mu.Unlock()
}

func (s *Store) writeAtomic(data []byte) error {
	tmp := filepath.Join(s.dir, ".panes.json.tmp."+strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("state: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.file); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: rename: %w", err)
	}
	return nil
}

func (s *Store) save(st *State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}
	return s.writeAtomic(data)
}

// mutate loads, applies fn, and saves atomically under lock.
func (s *Store) mutate(fn func(*State)) error {
	if err := s.acquireLock(); err != nil {
		return err
	}
	defer s.releaseLock()
	st, err := s.Load()
	if err != nil {
		return err
	}
	fn(st)
	return s.save(st)
}

// SetPane writes a pane record. Validates status and pane ID.
func (s *Store) SetPane(pane string, rec PaneRecord) error {
	if pane == "" {
		return fmt.Errorf("state: pane is required")
	}
	if !validStatus(rec.Status) {
		return fmt.Errorf("state: unsupported status: %s", rec.Status)
	}
	return s.mutate(func(st *State) {
		if st.Panes == nil {
			st.Panes = map[string]PaneRecord{}
		}
		st.Panes[pane] = rec
	})
}

// DelPane removes a pane record.
func (s *Store) DelPane(pane string) error {
	return s.mutate(func(st *State) {
		delete(st.Panes, pane)
	})
}

// SetLast records the last completed pane.
func (s *Store) SetLast(rec LastCompleted) error {
	if rec.Pane == "" {
		return fmt.Errorf("state: pane is required")
	}
	return s.mutate(func(st *State) {
		st.LastCompleted = &rec
	})
}

// Cleanup removes expired completed records and panes not in the live set.
// livePanes can be nil to skip the live-pane check.
func (s *Store) Cleanup(now int64, ttl int64, livePanes []string) {
	liveSet := map[string]bool{}
	for _, p := range livePanes {
		liveSet[p] = true
	}
	_ = s.mutate(func(st *State) {
		for key, rec := range st.Panes {
			if rec.Status == "completed" && now-rec.Time > ttl {
				delete(st.Panes, key)
				continue
			}
			if livePanes != nil && !liveSet[key] {
				delete(st.Panes, key)
			}
		}
	})
}

// Reset clears all state to the default empty document.
func (s *Store) Reset() error {
	if err := s.acquireLock(); err != nil {
		return err
	}
	defer s.releaseLock()
	return s.save(defaultState())
}

// SanitizeSummary collapses whitespace and truncates to 80 characters.
func SanitizeSummary(s string) string {
	var out []byte
	prevSpace := false
	for _, r := range s {
		switch r {
		case '\r', '\n', '\t':
			if !prevSpace {
				out = append(out, ' ')
				prevSpace = true
			}
		case ' ':
			if !prevSpace {
				out = append(out, ' ')
				prevSpace = true
			}
		default:
			out = append(out, []byte(string(r))...)
			prevSpace = false
		}
	}
	result := string(out)
	if len(result) > 80 {
		result = result[:80]
	}
	return result
}
