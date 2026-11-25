package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/core"
)

func res(target string, s core.Status) core.Result {
	return core.Result{Check: "http", Target: target, Status: s}
}

func TestUpdateLifecycle(t *testing.T) {
	s := State{Entries: map[string]Entry{}}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// First run: OK results are silent, failures are new.
	d := s.Update([]core.Result{res("a", core.OK), res("b", core.Crit)}, t0, time.Hour)
	if len(d.New) != 1 || d.New[0].Target != "b" || len(d.Recovered) != 0 || len(d.Ongoing) != 0 {
		t.Fatalf("first run: %+v", d)
	}

	// Unchanged before the reminder interval: nothing.
	d = s.Update([]core.Result{res("a", core.OK), res("b", core.Crit)}, t0.Add(30*time.Minute), time.Hour)
	if !d.Empty() {
		t.Fatalf("unchanged run should be silent: %+v", d)
	}

	// Reminder is due.
	d = s.Update([]core.Result{res("a", core.OK), res("b", core.Crit)}, t0.Add(61*time.Minute), time.Hour)
	if len(d.Ongoing) != 1 {
		t.Fatalf("reminder: %+v", d)
	}
	if got := s.Entries["http:b"].Since; !got.Equal(t0) {
		t.Errorf("Since must keep the first failure time, got %v", got)
	}

	// Severity change counts as new.
	d = s.Update([]core.Result{res("a", core.Warn), res("b", core.Warn)}, t0.Add(2*time.Hour), time.Hour)
	if len(d.New) != 2 {
		t.Fatalf("severity change: %+v", d)
	}

	// Recovery.
	d = s.Update([]core.Result{res("a", core.OK), res("b", core.OK)}, t0.Add(3*time.Hour), time.Hour)
	if len(d.Recovered) != 2 || len(d.New) != 0 {
		t.Fatalf("recovery: %+v", d)
	}

	// A check that disappears is forgotten, not reported.
	d = s.Update([]core.Result{res("a", core.OK)}, t0.Add(4*time.Hour), time.Hour)
	if !d.Empty() || len(s.Entries) != 1 {
		t.Fatalf("vanished check: %+v entries=%d", d, len(s.Entries))
	}
}

func TestNoRepeatWhenZero(t *testing.T) {
	s := State{Entries: map[string]Entry{}}
	t0 := time.Now()
	s.Update([]core.Result{res("b", core.Crit)}, t0, 0)
	if d := s.Update([]core.Result{res("b", core.Crit)}, t0.Add(1000*time.Hour), 0); !d.Empty() {
		t.Fatalf("repeat=0 must never remind: %+v", d)
	}
}

func TestSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	s := State{Entries: map[string]Entry{}}
	s.Update([]core.Result{res("b", core.Crit)}, time.Now(), 0)
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.Entries["http:b"].Status != core.Crit {
		t.Fatalf("load: %+v %v", got, err)
	}
	if got, err := Load(filepath.Join(t.TempDir(), "nope.json")); err != nil || len(got.Entries) != 0 {
		t.Fatalf("missing file should be empty state: %+v %v", got, err)
	}
}
