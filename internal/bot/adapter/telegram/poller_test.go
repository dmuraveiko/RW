package telegram

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmuraveiko/RW/internal/bot/app"
)

type pollerStore struct {
	mu       sync.Mutex
	seen     map[int64]string
	finished chan struct{}
}

func (s *pollerStore) Begin(_ context.Context, updateID int64, _ []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[updateID]; ok {
		return false, nil
	}
	s.seen[updateID] = "PROCESSING"
	return true, nil
}

func (s *pollerStore) Finish(_ context.Context, updateID int64, outcome string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[updateID] = outcome
	select {
	case s.finished <- struct{}{}:
	default:
	}
	return nil
}

type pollerPresenter struct{}

func (pollerPresenter) Text(reply app.Reply) string { return string(reply) }

func TestPollerProcessesUpdateAndStops(t *testing.T) {
	const token = "123456789:abcdefghijklmnopqrstuvwxyz_ABC"
	delivered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, "/getMe"):
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"id":123456789,"is_bot":true,"username":"rw_test_bot"}}`))
		case strings.HasSuffix(request.URL.Path, "/deleteWebhook"):
			_, _ = writer.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(request.URL.Path, "/getUpdates"):
			_, _ = writer.Write([]byte(`{"ok":true,"result":[{"update_id":101,"message":{"message_id":1,"text":"/status","chat":{"id":77,"type":"private"}}}]}`))
		case strings.HasSuffix(request.URL.Path, "/sendMessage"):
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"message_id":2,"chat":{"id":77,"type":"private"},"text":"status"}}`))
			delivered <- struct{}{}
		}
	}))
	defer server.Close()

	client, err := newClient(server.URL, token, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store := &pollerStore{seen: make(map[int64]string), finished: make(chan struct{}, 1)}
	handler := app.NewMessageHandler(client, store, pollerPresenter{})
	ready := make(chan struct{}, 1)
	poller := NewPoller(client, handler, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 123456789, "rw_test_bot", func(User) { ready <- struct{}{} })
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- poller.Run(ctx) }()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not become ready")
	}
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not deliver response")
	}
	select {
	case <-store.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not persist response outcome")
	}
	cancel()
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.seen[101] != "DELIVERED" {
		t.Fatalf("unexpected update outcome: %q", store.seen[101])
	}
}
