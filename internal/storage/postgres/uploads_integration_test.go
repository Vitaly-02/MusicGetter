//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/storage/postgres"
)

func ptr(n int64) *int64 { return &n }
func uploadRequest(f fixture, key string) importer.CreateRequest {
	return importer.CreateRequest{ClientRequestID: key, Source: importer.SourceInput{Service: domain.SourceSpotify, ProfileKey: "main", CollectionKey: "liked", Kind: domain.SourceFavorites, Title: "Liked"}, DestinationCollectionID: f.destination.ID}
}
func uploadChunk(seq int64, key string, titles ...string) importer.ChunkRequest {
	c := importer.ChunkRequest{SchemaVersion: 1, IdempotencyKey: key, Sequence: ptr(seq)}
	for i, title := range titles {
		c.Tracks = append(c.Tracks, importer.TrackInput{Title: title, Artists: []string{"Artist"}, Position: ptr(seq*200 + int64(i))})
	}
	return c
}
func uploadComplete(last, count int64) importer.CompleteRequest {
	return importer.CompleteRequest{LastSequence: ptr(last), ObservedCount: ptr(count), Completeness: "partial", Reason: "unknown_end"}
}
func TestUploadReplayGapsSealAndJobs(t *testing.T) {
	f := newFixture(t)
	r := postgres.NewUploadRepository(f.pool)
	input := uploadRequest(f, "req")
	created, err := r.CreateUpload(f.ctx, f.user.ID, input)
	requireOK(t, err)
	again, err := r.CreateUpload(f.ctx, f.user.ID, input)
	requireOK(t, err)
	if !again.Replay || again.ID != created.ID {
		t.Fatal("create replay")
	}
	input.Source.Title = "Changed"
	if _, err = r.CreateUpload(f.ctx, f.user.ID, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("create changed payload accepted")
	}
	second := uploadChunk(1, "c1", "B", "A")
	receipt, err := r.AppendChunk(f.ctx, f.user.ID, created.ID, second)
	requireOK(t, err)
	if receipt.Added != 2 {
		t.Fatal("wrong receipt")
	}
	if err = r.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(1, 2)); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("sealed with gap")
	}
	jobs, err := postgres.NewJobRepository(f.pool).Claim(f.ctx, "worker", 10, 1000000000)
	requireOK(t, err)
	if len(jobs) != 0 {
		t.Fatal("unsealed jobs executable")
	}
	first := uploadChunk(0, "c0", "A", "A")
	receipt, err = r.AppendChunk(f.ctx, f.user.ID, created.ID, first)
	requireOK(t, err)
	if receipt.Added != 0 || receipt.Received != 2 {
		t.Fatal("cross-chunk dedup")
	}
	view, err := r.GetUpload(f.ctx, f.user.ID, created.ID)
	requireOK(t, err)
	if view.TotalTracks != 2 || view.ReceivedObservations != 4 || view.ReceivedChunks != 2 || view.ContiguousThrough != 1 || view.State != domain.ImportCollecting {
		t.Fatalf("bad view %+v", view)
	}
	requireOK(t, r.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(1, 4)))
	requireOK(t, r.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(1, 4)))
	if err = r.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(1, 3)); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("complete replay changed")
	}
	replay, err := r.AppendChunk(f.ctx, f.user.ID, created.ID, second)
	requireOK(t, err)
	if !replay.Replay || replay.Added != 2 {
		t.Fatal("lost ACK after seal")
	}
	if _, err = r.AppendChunk(f.ctx, f.user.ID, created.ID, uploadChunk(2, "new", "C")); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("append after seal")
	}
	var count int
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM musicgetter.import_jobs WHERE import_id=$1`, created.ID).Scan(&count))
	if count != 2 {
		t.Fatal("duplicate jobs")
	}
	requireOK(t, r.CancelUpload(f.ctx, f.user.ID, created.ID))
	requireOK(t, r.CancelUpload(f.ctx, f.user.ID, created.ID))
	view, err = r.GetUpload(f.ctx, f.user.ID, created.ID)
	requireOK(t, err)
	if view.State != domain.ImportCancelled || view.Cancelled != 2 {
		t.Fatal("cancel failed")
	}
	jobs, err = postgres.NewJobRepository(f.pool).Claim(f.ctx, "worker", 10, 1000000000)
	requireOK(t, err)
	if len(jobs) != 0 {
		t.Fatal("cancelled jobs executable")
	}
}
func TestConcurrentChunkAndCreateIdempotency(t *testing.T) {
	f := newFixture(t)
	r := postgres.NewUploadRepository(f.pool)
	var wg sync.WaitGroup
	var first atomic.Int32
	ids := make(chan domain.ID, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := r.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, "concurrent"))
			if err != nil {
				t.Error(err)
				return
			}
			if !v.Replay {
				first.Add(1)
			}
			ids <- v.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id domain.ID
	for v := range ids {
		if id != "" && v != id {
			t.Fatal("created multiple imports")
		}
		id = v
	}
	if first.Load() != 1 {
		t.Fatal("create not idempotent")
	}
	first.Store(0)
	chunk := uploadChunk(0, "once", "Song")
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := r.AppendChunk(f.ctx, f.user.ID, id, chunk)
			if err != nil {
				t.Error(err)
				return
			}
			if !v.Replay {
				first.Add(1)
			}
		}()
	}
	wg.Wait()
	if first.Load() != 1 {
		t.Fatal("chunk consumed multiple times")
	}
	for _, changed := range []importer.ChunkRequest{uploadChunk(0, "once", "Changed"), uploadChunk(1, "once", "Song"), uploadChunk(0, "different", "Song")} {
		if _, err := r.AppendChunk(f.ctx, f.user.ID, id, changed); !errors.Is(err, domain.ErrConflict) {
			t.Fatal("conflicting receipt accepted", err)
		}
	}
	v, err := r.GetUpload(f.ctx, f.user.ID, id)
	requireOK(t, err)
	if v.TotalTracks != 1 || v.ReceivedChunks != 1 {
		t.Fatal("duplicates stored")
	}
}
func TestUploadOwnerAndTransactionalRollback(t *testing.T) {
	f := newFixture(t)
	r := postgres.NewUploadRepository(f.pool)
	created, err := r.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, "atomic"))
	requireOK(t, err)
	other, err := postgres.NewUserRepository(f.pool).EnsureTelegram(f.ctx, 333)
	requireOK(t, err)
	if _, err = r.CreateUpload(f.ctx, other.ID, uploadRequest(f, "theft")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("foreign destination accepted")
	}
	for _, action := range []func() error{
		func() error { _, err := r.GetUpload(f.ctx, other.ID, created.ID); return err },
		func() error {
			_, err := r.AppendChunk(f.ctx, other.ID, created.ID, uploadChunk(0, "x", "X"))
			return err
		},
		func() error { return r.CompleteUpload(f.ctx, other.ID, created.ID, uploadComplete(-1, 0)) },
		func() error { return r.CancelUpload(f.ctx, other.ID, created.ID) },
	} {
		if err = action(); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("cross-owner access", err)
		}
	}
	chunk := uploadChunk(0, "atomic", "Good", "Bad")
	badURL := "https://music.yandex.ru/track/1"
	chunk.Tracks[1].SourceURL = &badURL
	if _, err = r.AppendChunk(f.ctx, f.user.ID, created.ID, chunk); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("wrong source URL accepted")
	}
	view, err := r.GetUpload(f.ctx, f.user.ID, created.ID)
	requireOK(t, err)
	if view.TotalTracks != 0 || view.ReceivedChunks != 0 {
		t.Fatal("partial chunk committed")
	}
	var count int
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM musicgetter.canonical_tracks WHERE owner_id=$1`, f.user.ID).Scan(&count))
	if count != 0 {
		t.Fatal("canonical writes survived rollback")
	}
	requireOK(t, r.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(-1, 0)))
	view, err = r.GetUpload(f.ctx, f.user.ID, created.ID)
	requireOK(t, err)
	if view.State != domain.ImportCompleted {
		t.Fatal("empty import not terminal")
	}
}
func TestCancelAppendRace(t *testing.T) {
	f := newFixture(t)
	r := postgres.NewUploadRepository(f.pool)
	for i := 0; i < 8; i++ {
		v, err := r.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, fmt.Sprintf("race-%d", i)))
		requireOK(t, err)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := r.AppendChunk(f.ctx, f.user.ID, v.ID, uploadChunk(0, "race", "Song"))
			if err != nil && !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := r.CancelUpload(f.ctx, f.user.ID, v.ID); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		state, err := r.GetUpload(f.ctx, f.user.ID, v.ID)
		requireOK(t, err)
		if state.State != domain.ImportCancelled || state.Cancelled != state.TotalTracks {
			t.Fatal("race left uncancelled work")
		}
		if _, err = r.AppendChunk(f.ctx, f.user.ID, v.ID, uploadChunk(1, "new", "After cancel")); !errors.Is(err, domain.ErrConflict) {
			t.Fatal("cancelled upload writable")
		}
	}
}
