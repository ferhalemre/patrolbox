package netcheck

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

func tlsCfg() config.TLS {
	return config.TLS{Enabled: true, Timeout: config.Duration(2 * time.Second), WarnDays: 14, CritDays: 5}
}

func useRoots(t *testing.T, p *x509.CertPool) {
	t.Helper()
	old := rootCAs
	rootCAs = p
	t.Cleanup(func() { rootCAs = old })
}

func TestTLSStates(t *testing.T) {
	ca := newCA(t)
	useRoots(t, ca.pool)
	now := time.Now()
	day := 24 * time.Hour
	addrs := map[string]string{
		"ok.test:443":      ca.serve(t, "ok.test", now.Add(-day), now.Add(90*day)),
		"warn.test:443":    ca.serve(t, "warn.test", now.Add(-day), now.Add(10*day+time.Hour)),
		"crit.test:443":    ca.serve(t, "crit.test", now.Add(-day), now.Add(3*day+time.Hour)),
		"expired.test:443": ca.serve(t, "expired.test", now.Add(-30*day), now.Add(-2*day-time.Hour)),
		"wrong.test:443":   ca.serve(t, "other.test", now.Add(-day), now.Add(90*day)),
	}
	mapDial(t, addrs)

	cases := []struct {
		host   string
		status core.Status
		key    string
	}{
		{"ok.test", core.OK, keyTLSOK},
		{"warn.test", core.Warn, keyTLSExpiresSoon},
		{"crit.test", core.Crit, keyTLSExpiresCritical},
		{"expired.test", core.Crit, keyTLSExpired},
		{"wrong.test", core.Crit, keyTLSHostnameMismatch},
	}
	for _, c := range cases {
		r := one(t, TLS(context.Background(), tlsCfg(), []string{c.host}))
		if r.Status != c.status || r.MsgKey != c.key || r.Check != "tls" {
			t.Errorf("%s: unexpected %+v", c.host, r)
		}
	}
	r := one(t, TLS(context.Background(), tlsCfg(), []string{"ok.test"}))
	if r.Args["issuer"] != "Test CA" || r.Args["days"] != 89 && r.Args["days"] != 90 {
		t.Errorf("args: %+v", r.Args)
	}
	if d, _ := one(t, TLS(context.Background(), tlsCfg(), []string{"expired.test"})).Args["days"].(int); d != 2 {
		t.Errorf("expired days = %d, want 2", d)
	}
}

func TestTLSUntrusted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	useRoots(t, x509.NewCertPool()) // empty: nothing is trusted
	r := one(t, TLS(context.Background(), tlsCfg(), []string{hostOf(srv)}))
	if r.Status != core.Crit || r.MsgKey != keyTLSUntrusted {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestTLSHttptestTrusted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	useRoots(t, pool)
	r := one(t, TLS(context.Background(), tlsCfg(), []string{hostOf(srv)}))
	if r.Status != core.OK {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestTLSHandshakeFailure(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer plain.Close()
	r := one(t, TLS(context.Background(), tlsCfg(), []string{hostOf(plain)}))
	if r.Status != core.Crit || r.MsgKey != keyTLSHandshakeFailed {
		t.Fatalf("unexpected: %+v", r)
	}
	r = one(t, TLS(context.Background(), tlsCfg(), []string{"127.0.0.1:1"}))
	if r.Status != core.Crit || r.MsgKey != keyTLSHandshakeFailed {
		t.Fatalf("unexpected: %+v", r)
	}
}
