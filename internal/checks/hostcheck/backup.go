package hostcheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

const defaultMaxAge = 26 * time.Hour

type backupFile struct {
	path    string
	modTime time.Time
	size    int64
}

// Backup checks that the newest file behind each Backup.Path is fresh and big enough.
func Backup(cfg []config.Backup, now time.Time) []core.Result {
	var out []core.Result
	for _, b := range cfg {
		out = append(out, backupOne(b, now))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func backupOne(b config.Backup, now time.Time) core.Result {
	t0 := time.Now()
	r := core.Result{Check: "backup", Target: b.Name}
	if r.Target == "" {
		r.Target = b.Path
	}
	maxAge := b.MaxAge.D()
	if maxAge <= 0 {
		maxAge = defaultMaxAge
	}
	f, ok := newestFile(b.Path)
	defer func() { r.Took = time.Since(t0) }()
	if !ok {
		r.Status, r.MsgKey, r.Args = core.Crit, KeyBackupMissing, map[string]any{"path": b.Path}
		return r
	}
	age := now.Sub(f.modTime)
	if age < 0 {
		age = 0
	}
	args := map[string]any{
		"age": humanDur(age), "size": humanBytes(uint64(f.size)),
		"file": filepath.Base(f.path), "max_age": humanDur(maxAge),
	}
	switch {
	case age > maxAge:
		r.Status, r.MsgKey = core.Crit, KeyBackupStale
	case f.size <= 0:
		r.Status, r.MsgKey = core.Crit, KeyBackupEmpty
	case b.MinSize > 0 && f.size < b.MinSize:
		r.Status, r.MsgKey = core.Crit, KeyBackupTooSmall
		args["min_size"] = humanBytes(uint64(b.MinSize))
	default:
		r.Status, r.MsgKey = core.OK, KeyBackupOK
	}
	r.Args = args
	return r
}

// newestFile resolves p (file, directory or glob) and returns the most
// recently modified regular file.
func newestFile(p string) (backupFile, bool) {
	if p == "" {
		return backupFile{}, false
	}
	matches := []string{p}
	if strings.ContainsAny(p, "*?[") {
		m, err := filepath.Glob(p)
		if err != nil {
			return backupFile{}, false
		}
		matches = m
	}
	var best backupFile
	found := false
	consider := func(path string, info fs.FileInfo) {
		if !found || info.ModTime().After(best.modTime) {
			best = backupFile{path, info.ModTime(), info.Size()}
			found = true
		}
	}
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() {
			consider(m, info)
			continue
		}
		if !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(m, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if path != m && strings.HasPrefix(name, ".") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".part") {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				consider(path, fi)
			}
			return nil
		})
	}
	return best, found
}
