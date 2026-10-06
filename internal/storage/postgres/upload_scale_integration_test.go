//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
	"testing"
	"time"
)

// Seed the accepted ledger in SQL to exercise set-based completion at realistic
// library size; HTTP chunk processing is tested separately with 200 tracks.
func TestCompleteFiftyThousandTracks(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f.ctx = ctx
	started := time.Now()
	r := postgres.NewUploadRepository(f.pool)
	v, err := r.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, "scale"))
	requireOK(t, err)
	sample := f.track(t, "Scale track", nil)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO musicgetter.canonical_tracks(owner_id,profile_id,source,title,artists,album,duration_ms,edition,source_track_key,normalized_title,normalized_artists,fingerprint) SELECT owner_id,profile_id,source,title,artists,album,duration_ms,edition,'scale-'||n,normalized_title,normalized_artists,fingerprint FROM musicgetter.canonical_tracks CROSS JOIN generate_series(1,50000) n WHERE id=$1`, sample.ID)
	requireOK(t, err)
	t.Log("canonical fixture", time.Since(started))
	_, err = f.pool.Exec(f.ctx, `ANALYZE musicgetter.canonical_tracks`)
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO musicgetter.import_items(owner_id,import_id,canonical_track_id,profile_id,source,connection_id,position) SELECT i.owner_id,i.id,c.id,i.profile_id,i.source,i.connection_id,0 FROM musicgetter.imports i JOIN musicgetter.canonical_tracks c ON c.profile_id=i.profile_id WHERE i.id=$1 AND c.source_track_key LIKE 'scale-%'`, v.ID)
	requireOK(t, err)
	t.Log("item fixture", time.Since(started))
	_, err = f.pool.Exec(f.ctx, `ANALYZE musicgetter.import_items`)
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO musicgetter.import_chunks(import_id,owner_id,sequence,idempotency_key,payload_digest,received,added) SELECT $1,$2,n,'scale-'||n,decode(repeat('00',32),'hex'),200,200 FROM generate_series(0,249) n`, v.ID, f.user.ID)
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.import_uploads SET received_chunks=250,received_observations=50000,contiguous_through=249 WHERE import_id=$1`, v.ID)
	requireOK(t, err)
	completeCtx, stop := context.WithTimeout(f.ctx, 10*time.Second)
	defer stop()
	requireOK(t, r.CompleteUpload(completeCtx, f.user.ID, v.ID, uploadComplete(249, 50000)))
	t.Log("complete", time.Since(started))
	status, err := r.GetUpload(f.ctx, f.user.ID, v.ID)
	requireOK(t, err)
	if status.TotalTracks != 50000 || status.State != domain.ImportQueued {
		t.Fatal("scale progress incorrect")
	}
	var jobs int
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM musicgetter.import_jobs WHERE import_id=$1`, v.ID).Scan(&jobs))
	if jobs != 50000 {
		t.Fatal("scale enqueue incomplete")
	}
}
func TestChunkConstraintsAndCompleteAppendRace(t *testing.T) {
	f := newFixture(t)
	r := postgres.NewUploadRepository(f.pool)
	v, err := r.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, "constraints"))
	requireOK(t, err)
	_, err = r.AppendChunk(f.ctx, f.user.ID, v.ID, uploadChunk(0, "unique", "Song"))
	requireOK(t, err)
	for _, query := range []string{
		`INSERT INTO musicgetter.import_chunks SELECT import_id,owner_id,sequence,'another',payload_digest,received,added,created_at FROM musicgetter.import_chunks WHERE import_id=$1`,
		`INSERT INTO musicgetter.import_chunks SELECT import_id,owner_id,sequence+1,idempotency_key,payload_digest,received,added,created_at FROM musicgetter.import_chunks WHERE import_id=$1`,
	} {
		if _, err = f.pool.Exec(f.ctx, query, v.ID); err == nil {
			t.Fatal("database accepted duplicate receipt")
		}
	}
	finished := make(chan error, 1)
	go func() { finished <- r.CompleteUpload(f.ctx, f.user.ID, v.ID, uploadComplete(1, 2)) }()
	_, err = r.AppendChunk(f.ctx, f.user.ID, v.ID, uploadChunk(1, "second", "Second"))
	requireOK(t, err)
	if err = <-finished; err != nil && !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	requireOK(t, r.CompleteUpload(f.ctx, f.user.ID, v.ID, uploadComplete(1, 2)))
	state, err := r.GetUpload(f.ctx, f.user.ID, v.ID)
	requireOK(t, err)
	if state.TotalTracks != 2 || state.State != domain.ImportQueued {
		t.Fatal("complete/append race lost work")
	}
}
