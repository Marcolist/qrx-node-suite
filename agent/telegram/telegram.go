// Package telegram implements a minimal Telegram Bot API client for alert
// notifications (docs/telegram.md). The Bot API is plain HTTPS+JSON, so
// this needs no SDK dependency (ADR-001) -- just net/http and
// encoding/json.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultBaseURL = "https://api.telegram.org"

// Client sends messages to a Telegram chat via a bot token. Never log or
// persist the token anywhere except the config it's read from
// (docs/architecture.md principle 12: no local secrets exposed).
type Client struct {
	Token      string
	ChatID     string
	HTTPClient *http.Client
	// BaseURL overrides the API base (default https://api.telegram.org) --
	// exists so tests can point this at an httptest.Server.
	BaseURL string
}

func New(token, chatID string) *Client {
	return &Client{Token: token, ChatID: chatID}
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

// SendMessage sends text to the configured chat. Telegram's 4096-character
// message limit is enforced by truncating with a marker, never by silently
// dropping the message.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	const maxLen = 4096
	if len(text) > maxLen {
		text = text[:maxLen-innerTruncationMarkerLen] + truncationMarker
	}
	body, err := json.Marshal(sendMessageRequest{ChatID: c.ChatID, Text: text})
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL(), c.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("telegram: request failed: %w", err)
	}
	defer resp.Body.Close()

	var apiResp apiResponse
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("telegram: unexpected response (status %s): %s", resp.Status, respBody)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram: API error: %s", apiResp.Description)
	}
	return nil
}

const truncationMarker = "\n... (truncated)"

var innerTruncationMarkerLen = len(truncationMarker)

// FormatAlert renders a models.Alert-shaped notification. Kept as a plain
// function (not depending on agent/models to avoid import direction
// assumptions) -- callers pass already-extracted fields.
func FormatAlert(severity, title, message string) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(strings.ToUpper(severity))
	b.WriteString("] ")
	b.WriteString(title)
	if message != "" {
		b.WriteString("\n")
		b.WriteString(message)
	}
	return b.String()
}
