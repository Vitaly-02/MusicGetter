package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

type PipelineRepository struct{ pool *pgxpool.Pool }

func NewPipelineRepository(pool *pgxpool.Pool) *PipelineRepository { return &PipelineRepository{pool} }

var _ importer.PipelineStore = (*PipelineRepository)(nil)

// All item transitions serialize with cancellation on parent, then fence job.
// Lease expiry is checked again at commit boundary, not just transaction entry.
func (r *PipelineRepository) leased(ctx context.Context, j domain.ImportJob, fn func(pgx.Tx, domain.Import) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return repositoryError(err)
	}
	defer tx.Rollback(ctx)
	record, err := scanImport(tx.QueryRow(ctx, `SELECT `+importColumns+` FROM musicgetter.imports WHERE owner_id=$1 AND id=$2 FOR UPDATE`, j.OwnerID, j.ImportID))
	if err != nil {
		return err
	}
	var deadline time.Time
	err = tx.QueryRow(ctx, `SELECT lease_until FROM musicgetter.import_jobs WHERE `+leasePredicate+` AND import_id=$5 AND item_id=$6 FOR UPDATE`, j.OwnerID, j.ID, j.Generation, j.WorkerID, j.ImportID, j.ItemID).Scan(&deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLeaseLost
	}
	if err != nil {
		return repositoryError(err)
	}
	if err = fn(tx, record); err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp() < $1`, deadline).Scan(&valid); err != nil {
		return repositoryError(err)
	}
	if !valid {
		return domain.ErrLeaseLost
	}
	return repositoryError(tx.Commit(ctx))
}

func (r *PipelineRepository) Load(ctx context.Context, j domain.ImportJob) (importer.Work, error) {
	var w importer.Work
	err := r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		if i.State == domain.ImportCancelled {
			return domain.ErrConflict
		}
		if i.State != domain.ImportQueued && i.State != domain.ImportProcessing {
			return domain.ErrConflict
		}
		if i.ResolvedDestinationCollectionID == nil {
			var resolved *domain.ID
			err := tx.QueryRow(ctx, `SELECT resolved_collection_id FROM musicgetter.destination_target_bindings WHERE requested_collection_id=$1 AND owner_id=$2 AND connection_id=$3`, i.DestinationCollectionID, j.OwnerID, i.ConnectionID).Scan(&resolved)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return repositoryError(err)
			}
			w.FallbackPending = err == nil && resolved == nil
			if resolved != nil {
				i.ResolvedDestinationCollectionID = resolved
				if _, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET resolved_destination_collection_id=$2 WHERE id=$1`, i.ID, *resolved); err != nil {
					return repositoryError(err)
				}
			}
		}
		w.Import = i
		var err error
		w.Item, err = scanItem(tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM musicgetter.import_items WHERE owner_id=$1 AND import_id=$2 AND id=$3`, j.OwnerID, j.ImportID, j.ItemID))
		if err != nil {
			return err
		}
		if w.Item.State != domain.ItemPending && w.Item.State != domain.ItemSearching && w.Item.State != domain.ItemMatched {
			return domain.ErrConflict
		}
		w.Track, err = NewCanonicalTrackRepository(tx).Get(ctx, j.OwnerID, w.Item.CanonicalTrackID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT id,owner_id,adapter,account_key,created_at FROM musicgetter.destination_connections WHERE owner_id=$1 AND id=$2`, j.OwnerID, i.ConnectionID).Scan(&w.Connection.ID, &w.Connection.OwnerID, &w.Connection.Adapter, &w.Connection.AccountKey, &w.Connection.CreatedAt)
		if err != nil {
			return repositoryError(err)
		}
		d, err := NewDestinationCollectionRepository(tx).Get(ctx, j.OwnerID, deliveryCollection(i))
		if err != nil {
			return err
		}
		w.Target = domain.Target{ConnectionID: i.ConnectionID, ExternalID: d.ExternalKey, Kind: d.Kind}
		if w.Item.DestinationTrackID != nil {
			t, err := NewDestinationTrackRepository(tx).Get(ctx, j.OwnerID, *w.Item.DestinationTrackID)
			if err != nil {
				return err
			}
			w.Selected = &t
		} else {
			m, err := NewMappingRepository(tx).Get(ctx, j.OwnerID, w.Track.ID, i.ConnectionID)
			if err == nil {
				t, err := NewDestinationTrackRepository(tx).Get(ctx, j.OwnerID, m.DestinationTrackID)
				if err != nil {
					return err
				}
				w.Cached = &m
				w.CachedTrack = &t
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET state='processing' WHERE id=$1 AND state='queued'`, j.ImportID)
		if err != nil {
			return repositoryError(err)
		}
		_, err = tx.Exec(ctx, `UPDATE musicgetter.import_items SET state='searching' WHERE id=$1 AND state='pending'`, j.ItemID)
		return repositoryError(err)
	})
	return w, err
}

func (r *PipelineRepository) Matched(ctx context.Context, j domain.ImportJob, t domain.DestinationTrack, policy string) (domain.DestinationTrack, error) {
	var selected domain.DestinationTrack
	err := r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		if i.State == domain.ImportCancelled {
			return domain.ErrConflict
		}
		var canonical domain.ID
		if err := tx.QueryRow(ctx, `SELECT canonical_track_id FROM musicgetter.import_items WHERE owner_id=$1 AND id=$2`, j.OwnerID, j.ItemID).Scan(&canonical); err != nil {
			return repositoryError(err)
		}
		t.ID = ""
		t.OwnerID = j.OwnerID
		t.ConnectionID = i.ConnectionID
		var err error
		selected, err = NewDestinationTrackRepository(tx).Ensure(ctx, t)
		if err != nil {
			return err
		}
		// Preserve manual decisions and immutable automatic mapping. Different match
		// cannot silently overwrite an earlier accepted recording.
		_, err = tx.Exec(ctx, `INSERT INTO musicgetter.track_mappings(owner_id,canonical_track_id,connection_id,destination_track_id,origin,policy_version) VALUES($1,$2,$3,$4,'automatic',$5) ON CONFLICT(canonical_track_id,connection_id) DO NOTHING`, j.OwnerID, canonical, i.ConnectionID, selected.ID, policy)
		if err != nil {
			return repositoryError(err)
		}
		m, err := NewMappingRepository(tx).Get(ctx, j.OwnerID, canonical, i.ConnectionID)
		if err != nil {
			return err
		}
		if m.DestinationTrackID != selected.ID {
			return domain.ErrConflict
		}
		_, err = tx.Exec(ctx, `UPDATE musicgetter.import_items SET state='matched',destination_track_id=$2 WHERE id=$1`, j.ItemID, selected.ID)
		return repositoryError(err)
	})
	return selected, err
}

// Reserve and mark unknown in the same transaction BEFORE the external effect.
// Concurrent imports share the same operation key; safety across dispatchers
// additionally requires an atomic ensure destination contract.
func (r *PipelineRepository) Intent(ctx context.Context, j domain.ImportJob) (domain.DestinationMembership, error) {
	var m domain.DestinationMembership
	err := r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		if i.State == domain.ImportCancelled {
			return domain.ErrConflict
		}
		var track domain.ID
		if err := tx.QueryRow(ctx, `SELECT destination_track_id FROM musicgetter.import_items WHERE owner_id=$1 AND id=$2 AND state='matched'`, j.OwnerID, j.ItemID).Scan(&track); err != nil {
			return repositoryError(err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO musicgetter.destination_memberships(owner_id,connection_id,collection_id,destination_track_id) VALUES($1,$2,$3,$4) ON CONFLICT(collection_id,destination_track_id) DO NOTHING`, j.OwnerID, i.ConnectionID, deliveryCollection(i), track)
		if err != nil {
			return repositoryError(err)
		}
		// A separate statement uses a fresh READ COMMITTED snapshot after conflict wait.
		m, err = scanMembership(tx.QueryRow(ctx, `SELECT `+membershipColumns+` FROM musicgetter.destination_memberships WHERE owner_id=$1 AND collection_id=$2 AND destination_track_id=$3 FOR UPDATE`, j.OwnerID, deliveryCollection(i), track))
		if err != nil {
			return err
		}
		if m.State == domain.MembershipReserved {
			_, err = tx.Exec(ctx, `UPDATE musicgetter.destination_memberships SET state='unknown',updated_at=clock_timestamp() WHERE id=$1`, m.ID)
		}
		// Return pre-dispatch state so first dispatch need not reconcile an operation
		// the destination has never seen. The committed row is already unknown.
		return repositoryError(err)
	})
	return m, err
}

func deliveryCollection(i domain.Import) domain.ID {
	if i.ResolvedDestinationCollectionID != nil {
		return *i.ResolvedDestinationCollectionID
	}
	return i.DestinationCollectionID
}
