// Package i18n translates message keys. Every language lives in
// locales/<lang>/*.json; all files of a language are merged, so each package
// can keep its own fragment. English is the fallback for missing keys.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

//go:embed locales
var files embed.FS

var catalog = map[string]map[string]string{}

func init() {
	langs, _ := files.ReadDir("locales")
	for _, l := range langs {
		if !l.IsDir() {
			continue
		}
		m := map[string]string{}
		frags, _ := files.ReadDir(path.Join("locales", l.Name()))
		for _, f := range frags {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			raw, err := files.ReadFile(path.Join("locales", l.Name(), f.Name()))
			if err != nil {
				continue
			}
			var part map[string]string
			if err := json.Unmarshal(raw, &part); err != nil {
				panic(fmt.Sprintf("i18n: locales/%s/%s: %v", l.Name(), f.Name(), err))
			}
			for k, v := range part {
				m[k] = v
			}
		}
		catalog[l.Name()] = m
	}
}

// Languages lists the available language codes, sorted.
func Languages() []string {
	out := make([]string, 0, len(catalog))
	for l := range catalog {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Keys returns every key of a language (used by tests to compare catalogs).
func Keys(lang string) []string {
	out := make([]string, 0, len(catalog[lang]))
	for k := range catalog[lang] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Resolve picks a supported language: the explicit choice, else the
// environment (LC_ALL, LC_MESSAGES, LANG), else English.
func Resolve(explicit string) string {
	for _, c := range []string{explicit, os.Getenv("PATROLBOX_LANG"), os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")} {
		c = strings.ToLower(c)
		if i := strings.IndexAny(c, "_.-@"); i > 0 {
			c = c[:i]
		}
		if _, ok := catalog[c]; ok {
			return c
		}
	}
	return "en"
}

// T renders key in lang, replacing {name} placeholders from args.
// A missing key falls back to English, then to the key itself.
func T(lang, key string, args map[string]any) string {
	s, ok := catalog[lang][key]
	if !ok {
		if s, ok = catalog["en"][key]; !ok {
			s = key
		}
	}
	if len(args) == 0 || !strings.Contains(s, "{") {
		return s
	}
	pairs := make([]string, 0, len(args)*2)
	for k, v := range args {
		pairs = append(pairs, "{"+k+"}", fmt.Sprint(v))
	}
	return strings.NewReplacer(pairs...).Replace(s)
}
