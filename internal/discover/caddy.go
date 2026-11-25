package discover

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/core"
)

const maxImportDepth = 5

var caddyAuto = []string{
	"/etc/caddy/Caddyfile",
	"/opt/*/Caddyfile",
	"/opt/*/*/Caddyfile",
	"/srv/*/Caddyfile",
	"/root/*/Caddyfile",
	"/home/*/*/Caddyfile",
}

const caddyAdminURL = "http://localhost:2019/config/"

// Caddy returns the sites of Caddyfiles (path "auto", a file or a glob). In
// "auto" mode it also asks the Caddy admin API.
func Caddy(path string) []core.Site {
	var raw []string
	for _, f := range expandPaths(path, caddyAuto) {
		raw = append(raw, parseCaddyFile(f, 0, true, nil)...)
	}
	if path == "auto" {
		raw = append(raw, caddyAdmin(caddyAdminURL)...)
	}
	return collect("caddy", raw)
}

func caddyAdmin(url string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil
	}
	return caddyJSONHosts(b)
}

// caddyJSONHosts collects every match.host entry under
// apps.http.servers.*.routes of a Caddy JSON config.
func caddyJSONHosts(b []byte) []string {
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	apps, _ := doc["apps"].(map[string]any)
	httpApp, _ := apps["http"].(map[string]any)
	servers, _ := httpApp["servers"].(map[string]any)
	var out []string
	for _, s := range servers {
		srv, _ := s.(map[string]any)
		walkRoutes(srv["routes"], &out)
	}
	return out
}

func walkRoutes(v any, out *[]string) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			walkRoutes(e, out)
		}
	case map[string]any:
		if m, ok := t["match"].([]any); ok {
			for _, e := range m {
				if mo, ok := e.(map[string]any); ok {
					if hs, ok := mo["host"].([]any); ok {
						for _, h := range hs {
							if s, ok := h.(string); ok {
								*out = append(*out, s)
							}
						}
					}
				}
			}
		}
		for _, e := range t {
			walkRoutes(e, out)
		}
	}
}

type ctok struct {
	s      string
	quoted bool
}

// tokenizeCaddy splits a Caddyfile into lines of tokens. Comments are
// dropped, quotes removed, "\" at end of line joins lines.
func tokenizeCaddy(src string) [][]ctok {
	var lines [][]ctok
	var cur []ctok
	rs := []rune(src)
	n := len(rs)
	var heredoc string
	for i := 0; i < n; {
		c := rs[i]
		switch {
		case c == '\n':
			lines = append(lines, cur)
			cur = nil
			i++
			if heredoc != "" {
				// skip until a line equal to the marker
				for i < n {
					j := i
					for j < n && rs[j] != '\n' {
						j++
					}
					l := strings.TrimSpace(string(rs[i:j]))
					i = j
					if i < n {
						i++
					}
					if l == heredoc {
						break
					}
				}
				heredoc = ""
			}
		case c == '\\' && i+1 < n && (rs[i+1] == '\n' || (rs[i+1] == '\r' && i+2 < n && rs[i+2] == '\n')):
			for i < n && rs[i] != '\n' {
				i++
			}
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < n && rs[i] != '\n' {
				i++
			}
		case c == '"' || c == '`':
			q := c
			i++
			var sb strings.Builder
			for i < n && rs[i] != q {
				if q == '"' && rs[i] == '\\' && i+1 < n {
					i++
				}
				sb.WriteRune(rs[i])
				i++
			}
			i++
			cur = append(cur, ctok{sb.String(), true})
		default:
			j := i
			for j < n && !strings.ContainsRune(" \t\r\n", rs[j]) {
				j++
			}
			t := string(rs[i:j])
			i = j
			if strings.HasPrefix(t, "<<") && len(t) > 2 {
				heredoc = t[2:]
			}
			cur = append(cur, ctok{t, false})
		}
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

func isBrace(t ctok, b string) bool { return !t.quoted && t.s == b }

// parseCaddyFile returns the raw site addresses of one Caddyfile and of the
// files it imports. top is true for a file named by the user or discovery
// (only those may be a brace-less single-site file).
func parseCaddyFile(path string, depth int, top bool, stack []string) []string {
	if depth > maxImportDepth {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	for _, s := range stack {
		if s == abs {
			return nil
		}
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	stack = append(append([]string(nil), stack...), abs)
	lines := tokenizeCaddy(string(b))

	var out []string
	var pending []string
	depthB := 0
	sawBlock, started := false, false
	var single []string

	for _, ln := range lines {
		if len(ln) == 0 {
			continue
		}
		if depthB > 0 {
			for _, t := range ln {
				if isBrace(t, "{") {
					depthB++
				} else if isBrace(t, "}") {
					depthB--
				}
			}
			continue
		}
		if len(pending) == 0 && !ln[0].quoted && ln[0].s == "import" {
			started = true
			if len(ln) > 1 {
				for _, f := range resolveImport(ln[1].s, filepath.Dir(abs)) {
					out = append(out, parseCaddyFile(f, depth+1, false, stack)...)
				}
			}
			continue
		}
		if isBrace(ln[0], "}") {
			continue
		}
		last := ln[len(ln)-1]
		opens := isBrace(last, "{")
		body := ln
		if opens {
			body = ln[:len(ln)-1]
		}
		if !opens && !addressLike(body) {
			// a directive at top level: not an address line
			if len(pending) > 0 && !started {
				single = nonEmpty(strings.Fields(strings.Join(pending, " ")))
			}
			started = true
			pending = nil
			continue
		}
		for _, t := range body {
			if t.quoted {
				pending = append(pending, strings.ReplaceAll(t.s, ",", " "))
				continue
			}
			pending = append(pending, strings.Split(t.s, ",")...)
		}
		if opens {
			sawBlock = true
			started = true
			addrs := nonEmpty(strings.Fields(strings.Join(pending, " ")))
			pending = nil
			depthB = 1
			if len(addrs) == 0 || (len(addrs) == 1 && strings.HasPrefix(addrs[0], "(")) {
				continue // global options or snippet
			}
			out = append(out, addrs...)
		}
	}
	if len(pending) > 0 && !started {
		single = nonEmpty(strings.Fields(strings.Join(pending, " ")))
	}
	if top && !sawBlock {
		out = append(out, single...)
	}
	return out
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolveImport maps an import argument to existing files; snippet names
// and placeholders resolve to nothing.
func resolveImport(arg, dir string) []string {
	if arg == "" || strings.ContainsAny(arg, "{$") {
		return nil
	}
	p := arg
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	files, err := globFiles(p)
	if err != nil {
		return nil
	}
	return files
}

// globFiles expands a glob (or plain path) to regular files.
func globFiles(p string) ([]string, error) {
	m, err := filepath.Glob(p)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range m {
		if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
			out = append(out, f)
		}
	}
	return out, nil
}

// addressLike reports whether a top-level line can be a list of site
// addresses (its first token looks like a host, scheme or port).
func addressLike(ln []ctok) bool {
	if len(ln) == 0 {
		return false
	}
	f := ln[0].s
	return strings.ContainsAny(f, ".:") || strings.HasPrefix(f, "*") || strings.HasPrefix(f, "(") || strings.HasPrefix(f, "{$")
}
