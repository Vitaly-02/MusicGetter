package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type MembershipRepository struct{ db DBTX }

func NewMembershipRepository(db DBTX) *MembershipRepository { return &MembershipRepository{db} }

const membershipColumns = `id,owner_id,connection_id,collection_id,destination_track_id,operation_key,state,created_at,updated_at`

// Reserve returns the same intent/operation key to all imports and callers.
// This method does not send anything and cannot confirm remote membership.
func (r *MembershipRepository) Reserve(ctx context.Context, owner, connection, collection, track domain.ID) (domain.DestinationMembership, error) {
	return scanMembership(r.db.QueryRow(ctx, `INSERT INTO musicgetter.destination_memberships AS existing
 (owner_id,connection_id,collection_id,destination_track_id) VALUES ($1,$2,$3,$4)
 ON CONFLICT (collection_id,destination_track_id) DO UPDATE SET id=existing.id
 WHERE existing.owner_id=EXCLUDED.owner_id AND existing.connection_id=EXCLUDED.connection_id
 RETURNING `+membershipColumns, owner, connection, collection, track))
}

func (r *MembershipRepository) Get(ctx context.Context, owner, collection, track domain.ID) (domain.DestinationMembership, error) {
	return scanMembership(r.db.QueryRow(ctx, `SELECT `+membershipColumns+` FROM musicgetter.destination_memberships
 WHERE owner_id=$1 AND collection_id=$2 AND destination_track_id=$3`, owner, collection, track))
}

func (r *MembershipRepository) Transition(ctx context.Context, owner, id domain.ID, from, to domain.MembershipState) (domain.DestinationMembership, error) {
	valid := from == domain.MembershipReserved && (to == domain.MembershipUnknown || to == domain.MembershipApplied) || from == domain.MembershipUnknown && to == domain.MembershipApplied
	if !valid {
		return domain.DestinationMembership{}, domain.ErrInvalid
	}
	m, err := scanMembership(r.db.QueryRow(ctx, `UPDATE musicgetter.destination_memberships SET state=$4,updated_at=clock_timestamp()
 WHERE owner_id=$1 AND id=$2 AND state=$3 RETURNING `+membershipColumns, owner, id, from, to))
	if err == domain.ErrNotFound {
		return m, domain.ErrConflict
	}
	return m, err
}

func scanMembership(row pgx.Row) (domain.DestinationMembership, error) {
	var m domain.DestinationMembership
	err := row.Scan(&m.ID, &m.OwnerID, &m.ConnectionID, &m.CollectionID, &m.DestinationTrackID, &m.OperationKey, &m.State, &m.CreatedAt, &m.UpdatedAt)
	return m, repositoryError(err)
}
