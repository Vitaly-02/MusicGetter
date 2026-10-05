package postgres

import (
	"context"

	"musicgetter/internal/domain"
)

type UserRepository struct{ db DBTX }

func NewUserRepository(db DBTX) *UserRepository { return &UserRepository{db} }

func (r *UserRepository) EnsureTelegram(ctx context.Context, telegramID int64) (domain.User, error) {
	var user domain.User
	err := r.db.QueryRow(ctx, `INSERT INTO musicgetter.users (telegram_user_id) VALUES ($1)
 ON CONFLICT (telegram_user_id) DO UPDATE SET telegram_user_id = EXCLUDED.telegram_user_id
 RETURNING id,telegram_user_id,created_at`, telegramID).Scan(&user.ID, &user.TelegramUserID, &user.CreatedAt)
	return user, repositoryError(err)
}

func (r *UserRepository) Get(ctx context.Context, id domain.ID) (domain.User, error) {
	var user domain.User
	err := r.db.QueryRow(ctx, `SELECT id,telegram_user_id,created_at FROM musicgetter.users WHERE id=$1`, id).Scan(&user.ID, &user.TelegramUserID, &user.CreatedAt)
	return user, repositoryError(err)
}
