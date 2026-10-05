package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"musicgetter/internal/domain"
	"time"
)

type SessionRepository struct{ pool *pgxpool.Pool }

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository { return &SessionRepository{pool} }

// Serialize issuance, redemption and revocation on the user row, in that order.
func lockSessionOwner(ctx context.Context, tx pgx.Tx, owner domain.ID) error {
	var id domain.ID
	return tx.QueryRow(ctx, `SELECT id FROM musicgetter.users WHERE id=$1 FOR UPDATE`, owner).Scan(&id)
}
func (r *SessionRepository) IssueCode(ctx context.Context, owner domain.ID, hash []byte) (time.Time, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, repositoryError(err)
	}
	defer tx.Rollback(ctx)
	if err = lockSessionOwner(ctx, tx, owner); err != nil {
		return time.Time{}, repositoryError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.extension_pairings SET revoked_at=clock_timestamp() WHERE owner_id=$1 AND challenge_hash IS NULL AND consumed_at IS NULL AND revoked_at IS NULL`, owner)
	if err != nil {
		return time.Time{}, repositoryError(err)
	}
	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO musicgetter.extension_pairings(owner_id,code_hash,created_at,expires_at) VALUES($1,$2,statement_timestamp(),statement_timestamp()+interval '5 minutes') RETURNING expires_at`, owner, hash).Scan(&expires)
	if err != nil {
		return time.Time{}, repositoryError(err)
	}
	return expires, repositoryError(tx.Commit(ctx))
}
func (r *SessionRepository) RedeemCode(ctx context.Context, codeHash, tokenHash []byte) (domain.ExtensionSession, error) {
	var s domain.ExtensionSession
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return s, repositoryError(err)
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT owner_id FROM musicgetter.extension_pairings WHERE code_hash=$1 AND challenge_hash IS NULL`, codeHash).Scan(&s.OwnerID)
	if err != nil {
		return s, repositoryError(err)
	}
	if err = lockSessionOwner(ctx, tx, s.OwnerID); err != nil {
		return s, repositoryError(err)
	}
	var id domain.ID
	err = tx.QueryRow(ctx, `UPDATE musicgetter.extension_pairings SET consumed_at=statement_timestamp() WHERE code_hash=$1 AND challenge_hash IS NULL AND revoked_at IS NULL AND consumed_at IS NULL AND expires_at>statement_timestamp() RETURNING id`, codeHash).Scan(&id)
	if err != nil {
		return s, repositoryError(err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO musicgetter.extension_sessions(owner_id,token_hash) VALUES($1,$2) RETURNING id,expires_at`, s.OwnerID, tokenHash).Scan(&s.ID, &s.ExpiresAt)
	if err != nil {
		return s, repositoryError(err)
	}
	return s, repositoryError(tx.Commit(ctx))
}
func (r *SessionRepository) Authenticate(ctx context.Context, hash []byte) (domain.ExtensionSession, error) {
	var s domain.ExtensionSession
	err := r.pool.QueryRow(ctx, `SELECT id,owner_id,expires_at FROM musicgetter.extension_sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>statement_timestamp()`, hash).Scan(&s.ID, &s.OwnerID, &s.ExpiresAt)
	return s, repositoryError(err)
}
func (r *SessionRepository) RevokeAll(ctx context.Context, owner domain.ID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return repositoryError(err)
	}
	defer tx.Rollback(ctx)
	if err = lockSessionOwner(ctx, tx, owner); err != nil {
		return repositoryError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.extension_pairings SET revoked_at=clock_timestamp() WHERE owner_id=$1 AND revoked_at IS NULL AND consumed_at IS NULL`, owner)
	if err != nil {
		return repositoryError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.extension_sessions SET revoked_at=clock_timestamp() WHERE owner_id=$1 AND revoked_at IS NULL`, owner)
	if err != nil {
		return repositoryError(err)
	}
	return repositoryError(tx.Commit(ctx))
}
