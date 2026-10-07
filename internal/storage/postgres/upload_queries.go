package postgres

import (
	"context"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

func (r *UploadRepository) GetUpload(ctx context.Context, owner, id domain.ID) (importer.UploadView, error) {
	var v importer.UploadView
	err := r.pool.QueryRow(ctx, `SELECT i.id,i.state,u.capture_state,u.received_chunks,u.received_observations,u.contiguous_through,i.created_at,count(t.id),count(t.id) FILTER(WHERE t.state='added'),count(t.id) FILTER(WHERE t.state='already_present'),count(t.id) FILTER(WHERE t.state IN ('failed','not_found') AND t.error_code<>'import_cancelled'),count(t.id) FILTER(WHERE t.state='ambiguous'),count(t.id) FILTER(WHERE t.error_code='import_cancelled') FROM musicgetter.imports i JOIN musicgetter.import_uploads u ON u.import_id=i.id LEFT JOIN musicgetter.import_items t ON t.import_id=i.id WHERE i.owner_id=$1 AND i.id=$2 GROUP BY i.id,u.import_id`, owner, id).Scan(&v.ID, &v.State, &v.CaptureState, &v.ReceivedChunks, &v.ReceivedObservations, &v.ContiguousThrough, &v.CreatedAt, &v.TotalTracks, &v.Added, &v.AlreadyPresent, &v.Failed, &v.NeedsReview, &v.Cancelled)
	return v, repositoryError(err)
}
func (r *UploadRepository) User(ctx context.Context, owner domain.ID) (domain.User, error) {
	return NewUserRepository(r.pool).Get(ctx, owner)
}
func (r *UploadRepository) Destinations(ctx context.Context, owner domain.ID, cursor string, limit int) ([]importer.DestinationView, error) {
	if limit < 1 || limit > 100 || cursor != "" && !importer.ValidID(cursor) {
		return nil, domain.ErrInvalid
	}
	rows, err := r.pool.Query(ctx, `SELECT d.id,d.connection_id,c.adapter,d.kind,d.title FROM musicgetter.destination_collections d JOIN musicgetter.destination_connections c ON c.id=d.connection_id WHERE d.owner_id=$1 AND ($2='' OR d.id>NULLIF($2,'')::uuid) ORDER BY d.id LIMIT $3`, owner, cursor, limit)
	if err != nil {
		return nil, repositoryError(err)
	}
	defer rows.Close()
	result := make([]importer.DestinationView, 0, limit)
	for rows.Next() {
		var v importer.DestinationView
		if err = rows.Scan(&v.ID, &v.ConnectionID, &v.Adapter, &v.Kind, &v.Title); err != nil {
			return nil, repositoryError(err)
		}
		result = append(result, v)
	}
	return result, repositoryError(rows.Err())
}
