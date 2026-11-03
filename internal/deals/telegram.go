package deals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Notifier delivers a text message to the owner's phone.
type Notifier interface {
	Send(ctx context.Context, text string) error
}

var (
	botTokenRe = regexp.MustCompile(`^[0-9]{5,16}:[A-Za-z0-9_-]{30,64}$`)
	chatIDRe   = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31})$`)
)

// Bad settings. The messages never contain the values.
var (
	ErrBadToken  = errors.New("bad token format")
	ErrBadChatID = errors.New("bad chat id format")
)

// telegramAPI is where Telegram's Bot API is; tests point their client to their own server.
const telegramAPI = "https://api.telegram.org"

// Telegram sends messages with a bot to one chat. The token is part of the request address, so errors and logs never
// carry that address.
type Telegram struct {
	token, chatID string
	base          string // overridden in tests only
	hc            *http.Client
}

// NewTelegram checks the bot token and chat id (the formats Telegram uses). Both empty: off, with no error.
func NewTelegram(token, chatID string) (*Telegram, error) {
	token, chatID = strings.TrimSpace(token), strings.TrimSpace(chatID)
	if token == "" && chatID == "" {
		return nil, nil
	}
	if !botTokenRe.MatchString(token) {
		return nil, ErrBadToken
	}
	if !chatIDRe.MatchString(chatID) {
		return nil, ErrBadChatID
	}
	return &Telegram{token: token, chatID: chatID, base: telegramAPI, hc: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, // a redirect would take the token along
	}}, nil
}

// Send posts one message (no link preview).
func (t *Telegram) Send(ctx context.Context, text string) error {
	b, err := json.Marshal(map[string]any{"chat_id": t.chatID, "text": text, "disable_web_page_preview": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/bot"+t.token+"/sendMessage", bytes.NewReader(b))
	if err != nil {
		return errors.New("bad address")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // *url.Error carries the address
		}
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout") {
			return errors.New("Telegram did not answer within 10 s")
		}
		return errors.New("Telegram could not be reached")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("Telegram answered HTTP %d (a redirect, not followed)", resp.StatusCode)
	}
	var reply struct {
		Description string `json:"description"`
	}
	json.Unmarshal(data, &reply)
	if d := strings.TrimSpace(reply.Description); d != "" {
		if len(d) > 200 {
			d = d[:200] + "…"
		}
		return fmt.Errorf("Telegram answered HTTP %d: %s", resp.StatusCode, d)
	}
	return fmt.Errorf("Telegram answered HTTP %d", resp.StatusCode)
}
