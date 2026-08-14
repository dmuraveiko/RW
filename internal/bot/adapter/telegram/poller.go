package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dmuraveiko/RW/internal/bot/app"
)

type Poller struct {
	client   *Client
	handler  *app.MessageHandler
	logger   *slog.Logger
	timeout  time.Duration
	botID    int64
	username string
	onReady  func(User)
}

func NewPoller(client *Client, handler *app.MessageHandler, logger *slog.Logger, timeout time.Duration, botID int64, username string, onReady func(User)) *Poller {
	return &Poller{client: client, handler: handler, logger: logger, timeout: timeout, botID: botID, username: strings.TrimPrefix(username, "@"), onReady: onReady}
}

func (p *Poller) Run(ctx context.Context) error {
	identityCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	identity, err := p.client.GetMe(identityCtx)
	if err != nil {
		return fmt.Errorf("verify Telegram bot identity: %w", err)
	}
	if !identity.IsBot || identity.Username == "" {
		return errors.New("telegram token does not belong to a valid bot")
	}
	if p.botID > 0 && identity.ID != p.botID {
		return errors.New("telegram bot ID does not match token")
	}
	if p.username != "" && !strings.EqualFold(identity.Username, p.username) {
		return errors.New("telegram bot username does not match token")
	}
	if err = p.client.DeleteWebhook(identityCtx); err != nil {
		return fmt.Errorf("prepare Telegram polling: %w", err)
	}
	p.logger.Info("Telegram polling started", "bot_id", identity.ID, "bot_username", identity.Username)
	if p.onReady != nil {
		p.onReady(identity)
	}

	var offset int64
	backoff := time.Second
	for {
		updates, pollErr := p.client.GetUpdates(ctx, offset, p.timeout)
		if pollErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			p.logger.Warn("Telegram polling failed", "retry_in", backoff.String())
			if !wait(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		for _, update := range updates {
			if update.UpdateID < offset {
				continue
			}
			if update.Message != nil {
				userID := update.Message.From.ID
				if userID == 0 && update.Message.Chat.Type == "private" {
					userID = update.Message.Chat.ID
				}
				displayLabel := strings.TrimSpace(update.Message.From.FirstName + " " + update.Message.From.LastName)
				result, processErr := p.handler.Process(ctx, app.IncomingMessage{UpdateID: update.UpdateID, BotID: identity.ID, UserID: userID, ChatID: update.Message.Chat.ID, ChatType: update.Message.Chat.Type, Text: update.Message.Text, DisplayLabel: displayLabel, PayloadDigest: update.PayloadDigest})
				if processErr != nil {
					return processErr
				}
				if result.DeliveryFailed {
					p.logger.Warn("Telegram response outcome is unknown", "update_id", update.UpdateID)
				}
			}
			offset = update.UpdateID + 1
		}
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
