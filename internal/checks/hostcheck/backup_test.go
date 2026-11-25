package hostcheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

func mk(t *testing.T, path string, size int, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := now.Add(-age)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func one(t *testing.T, b config.Backup, now time.Time) core.Result {
	t.Helper()
	b.Name = "b"
	rs := Backup([]config.Backup{b}, now)
	if len(rs) != 1 {
		t.Fatalf("got %d results", len(rs))
	}
	if rs[0].Check != "backup" || rs[0].Target != "b" {
		t.Fatalf("bad result %+v", rs[0])
	}
	return rs[0]
}

func TestBackup(t *testing.T) {
	now := time.Now()
	h := config.Duration(time.Hour)
	d26 := config.Duration(26 * time.Hour)

	t.Run("fresh dir", func(t *testing.T) {
		dir := t.TempDir()
		mk(t, filepath.Join(dir, "old.sql"), 100, 50*time.Hour, now)
		mk(t, filepath.Join(dir, "sub", "new.sql"), 2048, 5*time.Hour, now)
		mk(t, filepath.Join(dir, ".hidden"), 100, time.Minute, now)
		mk(t, filepath.Join(dir, "dump.tmp"), 100, time.Minute, now)
		mk(t, filepath.Join(dir, "dump.part"), 100, time.Minute, now)
		r := one(t, config.Backup{Path: dir, MaxAge: d26}, now)
		if r.Status != core.OK || r.MsgKey != KeyBackupOK {
			t.Fatalf("%+v", r)
		}
		if r.Args["age"] != "5h" || r.Args["file"] != "new.sql" || r.Args["size"] != "2.0 KB" || r.Args["max_age"] != "1d" {
			t.Fatalf("args %v", r.Args)
		}
	})
	t.Run("stale file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "a.gz")
		mk(t, f, 100, 3*24*time.Hour, now)
		r := one(t, config.Backup{Path: f, MaxAge: d26}, now)
		if r.Status != core.Crit || r.MsgKey != KeyBackupStale || r.Args["age"] != "3d" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("default max age", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "a.gz")
		mk(t, f, 100, 30*time.Hour, now)
		if r := one(t, config.Backup{Path: f}, now); r.MsgKey != KeyBackupStale {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("empty dir", func(t *testing.T) {
		r := one(t, config.Backup{Path: t.TempDir(), MaxAge: d26}, now)
		if r.Status != core.Crit || r.MsgKey != KeyBackupMissing {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("only ignored files", func(t *testing.T) {
		dir := t.TempDir()
		mk(t, filepath.Join(dir, "x.tmp"), 10, 0, now)
		if r := one(t, config.Backup{Path: dir, MaxAge: d26}, now); r.MsgKey != KeyBackupMissing {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("missing path", func(t *testing.T) {
		r := one(t, config.Backup{Path: "/nonexistent/zzz", MaxAge: d26}, now)
		if r.Status != core.Crit || r.MsgKey != KeyBackupMissing {
			t.Fatalf("%+v", r)
		}
		if r := one(t, config.Backup{}, now); r.MsgKey != KeyBackupMissing {
			t.Fatalf("empty path: %+v", r)
		}
	})
	t.Run("zero size", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "z")
		mk(t, f, 0, time.Minute, now)
		r := one(t, config.Backup{Path: f, MaxAge: h}, now)
		if r.Status != core.Crit || r.MsgKey != KeyBackupEmpty {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("too small", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "s")
		mk(t, f, 100, time.Minute, now)
		r := one(t, config.Backup{Path: f, MaxAge: h, MinSize: 1000}, now)
		if r.Status != core.Crit || r.MsgKey != KeyBackupTooSmall || r.Args["min_size"] != "1000 B" {
			t.Fatalf("%+v", r)
		}
		if r := one(t, config.Backup{Path: f, MaxAge: h, MinSize: 100}, now); r.Status != core.OK {
			t.Fatalf("exact min size should pass: %+v", r)
		}
	})
	t.Run("glob", func(t *testing.T) {
		dir := t.TempDir()
		mk(t, filepath.Join(dir, "db-1.sql"), 100, 10*time.Hour, now)
		mk(t, filepath.Join(dir, "db-2.sql"), 100, 2*time.Hour, now)
		mk(t, filepath.Join(dir, "other.txt"), 100, time.Minute, now)
		mk(t, filepath.Join(dir, "dbdir", "inner.sql"), 100, time.Hour, now)
		r := one(t, config.Backup{Path: filepath.Join(dir, "db*"), MaxAge: d26}, now)
		if r.Status != core.OK || r.Args["file"] != "inner.sql" {
			t.Fatalf("%+v", r)
		}
		r = one(t, config.Backup{Path: filepath.Join(dir, "db-*.sql"), MaxAge: d26}, now)
		if r.Args["file"] != "db-2.sql" || r.Args["age"] != "2h" {
			t.Fatalf("%+v", r)
		}
		if r := one(t, config.Backup{Path: filepath.Join(dir, "nomatch*"), MaxAge: d26}, now); r.MsgKey != KeyBackupMissing {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("sorted by target", func(t *testing.T) {
		rs := Backup([]config.Backup{{Name: "z", Path: "/x"}, {Name: "a", Path: "/y"}}, now)
		if rs[0].Target != "a" || rs[1].Target != "z" {
			t.Fatalf("%+v", rs)
		}
	})
}
