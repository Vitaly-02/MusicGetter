package telegram

import (
	"context"
	"errors"
	"fmt"
	"musicgetter/internal/domain"
	"strings"
)

func (h *Handler) imports(ctx context.Context, owner domain.ID, cursor string) (string, *Keyboard, bool, error) {
	rows, err := h.queries.Imports(ctx, owner, cursor)
	if err != nil {
		return "", nil, false, err
	}
	if len(rows) == 0 {
		return "Импортов пока нет.", menu, false, nil
	}
	var lines []string
	k := &Keyboard{}
	for _, v := range rows {
		lines = append(lines, fmt.Sprintf("%s · %s\n%s", v.Source, state(string(v.State)), v.ID))
		k.Rows = append(k.Rows, []Button{{Text: "Статус " + string(v.ID)[:8], Data: "status:" + string(v.ID)}})
	}
	if len(rows) == 10 {
		k.Rows = append(k.Rows, []Button{{Text: "Далее", Data: "imports:" + string(rows[len(rows)-1].ID)}})
	}
	return strings.Join(lines, "\n\n"), k, false, nil
}
func (h *Handler) status(ctx context.Context, owner domain.ID, id string) (string, *Keyboard, bool, error) {
	v, err := h.queries.Status(ctx, owner, id)
	if errors.Is(err, domain.ErrNotFound) {
		return "Импорт не найден. Список: /imports.", menu, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return fmt.Sprintf("Импорт %s\n%s · %s\nТреков: %d\nДобавлено или уже существует: %d\nОшибок: %d\nОжидают выбора совпадения: %d", v.ID, v.Source, state(string(v.State)), v.Total, v.Done, v.Failed, v.NeedsReview), &Keyboard{Rows: [][]Button{{{Text: "Обновить", Data: "status:" + string(v.ID)}}, {{Text: "Все импорты", Data: "imports"}}}}, false, nil
}
func (h *Handler) playlists(ctx context.Context, owner domain.ID, cursor string) (string, *Keyboard, bool, error) {
	rows, err := h.queries.Playlists(ctx, owner, cursor)
	if err != nil {
		return "", nil, false, err
	}
	if len(rows) == 0 {
		return "Сохранённых коллекций пока нет. Подключение к музыкальному боту ещё не реализовано.", menu, false, nil
	}
	lines := []string{"Сохранённые коллекции:"}
	k := &Keyboard{}
	for _, v := range rows {
		lines = append(lines, fmt.Sprintf("%s · %s", label(v.Title), v.Kind))
	}
	if len(rows) == 10 {
		k.Rows = append(k.Rows, []Button{{Text: "Далее", Data: "playlists:" + string(rows[len(rows)-1].ID)}})
	}
	return strings.Join(lines, "\n"), k, false, nil
}
func state(value string) string {
	names := map[string]string{"queued": "в очереди", "running": "в работе", "needs_attention": "нужно ваше решение", "completed": "завершён", "completed_with_errors": "завершён с ошибками", "cancelled": "отменён", "failed": "ошибка"}
	if name, ok := names[value]; ok {
		return name
	}
	return value
}
