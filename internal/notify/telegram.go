package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ferhalemre/patrolbox/internal/config"
)

// telegramAPI is the Bot API base URL; tests point it at httptest.
var telegramAPI = "https://api.telegram.org"

const telegramLimit = 4096

type telegram struct{ cfg config.Telegram }

func (*telegram) Name() string { return "telegram" }

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func (t *telegram) Send(ctx context.Context, m Message) error {
	token := t.cfg.BotToken
	if token == "" || t.cfg.ChatID == "" {
		return fmt.Errorf("telegram: bot_token and chat_id are both required")
	}
	title := clampTitle(m.Title)
	// The limit applies to the text after entity parsing, so measure the raw text.
	body := truncate(m.Body, telegramLimit-runeLen(title)-2)
	text := "<b>" + htmlEscaper.Replace(title) + "</b>\n\n" + htmlEscaper.Replace(body)

	payload, err := json.Marshal(map[string]any{
		"chat_id":                  t.cfg.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	u := telegramAPI + "/bot" + token + "/sendMessage"
	r, err := post(ctx, "telegram", u, "application/json", payload, nil, token)
	if err != nil {
		return err
	}
	var api struct {
		OK          *bool  `json:"ok"`
		Description string `json:"description"`
	}
	parsed := json.Unmarshal(r.body, &api) == nil
	if r.ok() && (!parsed || api.OK == nil || *api.OK) {
		return nil
	}
	if parsed && api.Description != "" {
		return &sendError{msg: fmt.Sprintf("telegram: POST %s: HTTP %d: %s",
			target(u), r.status, scrub(api.Description, []string{token}))}
	}
	return statusError("telegram", u, r, token)
}
