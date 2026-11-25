package netcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

func domainCfg() config.Domain {
	return config.Domain{Enabled: true, Timeout: config.Duration(2 * time.Second), WarnDays: 30, CritDays: 7}
}

// fakeRDAP serves a bootstrap file (com -> this server) and RDAP answers.
func fakeRDAP(t *testing.T, expiry map[string]time.Time) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dns.json":
			json.NewEncoder(w).Encode(map[string]any{
				"services": [][][]string{{{"com"}, {srv.URL + "/rdap"}}},
			})
		case strings.HasPrefix(r.URL.Path, "/rdap/domain/"):
			name := strings.TrimPrefix(r.URL.Path, "/rdap/domain/")
			switch name {
			case "fail.com":
				http.Error(w, "boom", 500)
			case "noexp.com":
				fmt.Fprint(w, `{"events":[{"eventAction":"registration","eventDate":"2020-01-01T00:00:00Z"}]}`)
			default:
				exp, ok := expiry[name]
				if !ok {
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"events":[{"eventAction":"registration","eventDate":"2000-01-01T00:00:00Z"},{"eventAction":"expiration","eventDate":%q}]}`, exp.UTC().Format(time.RFC3339))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	bootstrapOnce = sync.Once{}
	bootstrapMap, bootstrapErr = nil, nil
	oldURL := bootstrapURL
	bootstrapURL = srv.URL + "/dns.json"
	t.Cleanup(func() {
		bootstrapURL = oldURL
		bootstrapOnce = sync.Once{}
		bootstrapMap, bootstrapErr = nil, nil
	})
	return srv
}

func TestDomain(t *testing.T) {
	day := 24 * time.Hour
	now := time.Now()
	fakeRDAP(t, map[string]time.Time{
		"ok.com":      now.Add(200 * day),
		"soon.com":    now.Add(20*day + time.Hour),
		"crit.com":    now.Add(3*day + time.Hour),
		"expired.com": now.Add(-4*day - time.Hour),
	})
	hosts := []string{
		"www.ok.com", "ok.com", "api.ok.com:8443", "soon.com", "crit.com", "expired.com",
		"fail.com", "noexp.com", "foo.com.tr", "127.0.0.1", "localhost", "intranet", "::1", "",
	}
	rs := Domain(context.Background(), domainCfg(), hosts)
	got := map[string]core.Result{}
	for _, r := range rs {
		got[r.Target] = r
		if r.Check != "domain" {
			t.Errorf("check = %q", r.Check)
		}
	}
	want := map[string]struct {
		s core.Status
		k string
	}{
		"ok.com":      {core.OK, keyDomainOK},
		"soon.com":    {core.Warn, keyDomainExpiresSoon},
		"crit.com":    {core.Crit, keyDomainExpiresCritical},
		"expired.com": {core.Crit, keyDomainExpired},
		"fail.com":    {core.Unknown, keyDomainLookupFailed},
		"noexp.com":   {core.Unknown, keyDomainNoExpiry},
	}
	if len(got) != len(want) {
		t.Fatalf("targets: got %d (%v), want %d", len(got), rs, len(want))
	}
	for name, w := range want {
		r, ok := got[name]
		if !ok || r.Status != w.s || r.MsgKey != w.k {
			t.Errorf("%s: %+v, want %v %s", name, r, w.s, w.k)
		}
	}
	if d, _ := got["soon.com"].Args["days"].(int); d != 20 {
		t.Errorf("soon days = %v", got["soon.com"].Args)
	}
	if _, ok := got["ok.com"].Args["date"].(string); !ok {
		t.Errorf("missing date arg")
	}
	for i := 1; i < len(rs); i++ {
		if rs[i-1].Target > rs[i].Target {
			t.Fatalf("not sorted")
		}
	}
	// foo.com.tr: TLD without RDAP -> no result.
	if _, ok := got["foo.com.tr"]; ok {
		t.Errorf("tr domain should be skipped")
	}
}

func TestDomainBootstrapFailure(t *testing.T) {
	srv := fakeRDAP(t, nil)
	bootstrapURL = srv.URL + "/missing.json"
	r := one(t, Domain(context.Background(), domainCfg(), []string{"a.com"}))
	if r.Status != core.Unknown || r.MsgKey != keyDomainLookupFailed {
		t.Fatalf("unexpected: %+v", r)
	}
}
