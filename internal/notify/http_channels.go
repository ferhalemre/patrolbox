package notify

import (
	"context"
	"encoding/json"
	"mime"
	"strings"
)

func postJSON(ctx context.Context, name, rawURL string, v any, hdr map[string]string, secrets ...string) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return postChecked(ctx, name, rawURL, "application/json", b, hdr, secrets...)
}

// webhook is the generic JSON webhook.
type webhook struct{ url string }

func (*webhook) Name() string { return "webhook" }

func (w *webhook) Send(ctx context.Context, m Message) error {
	fields := m.Fields
	if fields == nil {
		fields = map[string]any{}
	}
	return postJSON(ctx, "webhook", w.url, map[string]any{
		"title":  m.Title,
		"body":   m.Body,
		"level":  m.Level,
		"fields": fields,
	}, nil)
}

type slack struct{ url string }

func (*slack) Name() string { return "slack" }

func (s *slack) Send(ctx context.Context, m Message) error {
	text := "*" + strings.TrimSpace(m.Title) + "*\n" + m.Body
	return postJSON(ctx, "slack", s.url, map[string]string{"text": text}, nil)
}

type discord struct{ url string }

func (*discord) Name() string { return "discord" }

const discordLimit = 2000

func (d *discord) Send(ctx context.Context, m Message) error {
	head := "**" + clampTitle(m.Title) + "**\n"
	content := head + truncate(m.Body, discordLimit-runeLen(head))
	return postJSON(ctx, "discord", d.url, map[string]string{"content": content}, nil)
}

// ntfy posts to a full topic URL such as https://ntfy.sh/mytopic.
type ntfy struct{ url string }

func (*ntfy) Name() string { return "ntfy" }

func (n *ntfy) Send(ctx context.Context, m Message) error {
	title := strings.Join(strings.Fields(m.Title), " ")
	hdr := map[string]string{
		// RFC 2047 keeps the header ASCII-safe; ntfy decodes it.
		"Title":    mime.QEncoding.Encode("utf-8", title),
		"Priority": "default",
	}
	switch m.Level {
	case "problem":
		hdr["Priority"] = "high"
		hdr["Tags"] = "warning"
	case "recovered":
		hdr["Tags"] = "white_check_mark"
	}
	return postChecked(ctx, "ntfy", n.url, "text/plain; charset=utf-8", []byte(m.Body), hdr)
}
