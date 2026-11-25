package netcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

func httpCfg() config.HTTP {
	return config.HTTP{
		Enabled: true,
		Timeout: config.Duration(2 * time.Second),
		Slow:    config.Duration(time.Second),
		MaxCode: 399,
	}
}

func hostOf(s *httptest.Server) string {
	return strings.TrimPrefix(strings.TrimPrefix(s.URL, "https://"), "http://")
}

func TestHTTPOK(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			w.WriteHeader(418)
			return
		}
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()
	cfg := httpCfg()
	cfg.Contain = "hello"
	r := one(t, HTTP(context.Background(), cfg, []string{hostOf(srv)}))
	if r.Status != core.OK || r.MsgKey != keyHTTPOK || r.Check != "http" || r.Args["code"] != 200 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPFakeHostAndRedirect(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	mapDial(t, map[string]string{"site.test:443": hostOf(srv)})
	r := one(t, HTTP(context.Background(), httpCfg(), []string{"site.test"}))
	if r.Status != core.OK {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPTooManyRedirects(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusFound)
	}))
	defer srv.Close()
	r := one(t, HTTP(context.Background(), httpCfg(), []string{hostOf(srv)}))
	if r.Status != core.Crit || r.MsgKey != keyHTTPError {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPSlow(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
	}))
	defer srv.Close()
	cfg := httpCfg()
	cfg.Slow = config.Duration(50 * time.Millisecond)
	r := one(t, HTTP(context.Background(), cfg, []string{hostOf(srv)}))
	if r.Status != core.Warn || r.MsgKey != keyHTTPSlow {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPTimeout(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	cfg := httpCfg()
	cfg.Timeout = config.Duration(100 * time.Millisecond)
	r := one(t, HTTP(context.Background(), cfg, []string{hostOf(srv)}))
	if r.Status != core.Crit || r.MsgKey != keyHTTPTimeout {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPBadStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	r := one(t, HTTP(context.Background(), httpCfg(), []string{hostOf(srv)}))
	if r.Status != core.Crit || r.MsgKey != keyHTTPBadStatus || r.Args["code"] != 500 {
		t.Fatalf("unexpected: %+v", r)
	}
	cfg := httpCfg()
	cfg.MaxCode = 599
	if r := one(t, HTTP(context.Background(), cfg, []string{hostOf(srv)})); r.Status != core.OK {
		t.Fatalf("max_code ignored: %+v", r)
	}
}

func TestHTTPMissingText(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Service Unavailable"))
	}))
	defer srv.Close()
	cfg := httpCfg()
	cfg.Contain = "Welcome"
	r := one(t, HTTP(context.Background(), cfg, []string{hostOf(srv)}))
	if r.Status != core.Crit || r.MsgKey != keyHTTPMissingText {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPPlainFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	r := one(t, HTTP(context.Background(), httpCfg(), []string{hostOf(srv)}))
	if r.Status != core.OK {
		t.Fatalf("expected plain http fallback to work: %+v", r)
	}
}

func TestHTTPConnectionRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	r := one(t, HTTP(context.Background(), httpCfg(), []string{addr}))
	if r.Status != core.Crit || r.MsgKey != keyHTTPError {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestHTTPSortedAndBadInput(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	rs := HTTP(context.Background(), httpCfg(), []string{"", hostOf(srv), "bad host name", "[::1", hostOf(srv)})
	for i := 1; i < len(rs); i++ {
		if rs[i-1].Target > rs[i].Target {
			t.Fatalf("not sorted: %+v", rs)
		}
	}
	if len(rs) != 3 {
		t.Fatalf("want 3 results (empty skipped, dup merged), got %d", len(rs))
	}
}
