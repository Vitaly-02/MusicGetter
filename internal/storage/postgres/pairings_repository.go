package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type PairingRepository struct{ db DBTX }

func NewPairingRepository(db DBTX) *PairingRepository { return &PairingRepository{db} }

func scanPairing(row pgx.Row) (domain.ExtensionPairing, error) {
	var p domain.ExtensionPairing
	err := row.Scan(&p.ID, &p.OwnerID, &p.CodeHash, &p.ChallengeHash, &p.ExpiresAt, &p.ConsumedAt, &p.RevokedAt, &p.CreatedAt)
	return p, repositoryError(err)
}

const pairingColumns = `id,owner_id,code_hash,challenge_hash,expires_at,consumed_at,revoked_at,created_at`

func (r *PairingRepository) Create(ctx context.Context, codeHash, challengeHash []byte, expires time.Time) (domain.ExtensionPairing, error) {
	return scanPairing(r.db.QueryRow(ctx, `INSERT INTO musicgetter.extension_pairings
 (code_hash,challenge_hash,expires_at) VALUES ($1,$2,$3) RETURNING `+pairingColumns, codeHash, challengeHash, expires))
}

// Owner is trusted application identity from a verified Telegram update.
func (r *PairingRepository) Confirm(ctx context.Context, codeHash []byte, owner domain.ID) (domain.ExtensionPairing, error) {
	return scanPairing(r.db.QueryRow(ctx, `UPDATE musicgetter.extension_pairings SET owner_id=$2
 WHERE code_hash=$1 AND (owner_id IS NULL OR owner_id=$2)
 AND revoked_at IS NULL AND consumed_at IS NULL AND expires_at>clock_timestamp() RETURNING `+pairingColumns, codeHash, owner))
}

// Atomic consume accepts only a confirmed, unexpired pair with the correct proof.
// Token issuance remains an application concern; use a shared pgx.Tx if needed.
func (r *PairingRepository) Consume(ctx context.Context, codeHash, challengeHash []byte) (domain.ExtensionPairing, error) {
	return scanPairing(r.db.QueryRow(ctx, `UPDATE musicgetter.extension_pairings SET consumed_at=clock_timestamp()
 WHERE code_hash=$1 AND challenge_hash=$2 AND owner_id IS NOT NULL
 AND revoked_at IS NULL AND consumed_at IS NULL AND expires_at>clock_timestamp() RETURNING `+pairingColumns, codeHash, challengeHash))
}
