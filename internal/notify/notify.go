// Package notify delivers patrolbox messages to the configured channels
// (Telegram, generic webhook, Slack, Discord, ntfy, e-mail). It only uses the
// standard library. Messages arrive already translated.
package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
)

// Message is already translated by the caller. Body is plain text with "\n" line breaks.
type Message struct {
	Title string
	Body  string
	// Level is "problem", "recovered" or "test"; channels may use it for colour/priority.
	Level string
	// Fields is the machine-readable form, sent as-is by the generic webhook.
	Fields map[string]any
}

type Channel interface {
	Name() string // "telegram", "webhook", "slack", "discord", "ntfy", "email"
	Send(ctx context.Context, m Message) error
}

// Channels returns one Channel per configured (non-empty) section of cfg, in a
// fixed order: telegram, webhook, slack, discord, ntfy, email. A section counts
// as configured when any of its fields is set, so a half-filled section shows up
// as a send error instead of being silently ignored.
func Channels(cfg config.Notify) []Channel {
	var out []Channel
	if cfg.Telegram.BotToken != "" || cfg.Telegram.ChatID != "" {
		out = append(out, &telegram{cfg: cfg.Telegram})
	}
	if cfg.Webhook.URL != "" {
		out = append(out, &webhook{url: cfg.Webhook.URL})
	}
	if cfg.Slack.URL != "" {
		out = append(out, &slack{url: cfg.Slack.URL})
	}
	if cfg.Discord.URL != "" {
		out = append(out, &discord{url: cfg.Discord.URL})
	}
	if cfg.Ntfy.URL != "" {
		out = append(out, &ntfy{url: cfg.Ntfy.URL})
	}
	e := cfg.Email
	if e.Host != "" || e.From != "" || len(e.To) > 0 {
		out = append(out, &email{cfg: e})
	}
	return out
}

// httpClient is shared by all HTTP channels.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// maxErrBody is how much of an error response is quoted in an error.
const maxErrBody = 300

// sendError reports a failed delivery. Its text never contains the request URL
// (only scheme://host) nor any of the secrets it was built with.
type sendError struct {
	msg   string
	inner error
}

func (e *sendError) Error() string { return e.msg }
func (e *sendError) Unwrap() error { return e.inner }

// target reduces a URL to scheme://host, which is all that is safe to report.
func target(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "?"
	}
	return u.Scheme + "://" + u.Host
}

func scrub(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}

// response is the outcome of a completed HTTP exchange.
type response struct {
	status int
	body   []byte // at most maxErrBody bytes
}

func (r response) ok() bool { return r.status >= 200 && r.status < 300 }

// post sends body to rawURL. A returned error is a transport error, already
// scrubbed. secrets are removed from any text that ends up in an error.
func post(ctx context.Context, name, rawURL, contentType string, body []byte, hdr map[string]string, secrets ...string) (response, error) {
	secrets = append(secrets, rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return response{}, wrapErr(name, rawURL, err, secrets)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "patrolbox")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return response{}, wrapErr(name, rawURL, err, secrets)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return response{status: resp.StatusCode, body: b}, nil
}

func wrapErr(name, rawURL string, err error, secrets []string) error {
	inner := err
	var ue *url.Error
	if errors.As(err, &ue) {
		inner = ue.Err // net/http puts the full URL in url.Error; its cause has none
	}
	return &sendError{
		msg:   fmt.Sprintf("%s: POST %s: %s", name, target(rawURL), scrub(inner.Error(), secrets)),
		inner: inner,
	}
}

// statusError builds the error for a non-2xx response.
func statusError(name, rawURL string, r response, secrets ...string) error {
	snippet := strings.Join(strings.Fields(strings.ToValidUTF8(string(r.body), "")), " ")
	msg := fmt.Sprintf("%s: POST %s: HTTP %d", name, target(rawURL), r.status)
	if snippet != "" {
		msg += ": " + scrub(snippet, append(secrets, rawURL))
	}
	return &sendError{msg: msg}
}

// postChecked is post plus the generic non-2xx handling.
func postChecked(ctx context.Context, name, rawURL, contentType string, body []byte, hdr map[string]string, secrets ...string) error {
	r, err := post(ctx, name, rawURL, contentType, body, hdr, secrets...)
	if err != nil {
		return err
	}
	if !r.ok() {
		return statusError(name, rawURL, r, secrets...)
	}
	return nil
}

// truncate shortens s to at most max runes, cutting on a line boundary when it
// can and ending with "…".
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	cut := string(r[:max-1])
	if r[max-1] != '\n' {
		if i := strings.LastIndex(cut, "\n"); i > 0 {
			cut = cut[:i]
		}
	}
	return cut + "…"
}

func runeLen(s string) int { return len([]rune(s)) }

// clampTitle keeps absurdly long titles from eating the whole size budget.
func clampTitle(t string) string { return truncate(strings.TrimSpace(t), 256) }
