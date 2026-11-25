package netcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

// Test hooks.
var (
	bootstrapURL = "https://data.iana.org/rdap/dns.json"
	rdapClient   = &http.Client{}

	bootstrapOnce sync.Once
	bootstrapMap  map[string]string // tld -> RDAP base URL (with trailing slash)
	bootstrapErr  error
)

// Domain looks up the registration expiry of the registrable domain behind
// every host. TLDs without an RDAP service are skipped silently.
func Domain(ctx context.Context, cfg config.Domain, hosts []string) []core.Result {
	var names []string
	seen := map[string]bool{}
	for _, h := range hosts {
		name := registrable(h)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return forEach(names, func(name string) []core.Result {
		return domainOne(ctx, cfg, name)
	})
}

// registrable returns the eTLD+1 of a host, or "" for IPs, localhost and
// single-label names.
func registrable(h string) string {
	host, _ := splitHost(strings.TrimSpace(h))
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || host == "localhost" || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return ""
	}
	name, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return name
}

func domainOne(ctx context.Context, cfg config.Domain, name string) []core.Result {
	res := core.Result{Check: "domain", Target: name}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeoutOr(cfg.Timeout.D(), 10*time.Second))
	defer cancel()
	unknown := func(key string, err error) []core.Result {
		res.Took = time.Since(start)
		res.Status, res.MsgKey = core.Unknown, key
		if err != nil {
			res.Args = map[string]any{"error": shortErr(err)}
		}
		return []core.Result{res}
	}

	bootstrapOnce.Do(loadBootstrap)
	if bootstrapErr != nil {
		return unknown(keyDomainLookupFailed, bootstrapErr)
	}
	tld := name[strings.LastIndex(name, ".")+1:]
	base, ok := bootstrapMap[tld]
	if !ok {
		return nil // no RDAP for this TLD: not an error
	}

	exp, found, err := rdapExpiry(ctx, base, name)
	if err != nil {
		return unknown(keyDomainLookupFailed, err)
	}
	if !found {
		return unknown(keyDomainNoExpiry, nil)
	}
	days := daysUntil(exp)
	date := exp.UTC().Format("2006-01-02")
	args := map[string]any{"days": days, "date": date}
	res.Took = time.Since(start)
	res.Args = args
	switch {
	case days < 0:
		args["days"] = -days
		res.Status, res.MsgKey = core.Crit, keyDomainExpired
	case days <= cfg.CritDays:
		res.Status, res.MsgKey = core.Crit, keyDomainExpiresCritical
	case days <= cfg.WarnDays:
		res.Status, res.MsgKey = core.Warn, keyDomainExpiresSoon
	default:
		res.Status, res.MsgKey = core.OK, keyDomainOK
	}
	return []core.Result{res}
}

func loadBootstrap() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var doc struct {
		Services [][][]string `json:"services"`
	}
	if err := getJSON(ctx, bootstrapURL, &doc); err != nil {
		bootstrapErr = fmt.Errorf("RDAP bootstrap: %w", err)
		return
	}
	bootstrapMap = map[string]string{}
	for _, svc := range doc.Services {
		if len(svc) != 2 || len(svc[1]) == 0 {
			continue
		}
		base := svc[1][0]
		for _, u := range svc[1] {
			if strings.HasPrefix(u, "https://") {
				base = u
				break
			}
		}
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		for _, tld := range svc[0] {
			bootstrapMap[strings.ToLower(tld)] = base
		}
	}
}

func rdapExpiry(ctx context.Context, base, name string) (time.Time, bool, error) {
	var doc struct {
		Events []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
	}
	if err := getJSON(ctx, base+"domain/"+url.PathEscape(name), &doc); err != nil {
		return time.Time{}, false, err
	}
	for _, e := range doc.Events {
		if strings.EqualFold(e.Action, "expiration") {
			t, err := time.Parse(time.RFC3339, e.Date)
			if err != nil {
				return time.Time{}, false, fmt.Errorf("bad expiry date %q", e.Date)
			}
			return t, true, nil
		}
	}
	return time.Time{}, false, nil
}

func getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := rdapClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return errors.New("invalid JSON")
	}
	return nil
}
