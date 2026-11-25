// Package discover finds the sites hosted on this server without any
// configuration, by reading Caddy, nginx and Docker.
package discover

import (
	"net"
	"strings"
	"unicode"

	"github.com/ferhalemre/patrolbox/internal/core"
)

// normalize turns a site address into the form patrolbox watches
// ("host" or "host:port") and reports whether it is a public host that
// should answer over HTTPS.
func normalize(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", false
	}
	if strings.HasPrefix(s, "http://") {
		return "", false // explicitly http-only
	}
	s = strings.TrimPrefix(s, "https://")
	if strings.Contains(s, "://") {
		return "", false
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" || strings.ContainsAny(s, "{}$*~@ \t\"'`,;()") || strings.HasPrefix(s, "[") {
		return "", false
	}
	if strings.Count(s, ":") > 1 {
		return "", false // bare IPv6
	}
	host, port := s, ""
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host, port = s[:i], s[i+1:]
		for _, r := range port {
			if r < '0' || r > '9' {
				return "", false
			}
		}
		if port == "" {
			return "", false
		}
	}
	host = strings.TrimSuffix(host, ".")
	if !validHost(host) {
		return "", false
	}
	if port == "443" || port == "80" {
		port = ""
	}
	if port != "" {
		return host + ":" + port, true
	}
	return host, true
}

func validHost(h string) bool {
	if h == "" || !strings.Contains(h, ".") || net.ParseIP(h) != nil {
		return false
	}
	if h == "localhost" {
		return false
	}
	for _, suf := range []string{".local", ".localhost", ".internal", ".test", ".lan"} {
		if strings.HasSuffix(h, suf) {
			return false
		}
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if l == "" || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return false
		}
		for _, r := range l {
			if !(r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
				return false
			}
		}
	}
	// 1.2.3 style leftovers: a numeric top-level label is never a real TLD.
	last := labels[len(labels)-1]
	allDigits := true
	for _, r := range last {
		if r < '0' || r > '9' {
			allDigits = false
		}
	}
	return !allDigits
}

// collect normalises raw addresses, drops duplicates and tags them.
func collect(source string, raw []string) []core.Site {
	seen := map[string]bool{}
	var out []core.Site
	for _, r := range raw {
		h, ok := normalize(r)
		if !ok || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, core.Site{Host: h, Source: source})
	}
	return out
}

// expandPaths turns "auto" (using the given candidate globs) or a user path
// into the existing regular files it names.
func expandPaths(path string, auto []string) []string {
	pats := []string{path}
	if path == "auto" {
		pats = auto
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range pats {
		m, err := globFiles(p)
		if err != nil {
			continue
		}
		for _, f := range m {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}
