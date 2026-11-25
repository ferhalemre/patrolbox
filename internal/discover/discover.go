package discover

import (
	"context"
	"sort"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

// Sites runs every enabled source and returns unique hosts sorted by Host.
// When a host is found by several sources the first in the order caddy, nginx, docker wins.
// An empty Caddy/Nginx setting is treated as "auto"; "off" disables the source.
func Sites(ctx context.Context, cfg config.Discover, dockerSocket string) []core.Site {
	var all []core.Site
	if p := orAuto(cfg.Caddy); p != "off" {
		all = append(all, Caddy(p)...)
	}
	if p := orAuto(cfg.Nginx); p != "off" {
		all = append(all, Nginx(p)...)
	}
	if cfg.Docker {
		all = append(all, Docker(ctx, dockerSocket)...)
	}
	seen := map[string]bool{}
	var out []core.Site
	for _, s := range all {
		if !seen[s.Host] {
			seen[s.Host] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func orAuto(p string) string {
	if p == "" {
		return "auto"
	}
	return p
}
