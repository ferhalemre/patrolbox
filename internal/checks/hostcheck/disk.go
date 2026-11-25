package hostcheck

import (
	"bufio"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

// mountEntry is one line of /proc/self/mounts.
type mountEntry struct {
	Device, Point, FSType string
}

// fsStat is the part of statfs the check needs.
type fsStat struct {
	Blocks, Bfree, Bavail uint64
	Files, Ffree          uint64
	Bsize                 uint64
}

var skipFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true, "overlay": true,
	"squashfs": true, "nsfs": true, "devpts": true, "securityfs": true, "debugfs": true,
	"tracefs": true, "configfs": true, "pstore": true, "bpf": true, "mqueue": true,
	"hugetlbfs": true, "autofs": true, "binfmt_misc": true, "fusectl": true,
	"ramfs": true, "efivarfs": true, "selinuxfs": true, "rpc_pipefs": true,
}

var skipPrefixes = []string{"/proc", "/sys", "/dev", "/run", "/snap", "/var/lib/docker", "/var/lib/containerd"}

func skipPath(p string) bool {
	for _, pre := range skipPrefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

// unescapeMount decodes the \040 style octal escapes used in /proc/mounts.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseMounts reads /proc/mounts text and returns the real filesystems:
// pseudo and container mounts are dropped and, per device, only the shortest
// mount point is kept. The result is sorted by mount point.
func parseMounts(r io.Reader) []mountEntry {
	best := map[string]mountEntry{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		e := mountEntry{Device: unescapeMount(f[0]), Point: unescapeMount(f[1]), FSType: f[2]}
		if skipFS[e.FSType] || strings.HasPrefix(e.FSType, "cgroup") || e.FSType == "fuse" || strings.HasPrefix(e.FSType, "fuse.") {
			continue
		}
		if !strings.HasPrefix(e.Point, "/") || skipPath(e.Point) {
			continue
		}
		if cur, ok := best[e.Device]; !ok || len(e.Point) < len(cur.Point) ||
			len(e.Point) == len(cur.Point) && e.Point < cur.Point {
			best[e.Device] = e
		}
	}
	out := make([]mountEntry, 0, len(best))
	for _, e := range best {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Point < out[j].Point })
	return out
}

func ceilPercent(used, total uint64) int {
	if total == 0 {
		return 0
	}
	return int((used*100 + total - 1) / total)
}

func level(pct, warn, crit int) core.Status {
	switch {
	case crit > 0 && pct >= crit:
		return core.Crit
	case warn > 0 && pct >= warn:
		return core.Warn
	}
	return core.OK
}

// Disk checks space and inode usage of every real filesystem (or of
// cfg.Mounts when set).
func Disk(cfg config.Disk) []core.Result {
	var mounts []string
	if len(cfg.Mounts) > 0 {
		mounts = append(mounts, cfg.Mounts...)
	} else if f, err := os.Open("/proc/self/mounts"); err == nil {
		for _, e := range parseMounts(f) {
			mounts = append(mounts, e.Point)
		}
		f.Close()
	}

	var out []core.Result
	for _, m := range mounts {
		out = append(out, diskOne(m, cfg))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func diskOne(mount string, cfg config.Disk) core.Result {
	t0 := time.Now()
	st, err := statfs(mount)
	if err != nil {
		return core.Result{Check: "disk", Target: mount, Status: core.Unknown, MsgKey: KeyDiskUnreadable,
			Args: map[string]any{"mount": mount, "error": err.Error()}, Took: time.Since(t0)}
	}
	// Same arithmetic as df: used / (used + available to unprivileged users).
	used := st.Blocks - st.Bfree
	if st.Bfree > st.Blocks {
		used = 0
	}
	pct := ceilPercent(used, used+st.Bavail)
	free := humanBytes(st.Bavail * st.Bsize)

	ipct := 0
	if st.Files > 0 && st.Ffree <= st.Files {
		ipct = ceilPercent(st.Files-st.Ffree, st.Files)
	}

	sSt := level(pct, cfg.WarnPercent, cfg.CritPercent)
	iSt := level(ipct, cfg.WarnPercent, cfg.CritPercent)

	r := core.Result{Check: "disk", Target: mount, Took: time.Since(t0)}
	args := func(p int) map[string]any {
		return map[string]any{"percent": p, "free": free, "mount": mount}
	}
	switch {
	case iSt > sSt && iSt == core.Crit:
		r.Status, r.MsgKey, r.Args = core.Crit, KeyDiskInodesCrit, args(ipct)
	case iSt > sSt:
		r.Status, r.MsgKey, r.Args = core.Warn, KeyDiskInodesWarn, args(ipct)
	case sSt == core.Crit:
		r.Status, r.MsgKey, r.Args = core.Crit, KeySpaceCrit, args(pct)
	case sSt == core.Warn:
		r.Status, r.MsgKey, r.Args = core.Warn, KeyDiskSpaceWarn, args(pct)
	default:
		r.Status, r.MsgKey = core.OK, KeyDiskOK
		r.Args = args(pct)
		r.Args["inodes"] = ipct
	}
	r.Took = time.Since(t0)
	return r
}
