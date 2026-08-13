package telegram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

type Update struct {
	UpdateID      int64    `json:"update_id"`
	Message       *Message `json:"message,omitempty"`
	PayloadDigest []byte   `json:"-"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text,omitempty"`
	Chat      Chat   `json:"chat"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type apiResponse[T any] struct {
	OK     bool `json:"ok"`
	Result T    `json:"result"`
}

func NewClient(token string, timeout time.Duration) (*Client, error) {
	return newClient("https://api.telegram.org", token, timeout)
}

func newClient(baseURL, token string, timeout time.Duration) (*Client, error) {
	if !validToken(token) {
		return nil, errors.New("invalid Telegram bot token format")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid Telegram API base URL")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: &http.Client{Timeout: timeout}}, nil
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	return call[User](ctx, c, "getMe", struct{}{})
}

func (c *Client) DeleteWebhook(ctx context.Context) error {
	_, err := call[bool](ctx, c, "deleteWebhook", struct {
		DropPendingUpdates bool `json:"drop_pending_updates"`
	}{DropPendingUpdates: false})
	return err
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	rawUpdates, err := call[[]json.RawMessage](ctx, c, "getUpdates", struct {
		Offset         int64    `json:"offset"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates"`
	}{Offset: offset, Timeout: seconds, AllowedUpdates: []string{"message"}})
	if err != nil {
		return nil, err
	}
	updates := make([]Update, 0, len(rawUpdates))
	for _, raw := range rawUpdates {
		var update Update
		if err = json.Unmarshal(raw, &update); err != nil {
			return nil, errors.New("decode Telegram update")
		}
		digest := sha256.Sum256(raw)
		update.PayloadDigest = digest[:]
		updates = append(updates, update)
	}
	return updates, nil
}

func (c *Client) SendText(ctx context.Context, chatID int64, text string) error {
	_, err := call[Message](ctx, c, "sendMessage", struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}{ChatID: chatID, Text: text})
	return err
}

func call[T any](ctx context.Context, client *Client, method string, payload any) (T, error) {
	var zero T
	body, err := json.Marshal(payload)
	if err != nil {
		return zero, errors.New("encode Telegram request")
	}
	endpoint := client.baseURL + "/bot" + client.token + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return zero, errors.New("create Telegram request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return zero, errors.New("telegram request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return zero, fmt.Errorf("telegram API returned HTTP %d", response.StatusCode)
	}
	var decoded apiResponse[T]
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err = decoder.Decode(&decoded); err != nil {
		return zero, errors.New("decode Telegram response")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return zero, errors.New("telegram response contains trailing data")
	}
	if !decoded.OK {
		return zero, errors.New("telegram API rejected request")
	}
	return decoded.Result, nil
}

func validToken(token string) bool {
	prefix, secret, ok := strings.Cut(strings.TrimSpace(token), ":")
	if !ok || len(secret) < 20 || strings.ContainsAny(secret, "/\\ 	\r\n") {
		return false
	}
	_, err := strconv.ParseInt(prefix, 10, 64)
	return err == nil
}
