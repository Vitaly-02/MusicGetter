package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

var _ importer.TargetStore = (*PipelineRepository)(nil)

func (r *PipelineRepository) PrepareAlbumFallback(ctx context.Context, j domain.ImportJob) (importer.AlbumFallbackPlan, error) {
	var plan importer.AlbumFallbackPlan
	err := r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		if i.State == domain.ImportCancelled {
			return domain.ErrConflict
		}
		requested, err := NewDestinationCollectionRepository(tx).Get(ctx, j.OwnerID, i.DestinationCollectionID)
		if err != nil {
			return err
		}
		if requested.Kind != domain.CollectionAlbum {
			return domain.ErrInvalid
		}
		// Earliest captured item is deterministic after seal, independent of worker
		// scheduling. No full-library scan/aggregation or invented album artist.
		var artists []string
		var album string
		err = tx.QueryRow(ctx, `SELECT t.artists,COALESCE(NULLIF(t.album,''),CASE WHEN s.kind='album' THEN s.title ELSE $3 END)
 FROM musicgetter.import_items item JOIN musicgetter.canonical_tracks t ON t.id=item.canonical_track_id JOIN musicgetter.source_collections s ON s.id=$2
 WHERE item.import_id=$1 AND item.owner_id=$4 ORDER BY item.position,item.id LIMIT 1`, i.ID, i.SourceCollectionID, requested.Title, j.OwnerID).Scan(&artists, &album)
		if err != nil {
			return repositoryError(err)
		}
		title, err := destination.AlbumPlaylistTitle(artists, album)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO musicgetter.destination_target_bindings(requested_collection_id,owner_id,connection_id,title) VALUES($1,$2,$3,$4) ON CONFLICT(requested_collection_id) DO NOTHING`, i.DestinationCollectionID, j.OwnerID, i.ConnectionID, title)
		if err != nil {
			return repositoryError(err)
		}
		var resolved *domain.ID
		err = tx.QueryRow(ctx, `SELECT operation_key,title,resolved_collection_id FROM musicgetter.destination_target_bindings WHERE owner_id=$1 AND connection_id=$2 AND requested_collection_id=$3 FOR UPDATE`, j.OwnerID, i.ConnectionID, i.DestinationCollectionID).Scan(&plan.OperationKey, &plan.Title, &resolved)
		if err != nil {
			return repositoryError(err)
		}
		if resolved != nil {
			target, err := NewDestinationCollectionRepository(tx).Get(ctx, j.OwnerID, *resolved)
			if err != nil {
				return err
			}
			if target.Kind != domain.CollectionPlaylist {
				return domain.ErrConflict
			}
			plan.Resolved = &domain.Target{ConnectionID: i.ConnectionID, ExternalID: target.ExternalKey, Kind: target.Kind}
			_, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET resolved_destination_collection_id=$2 WHERE id=$1`, i.ID, *resolved)
			return repositoryError(err)
		}
		return nil
	})
	return plan, err
}
func (r *PipelineRepository) BindAlbumFallback(ctx context.Context, j domain.ImportJob, c destination.Collection) error {
	if c.Kind != domain.CollectionPlaylist || !destination.ValidKey(c.ID) {
		return domain.ErrInvalid
	}
	return r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		// Positive creation evidence is saved even if cancellation raced with the
		// remote response. Subsequent Load prevents delivery for the cancelled import.
		var title string
		var resolved *domain.ID
		err := tx.QueryRow(ctx, `SELECT title,resolved_collection_id FROM musicgetter.destination_target_bindings WHERE owner_id=$1 AND connection_id=$2 AND requested_collection_id=$3 FOR UPDATE`, j.OwnerID, i.ConnectionID, i.DestinationCollectionID).Scan(&title, &resolved)
		if err != nil {
			return repositoryError(err)
		}
		target, err := NewDestinationCollectionRepository(tx).Ensure(ctx, domain.DestinationCollection{OwnerID: j.OwnerID, ConnectionID: i.ConnectionID, ExternalKey: c.ID, Kind: c.Kind, Title: title})
		if err != nil {
			return err
		}
		if resolved != nil && *resolved != target.ID {
			return domain.ErrConflict
		}
		_, err = tx.Exec(ctx, `UPDATE musicgetter.destination_target_bindings SET resolved_collection_id=$2 WHERE requested_collection_id=$1`, i.DestinationCollectionID, target.ID)
		if err != nil {
			return repositoryError(err)
		}
		_, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET resolved_destination_collection_id=$2 WHERE id=$1`, i.ID, target.ID)
		return repositoryError(err)
	})
}
