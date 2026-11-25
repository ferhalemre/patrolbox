package discover

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func hosts(ss []core.Site) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.Host)
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"Example.COM":              "example.com",
		"https://a.example.com/x":  "a.example.com",
		"a.example.com:443":        "a.example.com",
		"a.example.com:80":         "a.example.com",
		"a.example.com:8443":       "a.example.com:8443",
		"b.example.com.":           "b.example.com",
		"https://c.example.com:81": "c.example.com:81",
	}
	for in, want := range ok {
		if got, g := normalize(in); !g || got != want {
			t.Errorf("normalize(%q)=%q,%v want %q", in, got, g, want)
		}
	}
	for _, in := range []string{"", "*.example.com", "1.2.3.4", "[::1]", "::1", "localhost", "intranet", "a.local",
		"a.localhost", "a.internal", "a.test", "a.lan", "{$DOMAIN}", "$host.example.com", "{host}.example.com",
		"http://example.com", ":8080", "example.com:abc", "~^x$", "10.0.0.1:8080", "x.1"} {
		if got, g := normalize(in); g {
			t.Errorf("normalize(%q)=%q should be dropped", in, got)
		}
	}
}

func TestCaddyfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Caddyfile", `# top comment
{
	email admin@example.com
	servers {
		trusted_proxies static 1.2.3.4/20
	}
}

(snippet) {
	header X-Foo "bar.example.com"
}

a.example.com, b.example.com {
	import snippet
	reverse_proxy app:8080 {
		header_up Host c.example.com
	}
	@m path /x
	handle @m {
		respond "d.example.com"
	}
}

multi1.example.com,
multi2.example.com
multi3.example.com {
	respond "ok"
}

http://plain.example.com {
	respond "no tls"
}
*.wild.example.com {
	respond "w"
}
{$DOMAIN} {
	respond "p"
}
port.example.com:8443, https://scheme.example.com:443, default.example.com:80 {
}
"quoted.example.com" {
}
foo.example.com # trailing comment
{
}
import sites/*.caddy
import snippet
import missing/*
localhost {
}
10.0.0.5 {
}
:9000 {
}
`)
	write(t, dir, "sites/one.caddy", "imported.example.com {\n\trespond x\n}\nimport ../loop.caddy\n")
	write(t, dir, "loop.caddy", "loop.example.com {\n}\nimport sites/one.caddy\n")
	got := hosts(Caddy(filepath.Join(dir, "Caddyfile")))
	eq(t, got, "a.example.com", "b.example.com", "multi1.example.com", "multi2.example.com", "multi3.example.com",
		"port.example.com:8443", "scheme.example.com", "default.example.com", "quoted.example.com",
		"foo.example.com", "imported.example.com", "loop.example.com")
}

func TestCaddyfileSingleSite(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "Caddyfile", "# one site\nsingle.example.com\nreverse_proxy app:8080\nencode gzip\n")
	eq(t, hosts(Caddy(p)), "single.example.com")
	// directive-only fragments imported from a block file are not sites
	write(t, dir, "frag.conf", "respond hello.example.com\n")
	p2 := write(t, dir, "Caddyfile2", "x.example.com {\n}\nimport frag.conf\n")
	eq(t, hosts(Caddy(p2)), "x.example.com")
}

func TestCaddyGlobAndMissing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a/Caddyfile", "a.example.com {\n}\n")
	write(t, dir, "b/Caddyfile", "b.example.com {\n}\n")
	eq(t, hosts(Caddy(filepath.Join(dir, "*", "Caddyfile"))), "a.example.com", "b.example.com")
	if s := Caddy(filepath.Join(dir, "nope")); len(s) != 0 {
		t.Fatalf("want none, got %v", s)
	}
	for _, s := range Caddy(filepath.Join(dir, "a", "Caddyfile")) {
		if s.Source != "caddy" {
			t.Fatalf("source %q", s.Source)
		}
	}
}

const adminDoc = `{"apps":{"http":{"servers":{
 "srv0":{"routes":[
  {"match":[{"host":["api.example.com","*.wild.example.com"]}],"handle":[{"handler":"subroute","routes":[
     {"match":[{"host":["deep.example.com"]},{"path":["/x"]}],"handle":[{"handler":"reverse_proxy"}]}
  ]}]},
  {"handle":[{"handler":"subroute","routes":[{"match":[{"host":["nested.example.com"]}]}]}]}
 ]},
 "srv1":{"routes":[{"match":[{"host":["other.example.com","10.1.1.1"]}]}],
   "errors":{"routes":[{"match":[{"host":["err.example.com"]}]}]}}
}},"tls":{"automation":{"policies":[{"subjects":["tls.example.com"]}]}}}}`

func TestCaddyJSONHosts(t *testing.T) {
	got := collect("caddy", caddyJSONHosts([]byte(adminDoc)))
	eq(t, hosts(got), "api.example.com", "deep.example.com", "nested.example.com", "other.example.com")
	if caddyJSONHosts([]byte("garbage")) != nil || caddyJSONHosts([]byte(`{}`)) != nil {
		t.Fatal("expected nil")
	}
}

func TestNginx(t *testing.T) {
	dir := t.TempDir()
	conf := write(t, dir, "nginx.conf", `
user www;
http {
    include mime.types;
    # comment
    server {
        listen 443 ssl http2;
        listen [::]:443 ssl;
        server_name  Ssl.example.com   alias.example.com "quoted.example.com";
        location / { proxy_pass http://app; }
    }
    server {
        listen 80;
        server_name ssl.example.com plain.example.com;
        return 301 https://$host$request_uri;
    }
    server {
        listen 8443 ssl;
        server_name alt.example.com:8443 port.example.com;
    }
    server {
        listen 80 default_server;
        server_name _ ~^(?<sub>.+)\.regex\.com$ *.wild.example.com $var.example.com;
    }
    server {
        listen *:443;
        server_name .dot.example.com;
    }
    include conf.d/*.conf;
    include loop.conf;
}
`)
	write(t, dir, "conf.d/x.conf", "server {\n listen 443 ssl;\n server_name included.example.com;\n}\n")
	write(t, dir, "loop.conf", "include nginx.conf;\nserver { listen 443 ssl; server_name loop.example.com; }\n")
	got := hosts(Nginx(conf))
	eq(t, got, "ssl.example.com", "alias.example.com", "quoted.example.com", "port.example.com", "alt.example.com:8443",
		"dot.example.com", "included.example.com", "loop.example.com")
	if s := Nginx(filepath.Join(dir, "none.conf")); len(s) != 0 {
		t.Fatal("want none")
	}
	if Nginx(conf)[0].Source != "nginx" {
		t.Fatal("source")
	}
}

func dockerServer(t *testing.T, body string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func TestDocker(t *testing.T) {
	labels := []map[string]any{
		{"Labels": map[string]string{"caddy": "d1.example.com, d2.example.com", "caddy_1": "d3.example.com http://nohttp.example.com", "caddy.reverse_proxy": "{{upstreams 80}}"}},
		{"Labels": map[string]string{
			"traefik.http.routers.web.rule":                    "Host(`t1.example.com`, `t2.example.com`) && PathPrefix(`/api`) || Host(\"t3.example.com\")",
			"traefik.http.routers.re.rule":                     "HostRegexp(`{sub:.+}.re.example.com`)",
			"traefik.http.routers.mixed.rule":                  "HostRegexp(`^x$`) && Host(`t4.example.com`)",
			"traefik.http.services.s.loadbalancer.server.port": "80",
		}},
		{"Labels": map[string]string{"com.patrolbox.host": "p1.example.com,p2.example.com:8443"}},
		{"Labels": nil},
	}
	b, _ := json.Marshal(labels)
	sock := dockerServer(t, string(b))
	got := Docker(context.Background(), sock)
	eq(t, hosts(got), "d1.example.com", "d2.example.com", "d3.example.com", "t1.example.com", "t2.example.com",
		"t3.example.com", "t4.example.com", "p1.example.com", "p2.example.com:8443")
	if got[0].Source != "docker" {
		t.Fatal("source")
	}
	if Docker(context.Background(), filepath.Join(t.TempDir(), "missing.sock")) != nil {
		t.Fatal("missing socket must give nil")
	}
	if Docker(context.Background(), "") != nil {
		t.Fatal("empty socket must give nil")
	}
	bad := dockerServer(t, "not json")
	if Docker(context.Background(), bad) != nil {
		t.Fatal("bad json must give nil")
	}
}

func TestSites(t *testing.T) {
	dir := t.TempDir()
	cf := write(t, dir, "Caddyfile", "zeta.example.com, both.example.com {\n}\n")
	ng := write(t, dir, "n.conf", "server { listen 443 ssl; server_name both.example.com alpha.example.com nginxonly.example.com; }\n")
	b, _ := json.Marshal([]map[string]any{{"Labels": map[string]string{"caddy": "alpha.example.com dockeronly.example.com"}}})
	sock := dockerServer(t, string(b))

	got := Sites(context.Background(), config.Discover{Caddy: cf, Nginx: ng, Docker: true}, sock)
	want := []core.Site{
		{Host: "alpha.example.com", Source: "nginx"},
		{Host: "both.example.com", Source: "caddy"},
		{Host: "dockeronly.example.com", Source: "docker"},
		{Host: "nginxonly.example.com", Source: "nginx"},
		{Host: "zeta.example.com", Source: "caddy"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}

	got = Sites(context.Background(), config.Discover{Caddy: "off", Nginx: ng, Docker: false}, sock)
	eq(t, hosts(got), "alpha.example.com", "both.example.com", "nginxonly.example.com")
	got = Sites(context.Background(), config.Discover{Caddy: cf, Nginx: "off", Docker: true}, sock)
	eq(t, hosts(got), "alpha.example.com", "both.example.com", "dockeronly.example.com", "zeta.example.com")
	if got := Sites(context.Background(), config.Discover{Caddy: "off", Nginx: "off"}, sock); len(got) != 0 {
		t.Fatalf("want none: %v", got)
	}
}
