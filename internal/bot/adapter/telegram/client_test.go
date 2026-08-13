package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientBotAPIFlow(t *testing.T) {
	const token = "123456789:abcdefghijklmnopqrstuvwxyz_ABC"
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		methods = append(methods, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, "/getMe"):
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"id":123456789,"is_bot":true,"username":"rw_test_bot"}}`))
		case strings.HasSuffix(request.URL.Path, "/getUpdates"):
			var body struct {
				Offset int64 `json:"offset"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Offset != 42 {
				t.Fatalf("unexpected getUpdates body: %+v, %v", body, err)
			}
			_, _ = writer.Write([]byte(`{"ok":true,"result":[{"update_id":42,"message":{"message_id":1,"text":"/start","chat":{"id":77,"type":"private"}}}]}`))
		case strings.HasSuffix(request.URL.Path, "/sendMessage"):
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"message_id":2,"chat":{"id":77,"type":"private"},"text":"ok"}}`))
		default:
			_, _ = writer.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer server.Close()

	client, err := newClient(server.URL, token, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.GetMe(context.Background())
	if err != nil || identity.Username != "rw_test_bot" {
		t.Fatalf("unexpected identity: %+v, %v", identity, err)
	}
	if err = client.DeleteWebhook(context.Background()); err != nil {
		t.Fatal(err)
	}
	updates, err := client.GetUpdates(context.Background(), 42, time.Second)
	if err != nil || len(updates) != 1 || updates[0].Message.Chat.ID != 77 || len(updates[0].PayloadDigest) != 32 {
		t.Fatalf("unexpected updates: %+v, %v", updates, err)
	}
	if err = client.SendText(context.Background(), 77, "ok"); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 4 {
		t.Fatalf("unexpected calls: %v", methods)
	}
}

func TestClientRejectsMalformedToken(t *testing.T) {
	if _, err := newClient("https://api.telegram.org", "bad/token", time.Second); err == nil {
		t.Fatal("malformed token must fail")
	}
}

func TestClientErrorDoesNotExposeToken(t *testing.T) {
	const token = "123456789:abcdefghijklmnopqrstuvwxyz_ABC"
	client, err := newClient("http://127.0.0.1:1", token, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetMe(context.Background())
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("unsafe error: %v", err)
	}
}
