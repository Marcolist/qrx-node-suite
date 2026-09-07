package telegram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qrx-node-suite/agent/telegram"
)

func TestSendMessageSuccess(t *testing.T) {
	var gotBody map[string]string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := &telegram.Client{Token: "TESTTOKEN", ChatID: "12345", BaseURL: srv.URL}
	if err := c.SendMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if gotPath != "/botTESTTOKEN/sendMessage" {
		t.Errorf("path = %q, want /botTESTTOKEN/sendMessage", gotPath)
	}
	if gotBody["chat_id"] != "12345" || gotBody["text"] != "hello" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestSendMessageAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "chat not found"})
	}))
	defer srv.Close()

	c := &telegram.Client{Token: "T", ChatID: "bad", BaseURL: srv.URL}
	err := c.SendMessage(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected an error mentioning the API's description, got %v", err)
	}
}

func TestSendMessageTruncatesLongText(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := &telegram.Client{Token: "T", ChatID: "1", BaseURL: srv.URL}
	longText := strings.Repeat("a", 5000)
	if err := c.SendMessage(context.Background(), longText); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(gotBody["text"]) > 4096 {
		t.Errorf("sent text length = %d, want <= 4096", len(gotBody["text"]))
	}
	if !strings.Contains(gotBody["text"], "truncated") {
		t.Error("expected a truncation marker in the sent text")
	}
}

func TestFormatAlert(t *testing.T) {
	got := telegram.FormatAlert("critical", "QRX node offline", "process not running")
	want := "[CRITICAL] QRX node offline\nprocess not running"
	if got != want {
		t.Errorf("FormatAlert = %q, want %q", got, want)
	}
}
