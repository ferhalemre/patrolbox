package hostcheck

import (
	"strings"
	"testing"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

const sampleMounts = `sysfs /sys sysfs rw,nosuid 0 0
proc /proc proc rw 0 0
udev /dev devtmpfs rw 0 0
tmpfs /run tmpfs rw 0 0
/dev/sda1 / ext4 rw,relatime 0 0
/dev/sda1 /var/lib/docker/overlay2/x ext4 rw 0 0
/dev/sda1 /srv/bind ext4 rw 0 0
overlay /var/lib/docker/overlay2/abc/merged overlay rw 0 0
/dev/loop0 /snap/core/1 squashfs ro 0 0
cgroup2 /sys/fs/cgroup cgroup2 rw 0 0
nsfs /run/docker/netns/x nsfs rw 0 0
gvfsd-fuse /run/user/1000/gvfs fuse.gvfsd-fuse rw 0 0
/dev/sdb1 /mnt/with\040space ext4 rw 0 0
/dev/sdc1 /data xfs rw 0 0
/dev/sdc1 /data/deeper xfs rw 0 0
tmpfs /dev/shm tmpfs rw 0 0
shortline
`

func TestParseMounts(t *testing.T) {
	got := parseMounts(strings.NewReader(sampleMounts))
	var pts []string
	for _, e := range got {
		pts = append(pts, e.Point)
	}
	want := []string{"/", "/data", "/mnt/with space"}
	if strings.Join(pts, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", pts, want)
	}
}

func TestDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Disk{Mounts: []string{dir}, WarnPercent: 101, CritPercent: 101}
	rs := Disk(cfg)
	if len(rs) != 1 || rs[0].Check != "disk" || rs[0].Target != dir {
		t.Fatalf("%+v", rs)
	}
	r := rs[0]
	if r.Status != core.OK || r.MsgKey != KeyDiskOK {
		t.Fatalf("%+v", r)
	}
	pct, _ := r.Args["percent"].(int)
	if _, ok := r.Args["free"].(string); !ok || r.Args["mount"] != dir {
		t.Fatalf("args %v", r.Args)
	}
	if _, ok := r.Args["inodes"].(int); !ok {
		t.Fatalf("args %v", r.Args)
	}
	if pct < 1 {
		t.Skip("filesystem is empty; cannot force thresholds")
	}

	r = Disk(config.Disk{Mounts: []string{dir}, WarnPercent: 1, CritPercent: 101})[0]
	if r.Status != core.Warn || (r.MsgKey != KeyDiskSpaceWarn && r.MsgKey != KeyDiskInodesWarn) {
		t.Fatalf("warn: %+v", r)
	}
	r = Disk(config.Disk{Mounts: []string{dir}, WarnPercent: 1, CritPercent: 1})[0]
	if r.Status != core.Crit || (r.MsgKey != KeySpaceCrit && r.MsgKey != KeyDiskInodesCrit) {
		t.Fatalf("crit: %+v", r)
	}
}

func TestDiskUnreadable(t *testing.T) {
	rs := Disk(config.Disk{Mounts: []string{"/nonexistent/zzz"}})
	if len(rs) != 1 || rs[0].Status != core.Unknown || rs[0].MsgKey != KeyDiskUnreadable {
		t.Fatalf("%+v", rs)
	}
}

func TestHumanize(t *testing.T) {
	if got := humanBytes(13194139534); got != "12.3 GB" {
		t.Fatal(got)
	}
	if humanBytes(5) != "5 B" {
		t.Fatal(humanBytes(5))
	}
}
