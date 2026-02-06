package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	hc      *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		baseURL: fmt.Sprintf("https://api.telegram.org/bot%s", token),
		hc:      &http.Client{Timeout: 9 * time.Second},
	}
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func NewInlineKeyboardMarkup(rows [][]InlineKeyboardButton) InlineKeyboardMarkup {
	return InlineKeyboardMarkup{InlineKeyboard: rows}
}

type InlineQueryResult interface{}

type InlineQueryResultPhoto struct {
	Type        string                `json:"type"`
	ID          string                `json:"id"`
	PhotoURL    string                `json:"photo_url"`
	ThumbURL    string                `json:"thumb_url"`
	Caption     string                `json:"caption,omitempty"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type AnswerInlineQueryRequest struct {
	InlineQueryID string              `json:"inline_query_id"`
	Results       []InlineQueryResult `json:"results"`
	CacheTime     int                 `json:"cache_time,omitempty"`
	IsPersonal    bool                `json:"is_personal,omitempty"`
}

func (c *Client) AnswerInlineQuery(ctx context.Context, req AnswerInlineQueryRequest) error {
	return c.post(ctx, "/answerInlineQuery", req)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackQueryID string, text string) error {
	payload := map[string]any{"callback_query_id": callbackQueryID}
	if text != "" {
		payload["text"] = text
	}
	return c.post(ctx, "/answerCallbackQuery", payload)
}

func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	return c.post(ctx, "/deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID})
}

type SendMessageRequest struct {
	ChatID      int64                 `json:"chat_id"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) error {
	return c.post(ctx, "/sendMessage", req)
}

type EditMessageTextRequest struct {
	ChatID      int64                 `json:"chat_id"`
	MessageID   int                   `json:"message_id"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

func (c *Client) EditMessageText(ctx context.Context, req EditMessageTextRequest) error {
	return c.post(ctx, "/editMessageText", req)
}

type EditMessageReplyMarkupRequest struct {
	ChatID      int64                 `json:"chat_id"`
	MessageID   int                   `json:"message_id"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

func (c *Client) EditMessageReplyMarkup(ctx context.Context, req EditMessageReplyMarkupRequest) error {
	return c.post(ctx, "/editMessageReplyMarkup", req)
}

func (c *Client) CopyMessage(ctx context.Context, toChatID int64, fromChatID int64, messageID int) error {
	return c.post(ctx, "/copyMessage", map[string]any{"chat_id": toChatID, "from_chat_id": fromChatID, "message_id": messageID})
}

func (c *Client) post(ctx context.Context, method string, payload any) error {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("telegram api %s status %d: %s", method, resp.StatusCode, string(body))
	}
	return nil
}
