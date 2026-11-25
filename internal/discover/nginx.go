package discover

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ferhalemre/patrolbox/internal/core"
)

var nginxAuto = []string{
	"/etc/nginx/nginx.conf",
	"/etc/nginx/conf.d/*.conf",
	"/etc/nginx/sites-enabled/*",
	"/usr/local/nginx/conf/nginx.conf",
	"/opt/*/nginx/*.conf",
	"/opt/*/nginx/conf.d/*.conf",
}

type ndir struct {
	name     string
	args     []string
	children []ndir
}

// Nginx returns the sites of nginx configs (path "auto", a file or a glob).
func Nginx(path string) []core.Site {
	visited := map[string]bool{}
	ssl := map[string]bool{}
	var order []string
	for _, f := range expandPaths(path, nginxAuto) {
		dirs := parseNginxFile(f, 0, visited)
		collectNginx(dirs, ssl, &order)
	}
	var raw []string
	for _, n := range order {
		if ssl[n] {
			raw = append(raw, n)
		}
	}
	return collect("nginx", raw)
}

func collectNginx(dirs []ndir, ssl map[string]bool, order *[]string) {
	for _, d := range dirs {
		if d.name != "server" || d.children == nil {
			collectNginx(d.children, ssl, order)
			continue
		}
		var names []string
		hasSSL := false
		for _, c := range d.children {
			switch c.name {
			case "server_name":
				names = append(names, c.args...)
			case "listen":
				if listenIsSSL(c.args) {
					hasSSL = true
				}
			case "ssl":
				if len(c.args) > 0 && c.args[0] == "on" {
					hasSSL = true
				}
			}
		}
		for _, n := range names {
			n = strings.ToLower(n)
			if n == "_" || n == "" || strings.HasPrefix(n, "~") || strings.ContainsAny(n, "*$") {
				continue
			}
			n = strings.TrimPrefix(n, ".")
			if _, seen := ssl[n]; !seen {
				*order = append(*order, n)
			}
			ssl[n] = ssl[n] || hasSSL
		}
		collectNginx(d.children, ssl, order) // nested server blocks do not exist, but stay safe
	}
}

func listenIsSSL(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, a := range args[1:] {
		if a == "ssl" {
			return true
		}
	}
	a := args[0]
	if i := strings.LastIndex(a, ":"); i >= 0 {
		a = a[i+1:]
	}
	return a == "443"
}

type ntok struct {
	s    string
	kind byte // 'w' word, ';', '{', '}'
}

func tokenizeNginx(src string) []ntok {
	var out []ntok
	rs := []rune(src)
	n := len(rs)
	for i := 0; i < n; {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '#':
			for i < n && rs[i] != '\n' {
				i++
			}
		case c == ';' || c == '{' || c == '}':
			out = append(out, ntok{string(c), byte(c)})
			i++
		case c == '"' || c == '\'':
			q := c
			i++
			var sb strings.Builder
			for i < n && rs[i] != q {
				if rs[i] == '\\' && i+1 < n {
					i++
				}
				sb.WriteRune(rs[i])
				i++
			}
			i++
			out = append(out, ntok{sb.String(), 'w'})
		default:
			j := i
			for j < n && !strings.ContainsRune(" \t\r\n;{}#", rs[j]) {
				j++
			}
			out = append(out, ntok{string(rs[i:j]), 'w'})
			i = j
		}
	}
	return out
}

// parseNginxFile parses a file into directives, expanding include inline.
func parseNginxFile(path string, depth int, visited map[string]bool) []ndir {
	if depth > maxImportDepth+3 {
		return nil
	}
	key := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		key = r
	}
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	if visited[key] {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	visited[key] = true
	toks := tokenizeNginx(string(b))
	pos := 0
	return parseNginxBlock(toks, &pos, filepath.Dir(path), depth, visited)
}

func parseNginxBlock(toks []ntok, pos *int, dir string, depth int, visited map[string]bool) []ndir {
	var out []ndir
	var words []string
	for *pos < len(toks) {
		t := toks[*pos]
		*pos++
		switch t.kind {
		case 'w':
			words = append(words, t.s)
		case ';':
			if len(words) > 0 {
				d := ndir{name: words[0], args: words[1:]}
				if d.name == "include" && len(d.args) > 0 {
					out = append(out, expandNginxInclude(d.args[0], dir, depth, visited)...)
				} else {
					out = append(out, d)
				}
			}
			words = nil
		case '{':
			ch := parseNginxBlock(toks, pos, dir, depth, visited)
			if ch == nil {
				ch = []ndir{}
			}
			if len(words) > 0 {
				out = append(out, ndir{name: words[0], args: words[1:], children: ch})
			} else {
				out = append(out, ch...)
			}
			words = nil
		case '}':
			return out
		}
	}
	return out
}

func expandNginxInclude(arg, dir string, depth int, visited map[string]bool) []ndir {
	if strings.ContainsAny(arg, "$") {
		return nil
	}
	var pats []string
	if filepath.IsAbs(arg) {
		pats = []string{arg}
	} else {
		pats = []string{filepath.Join(dir, arg), filepath.Join("/etc/nginx", arg)}
	}
	var out []ndir
	for _, p := range pats {
		files, err := globFiles(p)
		if err != nil || len(files) == 0 {
			continue
		}
		for _, f := range files {
			out = append(out, parseNginxFile(f, depth+1, visited)...)
		}
		break
	}
	return out
}
