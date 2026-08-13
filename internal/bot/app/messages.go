package app

import (
	"context"
	"fmt"
	"strings"
)

type IncomingMessage struct {
	UpdateID      int64
	ChatID        int64
	ChatType      string
	Text          string
	PayloadDigest []byte
}

type Messenger interface {
	SendText(ctx context.Context, chatID int64, text string) error
}

type UpdateStore interface {
	Begin(ctx context.Context, updateID int64, payloadDigest []byte) (bool, error)
	Finish(ctx context.Context, updateID int64, outcome string) error
}

type Reply string

const (
	ReplyNone            Reply = ""
	ReplyStart           Reply = "start"
	ReplyStartInvite     Reply = "start_invite"
	ReplyHelp            Reply = "help"
	ReplyStatus          Reply = "status"
	ReplyTextUnsupported Reply = "text_unsupported"
	ReplyUnknownCommand  Reply = "unknown_command"
)

type Presenter interface {
	Text(reply Reply) string
}

type ProcessResult struct {
	Duplicate      bool
	DeliveryFailed bool
}

type MessageHandler struct {
	messenger Messenger
	updates   UpdateStore
	presenter Presenter
}

func NewMessageHandler(messenger Messenger, updates UpdateStore, presenter Presenter) *MessageHandler {
	return &MessageHandler{messenger: messenger, updates: updates, presenter: presenter}
}

func (h *MessageHandler) Process(ctx context.Context, message IncomingMessage) (ProcessResult, error) {
	started, err := h.updates.Begin(ctx, message.UpdateID, message.PayloadDigest)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("begin Telegram update: %w", err)
	}
	if !started {
		return ProcessResult{Duplicate: true}, nil
	}

	response := h.presenter.Text(replyFor(message))
	outcome := "IGNORED"
	deliveryFailed := false
	if response != "" {
		outcome = "DELIVERED"
		if err = h.messenger.SendText(ctx, message.ChatID, response); err != nil {
			outcome = "OUTCOME_UNKNOWN"
			deliveryFailed = true
		}
	}
	if err = h.updates.Finish(ctx, message.UpdateID, outcome); err != nil {
		return ProcessResult{}, fmt.Errorf("finish Telegram update: %w", err)
	}
	return ProcessResult{DeliveryFailed: deliveryFailed}, nil
}

func replyFor(message IncomingMessage) Reply {
	if message.ChatType != "private" {
		return ReplyNone
	}

	command, argument := parseCommand(message.Text)
	switch command {
	case "start":
		if argument != "" {
			return ReplyStartInvite
		}
		return ReplyStart
	case "help":
		return ReplyHelp
	case "status":
		return ReplyStatus
	case "":
		return ReplyTextUnsupported
	default:
		return ReplyUnknownCommand
	}
}

func parseCommand(text string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", ""
	}
	command := strings.TrimPrefix(fields[0], "/")
	command, _, _ = strings.Cut(command, "@")
	argument := ""
	if len(fields) > 1 {
		argument = fields[1]
	}
	return strings.ToLower(command), argument
}
