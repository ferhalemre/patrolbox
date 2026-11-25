package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ferhalemre/patrolbox/internal/config"
)

type captured struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// capture starts a server answering status/reply and records the last request.
func capture(t *testing.T, status int, reply string) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.header = r.Method, r.URL.Path, r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, b)
	}
	return m
}

func TestChannelsOrderAndEmpty(t *testing.T) {
	if got := Channels(config.Notify{}); len(got) != 0 {
		t.Fatalf("empty config gave %d channels", len(got))
	}
	cfg := config.Notify{
		Email:    config.Email{Host: "smtp.example.com", To: []string{"a@b.c"}},
		Ntfy:     config.Webhook{URL: "https://ntfy.sh/x"},
		Discord:  config.Webhook{URL: "https://discord.com/api/webhooks/1/x"},
		Slack:    config.Webhook{URL: "https://hooks.slack.com/services/x"},
		Webhook:  config.Webhook{URL: "https://example.com/hook"},
		Telegram: config.Telegram{BotToken: "1:a", ChatID: "2"},
	}
	var names []string
	for _, c := range Channels(cfg) {
		names = append(names, c.Name())
	}
	want := "telegram,webhook,slack,discord,ntfy,email"
	if strings.Join(names, ",") != want {
		t.Fatalf("order = %v, want %s", names, want)
	}
	cfg.Slack.URL, cfg.Email = "", config.Email{}
	names = nil
	for _, c := range Channels(cfg) {
		names = append(names, c.Name())
	}
	if strings.Join(names, ",") != "telegram,webhook,discord,ntfy" {
		t.Fatalf("partial = %v", names)
	}
}

func TestWebhook(t *testing.T) {
	srv, got := capture(t, 200, "ok")
	ch := Channels(config.Notify{Webhook: config.Webhook{URL: srv.URL + "/hook"}})[0]
	err := ch.Send(context.Background(), Message{
		Title: "T", Body: "line1\nline2", Level: "problem",
		Fields: map[string]any{"check": "http", "n": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/hook" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if ct := got.header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	m := decode(t, got.body)
	if m["title"] != "T" || m["body"] != "line1\nline2" || m["level"] != "problem" {
		t.Fatalf("payload %v", m)
	}
	f := m["fields"].(map[string]any)
	if f["check"] != "http" || f["n"] != float64(2) {
		t.Fatalf("fields %v", f)
	}
}

func TestSlack(t *testing.T) {
	srv, got := capture(t, 200, "ok")
	ch := Channels(config.Notify{Slack: config.Webhook{URL: srv.URL + "/services/T/B/X"}})[0]
	if err := ch.Send(context.Background(), Message{Title: "Hi", Body: "a_b.c"}); err != nil {
		t.Fatal(err)
	}
	if got.path != "/services/T/B/X" || got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("%s %v", got.path, got.header)
	}
	if m := decode(t, got.body); m["text"] != "*Hi*\na_b.c" || len(m) != 1 {
		t.Fatalf("payload %v", m)
	}
}

func TestDiscordAndTruncation(t *testing.T) {
	srv, got := capture(t, 204, "")
	ch := Channels(config.Notify{Discord: config.Webhook{URL: srv.URL + "/api/webhooks/1/tok"}})[0]
	if err := ch.Send(context.Background(), Message{Title: "Hi", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if m := decode(t, got.body); m["content"] != "**Hi**\nbody" {
		t.Fatalf("payload %v", m)
	}
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("şatafatlı satır 0000\n")
	}
	if err := ch.Send(context.Background(), Message{Title: "Hi", Body: sb.String()}); err != nil {
		t.Fatal(err)
	}
	c := decode(t, got.body)["content"].(string)
	if n := len([]rune(c)); n > 2000 || n < 1900 {
		t.Fatalf("content length %d", n)
	}
	if !strings.HasSuffix(c, "satır 0000…") {
		t.Fatalf("not cut on a line boundary: %q", c[len(c)-40:])
	}
}

func TestNtfy(t *testing.T) {
	srv, got := capture(t, 200, "{}")
	ch := Channels(config.Notify{Ntfy: config.Webhook{URL: srv.URL + "/mytopic"}})[0]

	if err := ch.Send(context.Background(), Message{Title: "2 sorun — şimdi", Body: "gövde\nx", Level: "problem"}); err != nil {
		t.Fatal(err)
	}
	if got.path != "/mytopic" || got.header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%s %v", got.path, got.header)
	}
	if string(got.body) != "gövde\nx" {
		t.Fatalf("body %q", got.body)
	}
	title := got.header.Get("Title")
	if !strings.HasPrefix(title, "=?utf-8?q?") {
		t.Fatalf("title not RFC 2047 encoded: %q", title)
	}
	for _, r := range title {
		if r > 127 {
			t.Fatalf("non-ASCII in title %q", title)
		}
	}
	if got.header.Get("Priority") != "high" || got.header.Get("Tags") != "warning" {
		t.Fatalf("headers %v", got.header)
	}

	if err := ch.Send(context.Background(), Message{Title: "plain ascii", Body: "b", Level: "recovered"}); err != nil {
		t.Fatal(err)
	}
	if got.header.Get("Title") != "plain ascii" || got.header.Get("Priority") != "default" ||
		got.header.Get("Tags") != "white_check_mark" {
		t.Fatalf("headers %v", got.header)
	}
}

func TestHTTPErrorBodyAndSecretScrub(t *testing.T) {
	secretPath := "/services/SECRETPART"
	long := strings.Repeat("x", 1000)
	srv, _ := capture(t, 500, long)
	ch := Channels(config.Notify{Slack: config.Webhook{URL: srv.URL + secretPath}})[0]
	err := ch.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "SECRETPART") {
		t.Fatalf("secret leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 500") || strings.Count(err.Error(), "x") != 300 {
		t.Fatalf("want status and exactly 300 body bytes: %v", err)
	}

	// connection refused must not leak the URL path either
	srv.Close()
	err = ch.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil || strings.Contains(err.Error(), "SECRETPART") {
		t.Fatalf("refused: %v", err)
	}
}

func TestContextCancel(t *testing.T) {
	srv, _ := capture(t, 200, "")
	ch := Channels(config.Notify{Webhook: config.Webhook{URL: srv.URL}})[0]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ch.Send(ctx, Message{Title: "t"}); err == nil {
		t.Fatal("want error on cancelled context")
	}
}
