package config

import (
	"io"
	"path"
	"strings"
)

func stringsReader(s string) io.Reader { return strings.NewReader(s) }

// Match reports whether name matches any pattern. Patterns use shell
// wildcards ("*.example.com", "tmp-*") and compare case-insensitively.
func Match(patterns []string, name string) bool {
	name = strings.ToLower(name)
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == name {
			return true
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
