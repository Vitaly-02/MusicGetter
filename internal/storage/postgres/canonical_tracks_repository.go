package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type CanonicalTrackRepository struct{ db DBTX }

func NewCanonicalTrackRepository(db DBTX) *CanonicalTrackRepository {
	return &CanonicalTrackRepository{db}
}

const canonicalColumns = `id,owner_id,profile_id,source,title,artists,album,duration_ms,edition,
 source_track_key,source_url,normalized_title,normalized_artists,fingerprint,identity_key,created_at`

// First accepted metadata remains immutable. Retry never rewrites prior evidence.
func (r *CanonicalTrackRepository) Ensure(ctx context.Context, input domain.CanonicalTrackInput) (domain.CanonicalTrack, error) {
	c, err := domain.PrepareCanonicalTrack(input)
	if err != nil {
		return domain.CanonicalTrack{}, err
	}
	row := r.db.QueryRow(ctx, `INSERT INTO musicgetter.canonical_tracks AS existing
 (owner_id,profile_id,source,title,artists,album,duration_ms,edition,source_track_key,source_url,
 normalized_title,normalized_artists,fingerprint) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
 ON CONFLICT (profile_id,identity_key) DO UPDATE SET id=existing.id
 WHERE existing.owner_id=EXCLUDED.owner_id AND existing.source=EXCLUDED.source
 RETURNING `+canonicalColumns, c.OwnerID, c.ProfileID, c.Source, c.Metadata.Title, c.Metadata.Artists, c.Metadata.Album,
		c.Metadata.DurationMS, c.Metadata.Version, c.SourceTrackKey, c.SourceURL, c.NormalizedTitle, c.NormalizedArtists, c.Fingerprint)
	return scanCanonical(row)
}

func (r *CanonicalTrackRepository) Get(ctx context.Context, owner, id domain.ID) (domain.CanonicalTrack, error) {
	return scanCanonical(r.db.QueryRow(ctx, `SELECT `+canonicalColumns+` FROM musicgetter.canonical_tracks WHERE owner_id=$1 AND id=$2`, owner, id))
}

func scanCanonical(row pgx.Row) (domain.CanonicalTrack, error) {
	var c domain.CanonicalTrack
	err := row.Scan(&c.ID, &c.OwnerID, &c.ProfileID, &c.Source, &c.Metadata.Title, &c.Metadata.Artists, &c.Metadata.Album,
		&c.Metadata.DurationMS, &c.Metadata.Version, &c.SourceTrackKey, &c.SourceURL, &c.NormalizedTitle, &c.NormalizedArtists, &c.Fingerprint, &c.IdentityKey, &c.CreatedAt)
	return c, repositoryError(err)
}
