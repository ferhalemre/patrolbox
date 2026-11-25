package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

var placeholder = regexp.MustCompile(`\{[a-z_]+\}`)

func holes(s string) string {
	h := placeholder.FindAllString(s, -1)
	sort.Strings(h)
	return strings.Join(h, ",")
}

// Every language must have exactly the English keys, with the same placeholders.
func TestCatalogsMatchEnglish(t *testing.T) {
	en := catalog["en"]
	if len(en) == 0 {
		t.Fatal("no English catalog")
	}
	for lang, m := range catalog {
		if lang == "en" {
			continue
		}
		for k, v := range en {
			tr, ok := m[k]
			if !ok {
				t.Errorf("%s: missing key %q", lang, k)
				continue
			}
			if strings.TrimSpace(tr) == "" {
				t.Errorf("%s: empty translation for %q", lang, k)
			}
			if holes(tr) != holes(v) {
				t.Errorf("%s: %q placeholders %q, want %q", lang, k, holes(tr), holes(v))
			}
		}
		for k := range m {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: unknown key %q", lang, k)
			}
		}
	}
}

func TestT(t *testing.T) {
	got := T("en", "report.all_ok", map[string]any{"count": 7})
	if got != "Everything is fine: 7 checks passed." {
		t.Errorf("got %q", got)
	}
	if got := T("xx", "status.ok", nil); got != "OK" {
		t.Errorf("fallback to English failed: %q", got)
	}
	if got := T("en", "no.such.key", nil); got != "no.such.key" {
		t.Errorf("missing key should return the key, got %q", got)
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("PATROLBOX_LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := Resolve(""); got != "en" {
		t.Errorf("LANG=en_US.UTF-8 → %q", got)
	}
	if got := Resolve("zz"); got != "en" {
		t.Errorf("unsupported language should fall back to en, got %q", got)
	}
	if got := Resolve("EN-gb"); got != "en" {
		t.Errorf("EN-gb → %q", got)
	}
}
