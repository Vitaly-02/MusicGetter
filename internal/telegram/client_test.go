package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientWireProtocol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong transport")
		}
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/getUpdates":
			if !strings.Contains(string(body), `"offset":12`) || !strings.Contains(string(body), `"callback_query"`) {
				t.Error("missing polling parameters")
			}
			io.WriteString(w, `{"ok":true,"result":[{"update_id":12,"message":{"from":{"id":42},"chat":{"id":42,"type":"private"},"text":"/start"}}]}`)
		case "/sendMessage":
			if strings.Contains(string(body), "parse_mode") || !json.Valid(body) {
				t.Error("unsafe message")
			}
			io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
		case "/answerCallbackQuery":
			if !strings.Contains(string(body), `"callback_query_id":"cb"`) {
				t.Error("missing callback ID")
			}
			io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			t.Error("unexpected method")
		}
	}))
	defer srv.Close()
	client := &Client{http: srv.Client(), base: srv.URL + "/"}
	updates, err := client.Updates(context.Background(), 12)
	if err != nil || len(updates) != 1 || updates[0].Message.From.ID != 42 {
		t.Fatalf("decode: %v", err)
	}
	if err = client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "<b>plain</b>"}); err != nil {
		t.Fatal(err)
	}
	if err = client.AnswerCallback(context.Background(), "cb", ""); err != nil {
		t.Fatal(err)
	}
}
func TestClientSafeErrorsAndRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, `{"ok":false,"error_code":429,"description":"SECRET","parameters":{"retry_after":3}}`)
	}))
	defer srv.Close()
	client := &Client{http: srv.Client(), base: srv.URL + "/"}
	_, err := client.Updates(context.Background(), 0)
	var api *APIError
	if !errors.As(err, &api) || api.RetryAfter != 3*time.Second || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error %v", err)
	}
	client.base = "http://127.0.0.1:1/botSECRET/"
	_, err = client.GetMe(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("transport exposes token URL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.GetMe(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, token := range []string{"", "123:a/b", "123:a?b", "123:a\nb"} {
		if _, err = NewClient(token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}
