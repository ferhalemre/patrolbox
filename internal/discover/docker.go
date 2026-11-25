package discover

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/core"
)

var (
	traefikRuleKey = regexp.MustCompile(`^traefik\.http\.routers\..+\.rule$`)
	caddyLabelKey  = regexp.MustCompile(`^caddy(_\d+)?$`)
	traefikHost    = regexp.MustCompile(`\bHost\(([^)]*)\)`)
	traefikArg     = regexp.MustCompile("`([^`]*)`|\"([^\"]*)\"")
)

// Docker reads caddy-docker-proxy, Traefik and com.patrolbox.host labels of
// running containers through the Engine API on a unix socket.
func Docker(ctx context.Context, socket string) []core.Site {
	if socket == "" {
		return nil
	}
	if _, err := os.Stat(socket); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives: true,
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json", nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil
	}
	return collect("docker", dockerLabelHosts(b))
}

func dockerLabelHosts(b []byte) []string {
	var cs []struct {
		Labels map[string]string `json:"Labels"`
	}
	if json.Unmarshal(b, &cs) != nil {
		return nil
	}
	var out []string
	for _, c := range cs {
		keys := make([]string, 0, len(c.Labels))
		for k := range c.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := c.Labels[k]
			switch {
			case caddyLabelKey.MatchString(k):
				out = append(out, strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })...)
			case traefikRuleKey.MatchString(k):
				for _, m := range traefikHost.FindAllStringSubmatch(v, -1) {
					for _, a := range traefikArg.FindAllStringSubmatch(m[1], -1) {
						out = append(out, a[1]+a[2])
					}
				}
			case k == "com.patrolbox.host":
				out = append(out, strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })...)
			}
		}
	}
	return out
}
