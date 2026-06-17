package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

const telegramAPIBase = "https://api.telegram.org"

// TelegramNotifier posts incident notifications via the Telegram Bot API.
type TelegramNotifier struct {
	httpClient    *http.Client
	apiBase       string
	botToken      string
	defaultChatID string
	owners        map[string]config.BusinessOwner
	workHours     WorkHours
}

// NewTelegramNotifier creates a TelegramNotifier.
func NewTelegramNotifier(botToken, defaultChatID string, owners []config.BusinessOwner, wh WorkHours) *TelegramNotifier {
	return NewTelegramNotifierWithBaseURL(telegramAPIBase, botToken, defaultChatID, owners, wh)
}

// NewTelegramNotifierWithBaseURL is like NewTelegramNotifier but lets callers
// (notably tests) point at a different API base URL.
func NewTelegramNotifierWithBaseURL(apiBase, botToken, defaultChatID string, owners []config.BusinessOwner, wh WorkHours) *TelegramNotifier {
	idx := make(map[string]config.BusinessOwner, len(owners))
	for _, o := range owners {
		idx[o.Name] = o
	}
	return &TelegramNotifier{
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		apiBase:       apiBase,
		botToken:      botToken,
		defaultChatID: defaultChatID,
		owners:        idx,
		workHours:     wh,
	}
}

func (t *TelegramNotifier) Channel() string { return "telegram" }

func (t *TelegramNotifier) Notify(ctx context.Context, incident types.Incident) error {
	level := incident.Alert.Level
	if level == types.LevelP2 && !InWorkHours(t.workHours, time.Now()) {
		return nil
	}

	chatID := t.defaultChatID
	if level == types.LevelP2 {
		if owner, ok := t.owners[incident.Alert.BusinessLine]; ok && owner.TelegramID != "" {
			chatID = owner.TelegramID
		}
	}

	text := fmt.Sprintf("*%s %s*\nIncident `%s`\nService: %s\nNamespace: %s\nRoot cause: %s",
		escapeMarkdownV2(level), escapeMarkdownV2(incident.Alert.AlertName), escapeMarkdownV2(incident.ID),
		escapeMarkdownV2(incident.Alert.Service), escapeMarkdownV2(incident.Alert.Namespace), escapeMarkdownV2(incident.Alert.RootCauseHint))

	return t.send(ctx, tgSendMessage{ChatID: chatID, Text: text, ParseMode: "MarkdownV2"})
}

func (t *TelegramNotifier) Escalate(ctx context.Context, incident types.Incident) error {
	text := fmt.Sprintf("*Escalation: %s %s*\nIncident `%s` needs approval\\.\nService: %s",
		escapeMarkdownV2(incident.Alert.Level), escapeMarkdownV2(incident.Alert.AlertName),
		escapeMarkdownV2(incident.ID), escapeMarkdownV2(incident.Alert.Service))

	kb := tgInlineKeyboard{InlineKeyboard: [][]tgInlineButton{{
		{Text: "Approve", CallbackData: "approve:" + incident.ID},
		{Text: "Reject", CallbackData: "reject:" + incident.ID},
		{Text: "Snooze", CallbackData: "snooze:" + incident.ID},
	}}}

	return t.send(ctx, tgSendMessage{ChatID: t.defaultChatID, Text: text, ParseMode: "MarkdownV2", ReplyMarkup: &kb})
}

func (t *TelegramNotifier) send(ctx context.Context, msg tgSendMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshalling telegram message: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBase, t.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting to telegram: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}
	return nil
}

var mdV2Escaper = regexp.MustCompile(`([_*\[\]()~` + "`" + `>#+\-=|{}.!])`)

func escapeMarkdownV2(s string) string {
	return mdV2Escaper.ReplaceAllString(s, `\$1`)
}

type tgSendMessage struct {
	ChatID      string            `json:"chat_id"`
	Text        string            `json:"text"`
	ParseMode   string            `json:"parse_mode,omitempty"`
	ReplyMarkup *tgInlineKeyboard `json:"reply_markup,omitempty"`
}

type tgInlineKeyboard struct {
	InlineKeyboard [][]tgInlineButton `json:"inline_keyboard"`
}

type tgInlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}
