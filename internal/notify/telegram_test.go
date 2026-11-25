package notify

import (
	"context"
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/ferhalemre/patrolbox/internal/config"
)

const testToken = "123456:AAH-secret_TOKEN"

func tg(t *testing.T, api string) Channel {
	t.Helper()
	old := telegramAPI
	telegramAPI = api
	t.Cleanup(func() { telegramAPI = old })
	return Channels(config.Notify{Telegram: config.Telegram{BotToken: testToken, ChatID: "-100500"}})[0]
}

func TestTelegramRequestAndEscaping(t *testing.T) {
	srv, got := capture(t, 200, `{"ok":true}`)
	ch := tg(t, srv.URL)
	err := ch.Send(context.Background(), Message{
		Title: "a<b> & c",
		Body:  "my_site.example.com <down> & \"slow\"\nsecond_line.",
		Level: "problem",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/bot"+testToken+"/sendMessage" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type %v", got.header)
	}
	m := decode(t, got.body)
	if m["chat_id"] != "-100500" || m["parse_mode"] != "HTML" {
		t.Fatalf("payload %v", m)
	}
	want := "<b>a&lt;b&gt; &amp; c</b>\n\nmy_site.example.com &lt;down&gt; &amp; \"slow\"\nsecond_line."
	if m["text"] != want {
		t.Fatalf("text = %q\nwant   %q", m["text"], want)
	}
}

func TestTelegramTruncation(t *testing.T) {
	srv, got := capture(t, 200, `{"ok":true}`)
	ch := tg(t, srv.URL)
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "<line %05d>\n", i)
	}
	if err := ch.Send(context.Background(), Message{Title: "Title", Body: sb.String()}); err != nil {
		t.Fatal(err)
	}
	text := html.UnescapeString(decode(t, got.body)["text"].(string))
	if n := len([]rune(text)); n > 4096 {
		t.Fatalf("text has %d chars", n)
	}
	if !strings.HasSuffix(text, "…") {
		t.Fatalf("no ellipsis: %q", text[len(text)-20:])
	}
	lines := strings.Split(strings.TrimSuffix(text, "…"), "\n")
	last := lines[len(lines)-1]
	if len(last) != len("<line 00000>") || !strings.HasSuffix(last, ">") {
		t.Fatalf("cut inside a line: %q", last)
	}

	// a short message is untouched
	if err := ch.Send(context.Background(), Message{Title: "T", Body: "short"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(decode(t, got.body)["text"].(string), "…") {
		t.Fatal("short message truncated")
	}
}

func TestTelegramErrorScrubsToken(t *testing.T) {
	// description even echoes the token: it must still not appear
	srv, _ := capture(t, 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found `+testToken+`"}`)
	ch := tg(t, srv.URL)
	err := ch.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("want error")
	}
	s := err.Error()
	if !strings.Contains(s, "chat not found") || !strings.Contains(s, "400") {
		t.Fatalf("description missing: %v", s)
	}
	if strings.Contains(s, testToken) || strings.Contains(s, "AAH-secret") {
		t.Fatalf("token leaked: %v", s)
	}

	// connection refused: net/http embeds the URL in its error
	srv.Close()
	err = ch.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "AAH-secret") || strings.Contains(err.Error(), "/bot") {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestTelegramNon2xxWithoutJSON(t *testing.T) {
	srv, _ := capture(t, 502, "<html>bad gateway</html>")
	ch := tg(t, srv.URL)
	err := ch.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "bad gateway") {
		t.Fatalf("err = %v", err)
	}
}
