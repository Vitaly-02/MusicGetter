package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"musicgetter/internal/domain"
)

type fakeTelegram struct {
	messages []Outgoing
	answers  int
}

func (f *fakeTelegram) SendMessage(_ context.Context, m Outgoing) error {
	f.messages = append(f.messages, m)
	return nil
}
func (f *fakeTelegram) AnswerCallback(context.Context, string, string) error { f.answers++; return nil }

type fakeUsers struct{ ids []int64 }

func (f *fakeUsers) EnsureTelegram(_ context.Context, id int64) (domain.User, error) {
	f.ids = append(f.ids, id)
	return domain.User{ID: "owner", TelegramUserID: id}, nil
}

type fakePairings struct {
	created, revoked []domain.ID
	err              error
}

func (f *fakePairings) Create(_ context.Context, id domain.ID) (string, time.Time, error) {
	f.created = append(f.created, id)
	return "SECRET_CODE", time.Unix(10000, 0), f.err
}
func (f *fakePairings) RevokeAll(_ context.Context, id domain.ID) error {
	f.revoked = append(f.revoked, id)
	return f.err
}

type fakeQueries struct {
	owner       domain.ID
	err         error
	rows        []domain.ImportSummary
	collections []domain.CollectionSummary
}

func (f *fakeQueries) Imports(_ context.Context, o domain.ID, _ string) ([]domain.ImportSummary, error) {
	f.owner = o
	return f.rows, f.err
}
func (f *fakeQueries) Status(_ context.Context, o domain.ID, _ string) (domain.ImportSummary, error) {
	f.owner = o
	if len(f.rows) == 0 {
		return domain.ImportSummary{}, domain.ErrNotFound
	}
	return f.rows[0], f.err
}
func (f *fakeQueries) Playlists(_ context.Context, o domain.ID, _ string) ([]domain.CollectionSummary, error) {
	f.owner = o
	return f.collections, f.err
}
func (f *fakeQueries) Sessions(_ context.Context, o domain.ID) (int, error) {
	f.owner = o
	return 2, f.err
}
func private(text string) Update {
	return Update{Message: &Message{From: &User{ID: 42}, Chat: Chat{ID: 42, Type: "private"}, Text: text}}
}
func TestCommands(t *testing.T) {
	for _, command := range []string{"start", "connect", "imports", "status", "playlists", "settings", "help", "unknown"} {
		t.Run(command, func(t *testing.T) {
			tg := &fakeTelegram{}
			users := &fakeUsers{}
			p := &fakePairings{}
			q := &fakeQueries{}
			h := NewHandler(tg, users, p, q, "MusicGetterBot")
			if err := h.Handle(context.Background(), private("/"+command+"@MusicGetterBot")); err != nil {
				t.Fatal(err)
			}
			if len(tg.messages) != 1 || len(users.ids) != 1 || users.ids[0] != 42 {
				t.Fatal("command was not routed")
			}
			message := tg.messages[0]
			if message.Text == "" || !message.LinkPreview.Disabled {
				t.Fatal("invalid response")
			}
			if command == "connect" && (!message.Protect || !strings.Contains(message.Text, "SECRET_CODE") || len(p.created) != 1) {
				t.Fatal("missing protected code")
			}
			if command == "settings" && (message.Keyboard == nil || !strings.Contains(message.Text, "2")) {
				t.Fatal("missing settings")
			}
		})
	}
}
func TestPrivateChatAndSenderBoundary(t *testing.T) {
	for _, alter := range []func(*Update){
		func(u *Update) { u.Message.Chat.Type = "group" }, func(u *Update) { u.Message.Chat.ID = 99 }, func(u *Update) { u.Message.From = nil }, func(u *Update) { u.Message.From.IsBot = true }, func(u *Update) { u.Message.Text = "/connect@AnotherBot" },
	} {
		tg := &fakeTelegram{}
		users := &fakeUsers{}
		p := &fakePairings{}
		h := NewHandler(tg, users, p, &fakeQueries{}, "MusicGetterBot")
		u := private("/connect")
		alter(&u)
		if err := h.Handle(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		if len(p.created) != 0 || len(users.ids) != 0 || len(tg.messages) != 0 {
			t.Fatal("untrusted update reached application")
		}
	}
}
func TestCallbackRevokeOwnerAndAck(t *testing.T) {
	tg := &fakeTelegram{}
	p := &fakePairings{}
	users := &fakeUsers{}
	h := NewHandler(tg, users, p, &fakeQueries{}, "")
	u := Update{Callback: &Callback{ID: "cb", From: User{ID: 42}, Message: &Message{Chat: Chat{ID: 42, Type: "private"}}, Data: "revoke"}}
	if err := h.Handle(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if tg.answers != 1 || len(p.revoked) != 1 || p.revoked[0] != "owner" {
		t.Fatal("revoke must use authenticated sender")
	}
	u.Callback.From.ID = 99
	if err := h.Handle(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if tg.answers != 2 || len(p.revoked) != 1 {
		t.Fatal("cross-owner callback accepted")
	}
	u.Callback.Message = nil
	_ = h.Handle(context.Background(), u)
	if tg.answers != 3 {
		t.Fatal("inaccessible callback not acknowledged")
	}
}
func TestErrorsNeverExposeCodeOrStorageDetails(t *testing.T) {
	tg := &fakeTelegram{}
	p := &fakePairings{err: errors.New("SECRET storage details")}
	h := NewHandler(tg, &fakeUsers{}, p, &fakeQueries{}, "")
	if h.Handle(context.Background(), private("/connect")) == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(tg.messages[0].Text, "SECRET") {
		t.Fatal("secret exposed")
	}
}
func TestViewsPaginationAndUntrustedTitle(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	q := &fakeQueries{collections: []domain.CollectionSummary{{ID: id, Title: strings.Repeat("🎵", 2000) + "\n<script>", Kind: domain.CollectionPlaylist}}}
	for i := 0; i < 10; i++ {
		q.rows = append(q.rows, domain.ImportSummary{ID: id, State: domain.ImportQueued, Source: domain.SourceSpotify, Total: 7, Done: 3, Failed: 1, NeedsReview: 2})
	}
	tg := &fakeTelegram{}
	h := NewHandler(tg, &fakeUsers{}, &fakePairings{}, q, "")
	for _, command := range []string{"/imports", "/status " + id, "/playlists"} {
		if err := h.Handle(context.Background(), private(command)); err != nil {
			t.Fatal(err)
		}
	}
	if q.owner != "owner" || len(tg.messages[0].Keyboard.Rows) != 11 {
		t.Fatal("pagination/ownership missing")
	}
	if !strings.Contains(tg.messages[1].Text, "Треков: 7") || !strings.Contains(tg.messages[1].Text, "Ошибок: 1") {
		t.Fatal("wrong progress")
	}
	if len([]rune(tg.messages[2].Text)) > 200 {
		t.Fatal("title not bounded")
	}
	for _, row := range tg.messages[0].Keyboard.Rows {
		for _, b := range row {
			if len(b.Data) > 64 {
				t.Fatal("Telegram callback exceeds limit")
			}
		}
	}
	if err := h.Handle(context.Background(), private("/status invalid")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tg.messages[3].Text, "Некорректный ID") {
		t.Fatal("invalid cursor not rejected")
	}
}
