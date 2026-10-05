package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

type Client struct {
	http *http.Client
	base string
}
type APIError struct {
	Code       int
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return "Telegram request failed" }
func NewClient(token string) (*Client, error) {
	if !regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`).MatchString(token) {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is required and must have a valid format")
	}
	return &Client{http: &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: "https://api.telegram.org/bot" + token + "/"}, nil
}
func (c *Client) call(ctx context.Context, method string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return errors.New("Telegram encoding failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("Telegram request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &APIError{}
	}
	defer response.Body.Close()
	// Never propagate URL-bearing transport errors or Telegram response descriptions.
	var envelope struct {
		OK         bool            `json:"ok"`
		Result     json.RawMessage `json:"result"`
		Code       int             `json:"error_code"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 || json.Unmarshal(data, &envelope) != nil {
		return &APIError{Code: response.StatusCode}
	}
	if response.StatusCode != 200 || !envelope.OK {
		code := envelope.Code
		if code == 0 {
			code = response.StatusCode
		}
		delay := envelope.Parameters.RetryAfter
		if delay < 0 {
			delay = 0
		}
		if delay > 3600 {
			delay = 3600
		}
		return &APIError{Code: code, RetryAfter: time.Duration(delay) * time.Second}
	}
	if output != nil && json.Unmarshal(envelope.Result, output) != nil {
		return errors.New("Telegram response invalid")
	}
	return nil
}
func (c *Client) SendMessage(ctx context.Context, m Outgoing) error {
	return c.call(ctx, "sendMessage", m, nil)
}
func (c *Client) AnswerCallback(ctx context.Context, id, text string) error {
	return c.call(ctx, "answerCallbackQuery", struct {
		ID   string `json:"callback_query_id"`
		Text string `json:"text"`
	}{id, text}, nil)
}
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var u User
	err := c.call(ctx, "getMe", struct{}{}, &u)
	return u, err
}
func (c *Client) Updates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", struct {
		Offset  int64    `json:"offset"`
		Timeout int      `json:"timeout"`
		Limit   int      `json:"limit"`
		Allowed []string `json:"allowed_updates"`
	}{offset, 25, 20, []string{"message", "callback_query"}}, &updates)
	return updates, err
}
func (c *Client) SetCommands(ctx context.Context) error {
	type command struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	commands := []command{{"start", "Начало работы"}, {"connect", "Подключить расширение"}, {"imports", "Список импортов"}, {"status", "Прогресс импорта"}, {"playlists", "Сохранённые коллекции"}, {"settings", "Подключения и отзыв доступа"}, {"help", "Справка"}}
	return c.call(ctx, "setMyCommands", struct {
		Commands []command         `json:"commands"`
		Scope    map[string]string `json:"scope"`
	}{commands, map[string]string{"type": "all_private_chats"}}, nil)
}
