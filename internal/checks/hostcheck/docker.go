package hostcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

const (
	defaultSocket = "/var/run/docker.sock"
	// A container that restarted a lot but has run quietly for longer than
	// this is considered recovered: RestartCount never resets, so without a
	// cut-off one crash-loop last month would warn forever.
	restartWindow = 24 * time.Hour
	// Health "starting" is normal right after boot; only warn once a
	// container has had this long to become healthy.
	healthGrace = 10 * time.Minute
)

type listItem struct {
	ID    string   `json:"Id"`
	Names []string `json:"Names"`
}

type inspectInfo struct {
	Name         string `json:"Name"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Restarting bool   `json:"Restarting"`
		ExitCode   int    `json:"ExitCode"`
		OOMKilled  bool   `json:"OOMKilled"`
		StartedAt  string `json:"StartedAt"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
}

// Docker checks every container through the Docker Engine API on cfg.Socket.
// A missing socket means the server has no Docker, which is not a problem.
func Docker(ctx context.Context, cfg config.Docker) []core.Result {
	start := time.Now()
	sock := cfg.Socket
	if sock == "" {
		sock = defaultSocket
	}
	if _, err := os.Stat(sock); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return []core.Result{dockerUnavailable(err, start)}
	}

	cl := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
	var list []listItem
	if err := dockerGet(ctx, cl, "/containers/json?all=1", &list); err != nil {
		return []core.Result{dockerUnavailable(err, start)}
	}

	now := time.Now()
	var out []core.Result
	running := map[string]bool{}
	for _, it := range list {
		if len(it.Names) == 0 {
			continue
		}
		name := strings.TrimPrefix(it.Names[0], "/")
		t0 := time.Now()
		var info inspectInfo
		err := dockerGet(ctx, cl, "/containers/"+url.PathEscape(it.ID)+"/json", &info)
		if err != nil {
			if !config.Match(cfg.Ignore, name) {
				out = append(out, core.Result{Check: "docker", Target: name, Status: core.Unknown,
					MsgKey: KeyDockerUnavailable, Args: map[string]any{"error": err.Error()}, Took: time.Since(t0)})
			}
			continue
		}
		if info.State.Running {
			running[name] = true
		}
		if config.Match(cfg.Ignore, name) {
			continue
		}
		if r, ok := evalContainer(name, info, cfg.MaxRestarts, now); ok {
			r.Took = time.Since(t0)
			out = append(out, r)
		}
	}

	for _, pat := range cfg.RequireNames {
		found := false
		for name := range running {
			if config.Match([]string{pat}, name) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, core.Result{Check: "docker", Target: pat, Status: core.Crit,
				MsgKey: KeyDockerRequired, Args: map[string]any{"name": pat}, Took: time.Since(start)})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func dockerUnavailable(err error, start time.Time) core.Result {
	return core.Result{Check: "docker", Target: "docker", Status: core.Unknown,
		MsgKey: KeyDockerUnavailable, Args: map[string]any{"error": err.Error()}, Took: time.Since(start)}
}

func dockerGet(ctx context.Context, cl *http.Client, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("%s: %s %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

// evalContainer judges one container. ok=false means "no result" (one-shot
// jobs and cleanly stopped containers).
func evalContainer(name string, c inspectInfo, maxRestarts int, now time.Time) (core.Result, bool) {
	res := func(st core.Status, key string, args map[string]any) (core.Result, bool) {
		return core.Result{Check: "docker", Target: name, Status: st, MsgKey: key, Args: args}, true
	}
	s := c.State
	health := ""
	if s.Health != nil {
		health = s.Health.Status
	}
	var uptime time.Duration
	if t, err := time.Parse(time.RFC3339Nano, s.StartedAt); err == nil && t.Year() > 1 && s.Running {
		uptime = now.Sub(t)
	}
	up := humanDur(uptime)

	switch {
	case s.Restarting || s.Status == "restarting":
		return res(core.Crit, KeyDockerRestarting, map[string]any{"count": c.RestartCount})
	case s.OOMKilled:
		return res(core.Crit, KeyDockerOOM, nil)
	case s.Status == "exited" || s.Status == "dead":
		policy := c.HostConfig.RestartPolicy.Name
		if s.ExitCode != 0 && policy != "" && policy != "no" {
			return res(core.Crit, KeyDockerExited, map[string]any{"code": s.ExitCode})
		}
		return core.Result{}, false
	case s.Status == "created":
		return core.Result{}, false
	case s.Status == "paused":
		return res(core.Warn, KeyDockerPaused, nil)
	case !s.Running:
		return core.Result{}, false
	}

	if health == "unhealthy" {
		return res(core.Crit, KeyDockerUnhealthy, map[string]any{"uptime": up})
	}
	if c.RestartCount > maxRestarts && uptime < restartWindow {
		return res(core.Warn, KeyDockerRestartLoop,
			map[string]any{"count": c.RestartCount, "max": maxRestarts, "uptime": up})
	}
	if health == "starting" && uptime > healthGrace {
		return res(core.Warn, KeyDockerHealthStarting, map[string]any{"uptime": up})
	}
	return res(core.OK, KeyDockerOK, map[string]any{"uptime": up})
}
