package telegram

import "context"

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type Message struct {
	From *User  `json:"from"`
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}
type Callback struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}
type Update struct {
	ID       int64     `json:"update_id"`
	Message  *Message  `json:"message"`
	Callback *Callback `json:"callback_query"`
}
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
type Keyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}
type Outgoing struct {
	ChatID   int64     `json:"chat_id"`
	Text     string    `json:"text"`
	Keyboard *Keyboard `json:"reply_markup,omitempty"`
	Protect  bool      `json:"protect_content,omitempty"`
	// No parse_mode: titles and other untrusted metadata are plain text.
	LinkPreview struct {
		Disabled bool `json:"is_disabled"`
	} `json:"link_preview_options"`
}

// TelegramService is the outbound port used by handlers; polling is separate.
type TelegramService interface {
	SendMessage(context.Context, Outgoing) error
	AnswerCallback(context.Context, string, string) error
}
