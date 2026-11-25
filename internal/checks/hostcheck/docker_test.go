package hostcheck

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

type fc struct {
	name    string
	status  string
	running bool
	restart bool
	exit    int
	oom     bool
	health  string
	started time.Duration // how long ago
	rcount  int
	policy  string
}

func (c fc) inspect() map[string]any {
	st := map[string]any{
		"Status": c.status, "Running": c.running, "Restarting": c.restart,
		"ExitCode": c.exit, "OOMKilled": c.oom,
		"StartedAt": time.Now().Add(-c.started).UTC().Format(time.RFC3339Nano),
	}
	if c.health != "" {
		st["Health"] = map[string]any{"Status": c.health}
	}
	return map[string]any{
		"Name": "/" + c.name, "RestartCount": c.rcount, "State": st,
		"HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": c.policy}},
	}
}

func run(c fc) fc { // helper: a plain running container
	c.status, c.running = "running", true
	if c.started == 0 {
		c.started = 3 * 24 * time.Hour
	}
	return c
}

func serve(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func dockerServer(t *testing.T, cs ...fc) string {
	mux := http.NewServeMux()
	var list []map[string]any
	for i, c := range cs {
		id := "id" + string(rune('a'+i))
		list = append(list, map[string]any{"Id": id, "Names": []string{"/" + c.name}})
		c := c
		mux.HandleFunc("/containers/"+id+"/json", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(c.inspect())
		})
	}
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("all") != "1" {
			http.Error(w, "need all=1", 400)
			return
		}
		json.NewEncoder(w).Encode(list)
	})
	return serve(t, mux)
}

func byTarget(rs []core.Result) map[string]core.Result {
	m := map[string]core.Result{}
	for _, r := range rs {
		m[r.Target] = r
	}
	return m
}

func TestDockerRules(t *testing.T) {
	sock := dockerServer(t,
		run(fc{name: "web", health: "healthy", started: 3*24*time.Hour + time.Hour}),
		run(fc{name: "db", started: 5*time.Hour + time.Minute}),
		fc{name: "loopy", status: "restarting", restart: true, rcount: 9},
		fc{name: "crashed", status: "exited", exit: 1, policy: "always"},
		fc{name: "deadone", status: "dead", exit: 137, policy: "unless-stopped"},
		fc{name: "oneshot", status: "exited", exit: 1, policy: "no"},
		fc{name: "oneshot2", status: "exited", exit: 1, policy: ""},
		fc{name: "clean", status: "exited", exit: 0, policy: "always"},
		fc{name: "created", status: "created"},
		run(fc{name: "oom", oom: true}),
		run(fc{name: "sick", health: "unhealthy"}),
		run(fc{name: "flappy", rcount: 10, started: 2 * time.Hour}),
		run(fc{name: "oldflap", rcount: 10, started: 30 * time.Hour}),
		run(fc{name: "fewrestarts", rcount: 3, started: time.Hour}),
		run(fc{name: "slowstart", health: "starting", started: 20 * time.Minute}),
		run(fc{name: "freshstart", health: "starting", started: 2*time.Minute + 30*time.Second}),
		run(fc{name: "tmp-job"}),
		fc{name: "frozen", status: "paused", running: true},
	)
	rs := Docker(context.Background(), config.Docker{Socket: sock, MaxRestarts: 3, Ignore: []string{"tmp-*"}})
	m := byTarget(rs)
	for i := 1; i < len(rs); i++ {
		if rs[i-1].Target > rs[i].Target {
			t.Fatal("not sorted")
		}
	}
	want := map[string]struct {
		st  core.Status
		key string
	}{
		"web": {core.OK, KeyDockerOK}, "db": {core.OK, KeyDockerOK},
		"loopy": {core.Crit, KeyDockerRestarting}, "crashed": {core.Crit, KeyDockerExited},
		"deadone": {core.Crit, KeyDockerExited}, "oom": {core.Crit, KeyDockerOOM},
		"sick": {core.Crit, KeyDockerUnhealthy}, "flappy": {core.Warn, KeyDockerRestartLoop},
		"oldflap": {core.OK, KeyDockerOK}, "fewrestarts": {core.OK, KeyDockerOK},
		"slowstart": {core.Warn, KeyDockerHealthStarting}, "freshstart": {core.OK, KeyDockerOK},
		"frozen": {core.Warn, KeyDockerPaused},
	}
	for name, w := range want {
		r, ok := m[name]
		if !ok {
			t.Errorf("%s: no result", name)
			continue
		}
		if r.Status != w.st || r.MsgKey != w.key || r.Check != "docker" {
			t.Errorf("%s: got %v %s want %v %s", name, r.Status, r.MsgKey, w.st, w.key)
		}
	}
	for _, name := range []string{"oneshot", "oneshot2", "clean", "created", "tmp-job"} {
		if _, ok := m[name]; ok {
			t.Errorf("%s should produce no result", name)
		}
	}
	if len(rs) != len(want) {
		t.Errorf("got %d results want %d", len(rs), len(want))
	}
	if m["web"].Args["uptime"] != "3d" || m["db"].Args["uptime"] != "5h" || m["freshstart"].Args["uptime"] != "2m" {
		t.Errorf("uptimes: %v %v %v", m["web"].Args, m["db"].Args, m["freshstart"].Args)
	}
	if m["loopy"].Args["count"] != 9 || m["crashed"].Args["code"] != 1 {
		t.Errorf("args: %v %v", m["loopy"].Args, m["crashed"].Args)
	}
	if a := m["flappy"].Args; a["count"] != 10 || a["max"] != 3 {
		t.Errorf("flappy args %v", a)
	}
}

func TestDockerRequire(t *testing.T) {
	sock := dockerServer(t,
		run(fc{name: "app-1"}),
		fc{name: "stopped", status: "exited", exit: 0},
	)
	cfg := config.Docker{Socket: sock, MaxRestarts: 3,
		RequireNames: []string{"app-*", "stopped", "ghost", "db"}}
	m := byTarget(Docker(context.Background(), cfg))
	if r := m["app-1"]; r.Status != core.OK {
		t.Errorf("app-1: %+v", r)
	}
	if _, ok := m["app-*"]; ok {
		t.Error("matched pattern should add no extra result")
	}
	for _, n := range []string{"stopped", "ghost", "db"} {
		r, ok := m[n]
		if !ok || r.Status != core.Crit || r.MsgKey != KeyDockerRequired || r.Args["name"] != n {
			t.Errorf("%s: %+v", n, r)
		}
	}
	cfg.RequireNames = []string{"nothing-*"}
	m = byTarget(Docker(context.Background(), cfg))
	if r := m["nothing-*"]; r.Status != core.Crit {
		t.Errorf("unmatched wildcard: %+v", r)
	}
}

func TestDockerNoSocket(t *testing.T) {
	rs := Docker(context.Background(), config.Docker{Socket: filepath.Join(t.TempDir(), "nope.sock")})
	if rs != nil {
		t.Fatalf("want nil, got %+v", rs)
	}
}

func TestDockerAPIFailure(t *testing.T) {
	sock := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "permission denied", http.StatusForbidden)
	}))
	rs := Docker(context.Background(), config.Docker{Socket: sock})
	if len(rs) != 1 || rs[0].Target != "docker" || rs[0].Status != core.Unknown || rs[0].MsgKey != KeyDockerUnavailable {
		t.Fatalf("%+v", rs)
	}
	if !strings.Contains(rs[0].Args["error"].(string), "403") {
		t.Fatalf("%v", rs[0].Args)
	}

	// A path that exists but is not a socket.
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o600)
	rs = Docker(context.Background(), config.Docker{Socket: f})
	if len(rs) != 1 || rs[0].Status != core.Unknown {
		t.Fatalf("%+v", rs)
	}
}

func TestDockerInspectFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"Id":"x","Names":["/gone"]},{"Id":"y","Names":[]}]`))
	})
	sock := serve(t, mux)
	rs := Docker(context.Background(), config.Docker{Socket: sock})
	if len(rs) != 1 || rs[0].Target != "gone" || rs[0].Status != core.Unknown {
		t.Fatalf("%+v", rs)
	}
}
