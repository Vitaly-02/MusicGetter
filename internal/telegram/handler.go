package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"musicgetter/internal/domain"
)

type Users interface {
	EnsureTelegram(context.Context, int64) (domain.User, error)
}
type Pairings interface {
	Create(context.Context, domain.ID) (string, time.Time, error)
	RevokeAll(context.Context, domain.ID) error
}
type Queries interface {
	Imports(context.Context, domain.ID, string) ([]domain.ImportSummary, error)
	Status(context.Context, domain.ID, string) (domain.ImportSummary, error)
	Playlists(context.Context, domain.ID, string) ([]domain.CollectionSummary, error)
	Sessions(context.Context, domain.ID) (int, error)
}
type Handler struct {
	telegram TelegramService
	users    Users
	pairings Pairings
	queries  Queries
	username string
}

func NewHandler(t TelegramService, u Users, p Pairings, q Queries, username string) *Handler {
	return &Handler{t, u, p, q, username}
}

var menu = &Keyboard{Rows: [][]Button{
	{{Text: "Подключить расширение", Data: "connect"}},
	{{Text: "Импорты", Data: "imports"}, {Text: "Статус", Data: "status"}},
	{{Text: "Плейлисты", Data: "playlists"}, {Text: "Настройки", Data: "settings"}},
}}

const help = "/connect — подключить расширение\n/imports — список импортов\n/status [ID] — прогресс импорта\n/playlists — сохранённые коллекции\n/settings — подключения и отзыв доступа\n/help — справка"

func (h *Handler) Handle(ctx context.Context, update Update) error {
	var sender *User
	var chat Chat
	var command, arg string
	if cb := update.Callback; cb != nil {
		if cb.Message == nil {
			return h.telegram.AnswerCallback(ctx, cb.ID, "Откройте личный чат с ботом")
		}
		sender = &cb.From
		chat = cb.Message.Chat
		if sender.IsBot || sender.ID <= 0 || chat.Type != "private" || chat.ID != sender.ID {
			return h.telegram.AnswerCallback(ctx, cb.ID, "Действие недоступно")
		}
		// Always dismiss Telegram's callback spinner, even if the application fails.
		if err := h.telegram.AnswerCallback(ctx, cb.ID, ""); err != nil {
			return err
		}
		command, arg, _ = strings.Cut(cb.Data, ":")
	} else if m := update.Message; m != nil {
		sender = m.From
		chat = m.Chat
		fields := strings.Fields(m.Text)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
			return nil
		}
		command = strings.TrimPrefix(fields[0], "/")
		if name, mention, ok := strings.Cut(command, "@"); ok {
			if !strings.EqualFold(mention, h.username) {
				return nil
			}
			command = name
		}
		if len(fields) > 1 {
			arg = fields[1]
		}
	} else {
		return nil
	}
	if sender == nil || sender.IsBot || sender.ID <= 0 || chat.Type != "private" || chat.ID != sender.ID {
		return nil
	}
	if (command == "imports" || command == "status" || command == "playlists") && arg != "" && !validID(arg) {
		return h.send(ctx, chat.ID, "Некорректный ID. Используйте кнопки меню.", menu, false)
	}
	user, err := h.users.EnsureTelegram(ctx, sender.ID)
	if err != nil {
		return h.failure(ctx, chat.ID, err)
	}
	text, keyboard, protect, err := h.respond(ctx, user.ID, command, arg)
	if err != nil {
		return h.failure(ctx, chat.ID, err)
	}
	return h.send(ctx, chat.ID, text, keyboard, protect)
}
func (h *Handler) send(ctx context.Context, chat int64, text string, k *Keyboard, protect bool) error {
	m := Outgoing{ChatID: chat, Text: text, Keyboard: k, Protect: protect}
	m.LinkPreview.Disabled = true
	return h.telegram.SendMessage(ctx, m)
}
func (h *Handler) failure(ctx context.Context, chat int64, cause error) error {
	_ = h.send(ctx, chat, "Не удалось выполнить команду. Попробуйте ещё раз.", menu, false)
	return cause
}
func validID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
func label(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return value
}
func (h *Handler) respond(ctx context.Context, owner domain.ID, command, arg string) (string, *Keyboard, bool, error) {
	switch command {
	case "start":
		return "MusicGetter — перенос музыкальных коллекций через расширение.\nНачните с /connect.\n\n" + help, menu, false, nil
	case "help":
		return help, menu, false, nil
	case "connect":
		code, expires, err := h.pairings.Create(ctx, owner)
		return fmt.Sprintf("Код подключения расширения:\n%s\n\nВведите его в MusicGetter. Код одноразовый и действует 5 минут (до %s UTC). Новый /connect заменяет предыдущий код. Не передавайте код другим людям.", code, expires.UTC().Format("15:04:05")), nil, true, err
	case "settings":
		count, err := h.queries.Sessions(ctx, owner)
		return fmt.Sprintf("Активных подключений расширения: %d.\nСессия действует 30 дней. Отзыв отключит все расширения и аннулирует неиспользованные коды.", count), &Keyboard{Rows: [][]Button{{{Text: "Отозвать все подключения", Data: "revoke"}}, {{Text: "Подключить расширение", Data: "connect"}}}}, false, err
	case "revoke":
		return "Все подключения и неиспользованные коды отозваны. Для нового подключения: /connect.", menu, false, h.pairings.RevokeAll(ctx, owner)
	case "imports":
		return h.imports(ctx, owner, arg)
	case "status":
		return h.status(ctx, owner, arg)
	case "playlists":
		return h.playlists(ctx, owner, arg)
	default:
		return "Неизвестная команда.\n" + help, menu, false, nil
	}
}
