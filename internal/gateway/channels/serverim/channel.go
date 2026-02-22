// Package serverim implements an IMChannel adapter for the Server酱 bot platform.
// API base: https://bot-go.apijia.cn/bot{TOKEN}/{method}
package serverim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mini-agent-go/internal/config"
	"mini-agent-go/internal/gateway"
)

const apiBase = "https://bot-go.apijia.cn"

// Channel is the Server酱 IMChannel implementation.
type Channel struct {
	accountID     string
	token         string
	mode          string // "webhook" (default) or "polling"
	webhookAddr   string
	webhookSecret string
	httpServer    *http.Server
	logger        *log.Logger
}

// New creates a Channel from config. accountID is derived from the token prefix if not set.
func New(cfg config.ChannelConfig) *Channel {
	accountID := cfg.AccountID
	if accountID == "" {
		// Token format: "<accountID>:<secret>" — e.g. "174:WS0-7jhJK..."
		if idx := strings.Index(cfg.Token, ":"); idx > 0 {
			accountID = cfg.Token[:idx]
		} else {
			accountID = "default"
		}
	}

	mode := cfg.Mode
	if mode == "" {
		mode = "webhook"
	}

	addr := cfg.WebhookAddr
	if addr == "" {
		addr = ":8088"
	}

	return &Channel{
		accountID:     accountID,
		token:         cfg.Token,
		mode:          mode,
		webhookAddr:   addr,
		webhookSecret: cfg.WebhookSecret,
		// default: discard all logs until SetLogger is called
		logger: log.New(io.Discard, "", 0),
	}
}

// SetLogger injects a file-based logger provided by the Gateway.
func (c *Channel) SetLogger(logger *log.Logger) {
	c.logger = logger
}

// Type returns the channel type identifier.
func (c *Channel) Type() string { return "serverim" }

// AccountID returns the bot account ID.
func (c *Channel) AccountID() string { return c.accountID }

// Start begins receiving messages, forwarding them to inbound.
// Blocks until ctx is cancelled.
func (c *Channel) Start(ctx context.Context, inbound chan<- *gateway.GatewayMessage) error {
	if c.mode == "polling" {
		return c.startPolling(ctx, inbound)
	}
	return c.startWebhook(ctx, inbound)
}

// Send delivers a reply to the user identified by msg.SenderID.
func (c *Channel) Send(ctx context.Context, msg *gateway.GatewayMessage) error {
	return c.sendMessage(ctx, msg.SenderID, msg.Content)
}

// Stop shuts down the HTTP server (webhook mode) gracefully.
func (c *Channel) Stop() error {
	if c.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.httpServer.Shutdown(ctx)
	}
	return nil
}

// ─── Webhook mode ────────────────────────────────────────────────────────────

func (c *Channel) startWebhook(ctx context.Context, inbound chan<- *gateway.GatewayMessage) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c.handleWebhook(w, r, inbound)
	})

	c.httpServer = &http.Server{
		Addr:    c.webhookAddr,
		Handler: mux,
	}

	c.logger.Printf("[serverim/%s] webhook server listening on %s", c.accountID, c.webhookAddr)

	errCh := make(chan error, 1)
	go func() { errCh <- c.httpServer.ListenAndServe() }()

	select {
	case <-ctx.Done():
		return c.httpServer.Shutdown(context.Background())
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (c *Channel) handleWebhook(w http.ResponseWriter, r *http.Request, inbound chan<- *gateway.GatewayMessage) {
	if c.webhookSecret != "" {
		if r.Header.Get("X-Sc3Bot-Webhook-Secret") != c.webhookSecret {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var update Update
	if err := json.Unmarshal(body, &update); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if update.Message.Text != "" {
		senderID := fmt.Sprintf("%d", update.Message.ChatID)
		c.logger.Printf("[serverim/%s] webhook msg from %s: %s", c.accountID, senderID, update.Message.Text)
		inbound <- &gateway.GatewayMessage{
			ChannelType: "serverim",
			AccountID:   c.accountID,
			SenderID:    senderID,
			Content:     update.Message.Text,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
}

// ─── Polling mode ─────────────────────────────────────────────────────────────

func (c *Channel) startPolling(ctx context.Context, inbound chan<- *gateway.GatewayMessage) error {
	c.logger.Printf("[serverim/%s] polling started", c.accountID)
	var offset int64

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		updates, err := c.getUpdates(ctx, 30, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.Printf("[serverim/%s] getUpdates error: %v", c.accountID, err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return nil
			}
			continue
		}

		for _, u := range updates {
			if u.Message.Text != "" {
				senderID := fmt.Sprintf("%d", u.Message.ChatID)
				c.logger.Printf("[serverim/%s] poll msg from %s: %s", c.accountID, senderID, u.Message.Text)
				select {
				case inbound <- &gateway.GatewayMessage{
					ChannelType: "serverim",
					AccountID:   c.accountID,
					SenderID:    senderID,
					Content:     u.Message.Text,
				}:
				case <-ctx.Done():
					return nil
				}
			}
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
		}
	}
}

// ─── API helpers ──────────────────────────────────────────────────────────────

// Update and Message mirror the Server酱 API response structures.
type Update struct {
	UpdateID int64   `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
}

type apiResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
}

func (c *Channel) apiURL(method string) string {
	return fmt.Sprintf("%s/bot%s/%s", apiBase, c.token, method)
}

func (c *Channel) doPost(ctx context.Context, method string, payload interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(method), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read full body first so we can show the complete response on error
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ar apiResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return nil, err
	}
	if !ar.OK {
		return nil, fmt.Errorf("API error: %s", string(respBody))
	}
	return ar.Result, nil
}

func (c *Channel) doGet(ctx context.Context, rawURL string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ar apiResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return nil, err
	}
	if !ar.OK {
		return nil, fmt.Errorf("API error: %s", string(respBody))
	}
	return ar.Result, nil
}

func (c *Channel) getUpdates(ctx context.Context, timeout int, offset int64) ([]Update, error) {
	url := fmt.Sprintf("%s/bot%s/getUpdates?timeout=%d&offset=%d", apiBase, c.token, timeout, offset)
	raw, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var updates []Update
	json.Unmarshal(raw, &updates) //nolint:errcheck
	return updates, nil
}

func (c *Channel) sendMessage(ctx context.Context, chatID, text string) error {
	// API expects chat_id as integer, not string
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat_id %q: %w", chatID, err)
	}
	payload := map[string]interface{}{
		"chat_id": chatIDInt,
		"text":    text,
	}
	_, err = c.doPost(ctx, "sendMessage", payload)
	return err
}
