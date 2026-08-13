package app

import (
	"context"
	"errors"
	"testing"
)

type memoryUpdates struct {
	seen     map[int64]bool
	outcomes map[int64]string
}

func (m *memoryUpdates) Begin(_ context.Context, updateID int64, _ []byte) (bool, error) {
	if m.seen[updateID] {
		return false, nil
	}
	m.seen[updateID] = true
	return true, nil
}

func (m *memoryUpdates) Finish(_ context.Context, updateID int64, outcome string) error {
	m.outcomes[updateID] = outcome
	return nil
}

type memoryMessenger struct {
	chatID int64
	text   string
	err    error
}

type testPresenter struct{}

func (testPresenter) Text(reply Reply) string { return string(reply) }

func (m *memoryMessenger) SendText(_ context.Context, chatID int64, text string) error {
	m.chatID = chatID
	m.text = text
	return m.err
}

func TestMessageHandlerStartAndDuplicate(t *testing.T) {
	updates := &memoryUpdates{seen: make(map[int64]bool), outcomes: make(map[int64]string)}
	messenger := &memoryMessenger{}
	handler := NewMessageHandler(messenger, updates, testPresenter{})

	result, err := handler.Process(context.Background(), IncomingMessage{UpdateID: 10, ChatID: 20, ChatType: "private", Text: "/start"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Duplicate || messenger.chatID != 20 || messenger.text == "" || updates.outcomes[10] != "DELIVERED" {
		t.Fatalf("unexpected first result: %+v, message %q, outcome %q", result, messenger.text, updates.outcomes[10])
	}

	result, err = handler.Process(context.Background(), IncomingMessage{UpdateID: 10, ChatID: 20, ChatType: "private", Text: "/start"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Duplicate {
		t.Fatal("duplicate update was processed again")
	}
}

func TestMessageHandlerRecordsUnknownDeliveryOutcome(t *testing.T) {
	updates := &memoryUpdates{seen: make(map[int64]bool), outcomes: make(map[int64]string)}
	messenger := &memoryMessenger{err: errors.New("connection lost")}
	handler := NewMessageHandler(messenger, updates, testPresenter{})

	result, err := handler.Process(context.Background(), IncomingMessage{UpdateID: 11, ChatID: 20, ChatType: "private", Text: "/status"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DeliveryFailed || updates.outcomes[11] != "OUTCOME_UNKNOWN" {
		t.Fatalf("unexpected result: %+v, outcome %q", result, updates.outcomes[11])
	}
}

func TestResponseForUnknownCommand(t *testing.T) {
	reply := replyFor(IncomingMessage{ChatType: "private", Text: "/unknown@realwallet_bot"})
	if reply != ReplyUnknownCommand {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestGroupCommandIsIgnored(t *testing.T) {
	reply := replyFor(IncomingMessage{ChatType: "group", Text: "/start secret-invite"})
	if reply != ReplyNone {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestPlainGroupMessageIsIgnored(t *testing.T) {
	reply := replyFor(IncomingMessage{ChatType: "group", Text: "обычное сообщение"})
	if reply != ReplyNone {
		t.Fatalf("unexpected reply: %q", reply)
	}
}
