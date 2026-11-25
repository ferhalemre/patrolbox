// Package state remembers the last status of every check so patrolbox only
// notifies when something changes (or when a reminder is due).
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ferhalemre/patrolbox/internal/core"
)

type Entry struct {
	Status   core.Status `json:"status"`
	Since    time.Time   `json:"since"`    // when this status started
	Notified time.Time   `json:"notified"` // when it was last reported
}

type State struct {
	Entries map[string]Entry `json:"entries"`
}

// Load reads the state file; a missing file is an empty state.
func Load(path string) (State, error) {
	s := State{Entries: map[string]Entry{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.Entries == nil {
		// A corrupt state file must not stop monitoring: start over.
		return State{Entries: map[string]Entry{}}, nil
	}
	return s, nil
}

// Save writes the state atomically.
func (s State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Diff is what changed since the previous run.
type Diff struct {
	New       []core.Result // newly failing, or failing with a different severity
	Ongoing   []core.Result // still failing and due for a reminder
	Recovered []core.Result // was failing, now OK
}

func (d Diff) Empty() bool { return len(d.New)+len(d.Ongoing)+len(d.Recovered) == 0 }

// Update compares results with the stored state, returns what to report, and
// advances the state. repeat is the reminder interval for unchanged failures
// (0 = never remind). Checks that no longer exist are forgotten.
func (s *State) Update(results []core.Result, now time.Time, repeat time.Duration) Diff {
	var d Diff
	next := make(map[string]Entry, len(results))
	for _, r := range results {
		prev, seen := s.Entries[r.ID()]
		e := Entry{Status: r.Status, Since: now}
		if seen && prev.Status == r.Status {
			e = prev
		}
		switch {
		case r.Status == core.OK:
			if seen && prev.Status != core.OK {
				d.Recovered = append(d.Recovered, r)
			}
		case !seen || prev.Status != r.Status:
			d.New = append(d.New, r)
			e.Notified = now
		case repeat > 0 && now.Sub(prev.Notified) >= repeat:
			d.Ongoing = append(d.Ongoing, r)
			e.Notified = now
		}
		next[r.ID()] = e
	}
	s.Entries = next
	return d
}
