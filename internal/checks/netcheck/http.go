package netcheck

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
	"github.com/ferhalemre/patrolbox/internal/core"
)

const userAgent = "patrolbox/1 (+https://github.com/ferhalemre/patrolbox)"

// HTTP requests every host and judges status code, speed and content.
func HTTP(ctx context.Context, cfg config.HTTP, hosts []string) []core.Result {
	return forEach(uniqueHosts(hosts), func(h string) []core.Result {
		return []core.Result{httpOne(ctx, cfg, h)}
	})
}

func httpOne(ctx context.Context, cfg config.HTTP, host string) core.Result {
	res := core.Result{Check: "http", Target: host}
	timeout := timeoutOr(cfg.Timeout.D(), 10*time.Second)
	maxCode := cfg.MaxCode
	if maxCode == 0 {
		maxCode = 399
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:       func(ctx context.Context, n, a string) (net.Conn, error) { return dialContext(ctx, n, a) },
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // the TLS check owns certificate problems
			DisableKeepAlives: true,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errors.New("stopped after 5 redirects")
			}
			return nil
		},
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	resp, err := httpGet(ctx, client, "https://"+host+"/")
	if err != nil && tlsUnavailable(err) {
		resp, err = httpGet(ctx, client, "http://"+host+"/")
	}
	if err != nil {
		res.Took = time.Since(start)
		res.Status = core.Crit
		if isTimeout(err) {
			res.MsgKey = keyHTTPTimeout
			res.Args = map[string]any{"timeout": ms(timeout)}
		} else {
			res.MsgKey = keyHTTPError
			res.Args = map[string]any{"error": shortErr(err)}
		}
		return res
	}
	defer resp.Body.Close()
	took := time.Since(start)
	res.Took = took
	args := map[string]any{"code": resp.StatusCode, "ms": ms(took)}

	if resp.StatusCode > maxCode {
		args["max"] = maxCode
		res.Status, res.MsgKey, res.Args = core.Crit, keyHTTPBadStatus, args
		return res
	}
	if cfg.Contain != "" {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if !strings.Contains(string(body), cfg.Contain) {
			res.Status, res.MsgKey = core.Crit, keyHTTPMissingText
			res.Args = map[string]any{"code": resp.StatusCode, "text": cfg.Contain}
			return res
		}
	}
	if cfg.Slow.D() > 0 && took > cfg.Slow.D() {
		args["limit"] = ms(cfg.Slow.D())
		res.Status, res.MsgKey, res.Args = core.Warn, keyHTTPSlow, args
		return res
	}
	res.Status, res.MsgKey, res.Args = core.OK, keyHTTPOK, args
	return res
}

func httpGet(ctx context.Context, c *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return c.Do(req)
}

// tlsUnavailable reports whether the https attempt failed because nothing
// speaks TLS on that port (as opposed to a slow or broken site).
func tlsUnavailable(err error) bool {
	var rhe tls.RecordHeaderError
	if errors.As(err, &rhe) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(err.Error(), "server gave HTTP response")
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func shortErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Sprint(err)
}
