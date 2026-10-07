//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/matcher"
	"musicgetter/internal/storage/postgres"
	"testing"
)

func TestPipelineChunkFallbackAndCrossSourceDedup(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	p := pipeline(f, d)
	uploads := postgres.NewUploadRepository(f.pool)
	for n, source := range []domain.Source{domain.SourceSpotify, domain.SourceYandex, domain.SourceVK, domain.SourceSpotify} {
		request := uploadRequest(f, fmt.Sprintf("capture-%d", n))
		request.Source.Service = source
		if n == 3 {
			request.Source.Kind = domain.SourceSelection
			request.Source.CollectionKey = "selected"
		}
		created, err := uploads.CreateUpload(f.ctx, f.user.ID, request)
		requireOK(t, err)
		assertState(t, f, created.ID, domain.ImportCreated)
		chunk := uploadChunk(0, "chunk", "Song", " SONG ")
		receipt, err := uploads.AppendChunk(f.ctx, f.user.ID, created.ID, chunk)
		requireOK(t, err)
		if receipt.Added != 1 {
			t.Fatal("normalized fallback did not dedup")
		}
		replay, err := uploads.AppendChunk(f.ctx, f.user.ID, created.ID, chunk)
		requireOK(t, err)
		if !replay.Replay {
			t.Fatal("chunk not replayed")
		}
		assertState(t, f, created.ID, domain.ImportReceiving)
		requireOK(t, uploads.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(0, 2)))
		requireOK(t, p.Process(f.ctx, claimOne(t, f)))
		assertState(t, f, created.ID, domain.ImportCompleted)
	}
	if d.adds != 1 || d.calls != 1 || d.searches != 4 {
		t.Fatalf("adds=%d calls=%d searches=%d (provisional must revalidate)", d.adds, d.calls, d.searches)
	}
}

func TestPipelineCancellationRetainsEvidenceAndFencesOtherOwners(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	p := pipeline(f, d)
	u := postgres.NewUploadRepository(f.pool)
	created, err := u.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, "cancel-inflight"))
	requireOK(t, err)
	_, err = u.AppendChunk(f.ctx, f.user.ID, created.ID, uploadChunk(0, "chunk", "Song"))
	requireOK(t, err)
	requireOK(t, u.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(0, 1)))
	j := claimOne(t, f)
	w, err := p.Store.Load(f.ctx, j)
	requireOK(t, err)
	track, err := p.Store.Matched(f.ctx, j, domain.DestinationTrack{ExternalKey: "remote:song", Metadata: w.Track.Metadata}, matcher.ExactPolicyVersion)
	requireOK(t, err)
	intent, err := p.Store.Intent(f.ctx, j)
	requireOK(t, err)
	// A request accepted by the destination before cancellation may finish after it.
	_, err = d.EnsureTrack(f.ctx, destination.EnsureRequest{OperationKey: string(intent.OperationKey), Target: w.Target, TrackID: track.ExternalKey})
	requireOK(t, err)
	requireOK(t, u.CancelUpload(f.ctx, f.user.ID, created.ID))
	if _, err = p.Store.Load(f.ctx, j); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("cancelled import can send")
	}
	other, err := postgres.NewUserRepository(f.pool).EnsureTelegram(f.ctx, 12345)
	requireOK(t, err)
	forged := j
	forged.OwnerID = other.ID
	if err = p.Store.Finish(f.ctx, forged, domain.ItemAdded, ""); err == nil {
		t.Fatal("owner escaped")
	}
	requireOK(t, p.Store.Finish(f.ctx, j, domain.ItemAdded, ""))
	assertState(t, f, created.ID, domain.ImportCancelled)
	view, err := u.GetUpload(f.ctx, f.user.ID, created.ID)
	requireOK(t, err)
	if view.Added != 1 || view.Cancelled != 0 {
		t.Fatal("late evidence lost", view)
	}
}

type catalogFunc func(context.Context, matcher.Query) (matcher.CandidatePage, error)

func (f catalogFunc) Search(ctx context.Context, q matcher.Query) (matcher.CandidatePage, error) {
	return f(ctx, q)
}
func TestPipelineAmbiguousAndNotFoundAreTerminal(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	p := pipeline(f, d)
	p.Resolver = importer.Registry{"fake": func(context.Context, domain.DestinationConnection) (importer.Binding, error) {
		return importer.Binding{Destination: d, Catalog: catalogFunc(func(_ context.Context, q matcher.Query) (matcher.CandidatePage, error) {
			if q.Track.Title == "None" {
				return matcher.CandidatePage{}, nil
			}
			return matcher.CandidatePage{Candidates: []matcher.Candidate{{Track: domain.DestinationTrack{ExternalKey: "a", Metadata: q.Track}}, {Track: domain.DestinationTrack{ExternalKey: "b", Metadata: q.Track}}}}, nil
		})}, nil
	}}
	i := f.importRecord(t, "decisions")
	for _, title := range []string{"Ambiguous", "None"} {
		f.enqueue(t, f.item(t, i, title), 2)
	}
	for range 2 {
		requireOK(t, p.Process(f.ctx, claimOne(t, f)))
	}
	assertState(t, f, i.ID, domain.ImportCompletedWithErrors)
	items, err := postgres.NewImportRepository(f.pool).ListItems(f.ctx, f.user.ID, i.ID, nil, 10)
	requireOK(t, err)
	states := map[domain.ImportItemState]int{}
	for _, item := range items {
		states[item.State]++
	}
	if states[domain.ItemAmbiguous] != 1 || states[domain.ItemNotFound] != 1 || d.calls != 0 {
		t.Fatal("bad decisions", states, d.calls)
	}
}
