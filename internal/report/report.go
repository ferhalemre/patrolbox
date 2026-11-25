// Package report turns results into terminal output, JSON and notification text.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ferhalemre/patrolbox/internal/core"
	"github.com/ferhalemre/patrolbox/internal/i18n"
	"github.com/ferhalemre/patrolbox/internal/notify"
	"github.com/ferhalemre/patrolbox/internal/state"
)

// Translate fills Message on every result and sorts them: problems first,
// then by check and target.
func Translate(lang string, rs []core.Result) []core.Result {
	for i := range rs {
		rs[i].Message = i18n.T(lang, rs[i].MsgKey, rs[i].Args)
	}
	rank := map[core.Status]int{core.Crit: 0, core.Unknown: 1, core.Warn: 2, core.OK: 3}
	sort.SliceStable(rs, func(a, b int) bool {
		if rank[rs[a].Status] != rank[rs[b].Status] {
			return rank[rs[a].Status] < rank[rs[b].Status]
		}
		if rs[a].Check != rs[b].Check {
			return rs[a].Check < rs[b].Check
		}
		return rs[a].Target < rs[b].Target
	})
	return rs
}

// Counts tallies results per status.
func Counts(rs []core.Result) map[string]any {
	c := map[core.Status]int{}
	for _, r := range rs {
		c[r.Status]++
	}
	return map[string]any{"ok": c[core.OK], "warn": c[core.Warn], "crit": c[core.Crit], "unknown": c[core.Unknown]}
}

var colors = map[core.Status]string{core.OK: "\033[32m", core.Warn: "\033[33m", core.Crit: "\033[31m", core.Unknown: "\033[35m"}

// Text prints an aligned table. quiet hides OK rows.
func Text(w io.Writer, lang string, rs []core.Result, quiet, color bool) {
	type row struct {
		st           core.Status
		a, b, c, msg string
	}
	var rows []row
	wa, wb, wc := 0, 0, 0
	for _, r := range rs {
		if quiet && r.Status == core.OK {
			continue
		}
		x := row{r.Status, i18n.T(lang, "status."+r.Status.String(), nil), i18n.T(lang, "check."+r.Check, nil), r.Target, r.Message}
		wa, wb, wc = max(wa, width(x.a)), max(wb, width(x.b)), max(wc, width(x.c))
		rows = append(rows, x)
	}
	for _, x := range rows {
		st := x.a + pad(wa-width(x.a))
		if color {
			st = colors[x.st] + st + "\033[0m"
		}
		fmt.Fprintf(w, "%s  %s%s  %s%s  %s\n", st, x.b, pad(wb-width(x.b)), x.c, pad(wc-width(x.c)), x.msg)
	}
	if len(rows) > 0 {
		fmt.Fprintln(w)
	}
	if core.Worst(rs) == core.OK {
		fmt.Fprintln(w, i18n.T(lang, "report.all_ok", map[string]any{"count": len(rs)}))
		return
	}
	fmt.Fprintln(w, i18n.T(lang, "report.summary", Counts(rs)))
}

// width counts runes; good enough for alignment outside East Asian wide text.
func width(s string) int { return len([]rune(s)) }

func pad(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// JSON prints the machine-readable report.
func JSON(w io.Writer, host string, sites []core.Site, rs []core.Result) error {
	type item struct {
		core.Result
		TookMS int64 `json:"took_ms"`
	}
	items := make([]item, len(rs))
	for i, r := range rs {
		items[i] = item{r, r.Took.Milliseconds()}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"host":    host,
		"status":  core.Worst(rs),
		"summary": Counts(rs),
		"sites":   nonNil(sites),
		"results": items,
	})
}

// Message builds the notification for a set of changes.
func Message(lang, host string, d state.Diff) notify.Message {
	var b strings.Builder
	section := func(key string, rs []core.Result) {
		if len(rs) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(i18n.T(lang, key, nil) + ":\n")
		for _, r := range rs {
			fmt.Fprintf(&b, "%s %s — %s: %s\n", icon(r.Status), i18n.T(lang, "check."+r.Check, nil), r.Target, r.Message)
		}
	}
	section("notify.section_new", d.New)
	section("notify.section_ongoing", d.Ongoing)
	section("notify.section_recovered", d.Recovered)

	problems := len(d.New) + len(d.Ongoing)
	m := notify.Message{
		Body:  strings.TrimRight(b.String(), "\n"),
		Level: "problem",
		Fields: map[string]any{
			"host": host, "new": d.New, "ongoing": d.Ongoing, "recovered": d.Recovered,
		},
	}
	if problems == 0 {
		m.Level = "recovered"
		m.Title = i18n.T(lang, "notify.title_recovered", map[string]any{"host": host})
	} else {
		m.Title = i18n.T(lang, "notify.title_problem", map[string]any{"host": host, "count": problems})
	}
	return m
}

func icon(s core.Status) string {
	switch s {
	case core.OK:
		return "✅"
	case core.Warn:
		return "⚠️"
	case core.Crit:
		return "🔴"
	default:
		return "❔"
	}
}

// nonNil makes an empty list print as [] instead of null.
func nonNil(s []core.Site) []core.Site {
	if s == nil {
		return []core.Site{}
	}
	return s
}
