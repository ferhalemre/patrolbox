// Package netcheck holds the network checks: HTTP, TLS certificate and
// domain registration expiry.
package netcheck

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ferhalemre/patrolbox/internal/core"
)

const workers = 8

// Test hooks.
var (
	dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
)

// forEach runs fn for every item with a bounded worker pool and returns the
// results sorted by Target.
func forEach(items []string, fn func(string) []core.Result) []core.Result {
	var (
		mu  sync.Mutex
		out []core.Result
		wg  sync.WaitGroup
	)
	ch := make(chan string)
	n := workers
	if len(items) < n {
		n = len(items)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range ch {
				rs := safe(fn, it)
				mu.Lock()
				out = append(out, rs...)
				mu.Unlock()
			}
		}()
	}
	for _, it := range items {
		ch <- it
	}
	close(ch)
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// safe turns a panic into nothing rather than crashing the whole run.
func safe(fn func(string) []core.Result, it string) (rs []core.Result) {
	defer func() {
		if recover() != nil {
			rs = nil
		}
	}()
	return fn(it)
}

func uniqueHosts(hosts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// splitHost separates "host" or "host:port"; port is "" when absent.
func splitHost(h string) (host, port string) {
	if hh, p, err := net.SplitHostPort(h); err == nil {
		return hh, p
	}
	return strings.Trim(h, "[]"), ""
}

func timeoutOr(d time.Duration, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// daysUntil returns whole days from now to t, rounded down (negative when past).
func daysUntil(t time.Time) int {
	d := time.Until(t).Hours() / 24
	if d < 0 {
		return -int(-d)
	}
	return int(d)
}

func ms(d time.Duration) int { return int(d / time.Millisecond) }
