//go:build integration

package postgres_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
)

func TestPairingProofExpirationAndSingleConsume(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewPairingRepository(f.pool)
	code := sha256.Sum256([]byte("random-code"))
	proof := sha256.Sum256([]byte("random-proof"))
	wrong := sha256.Sum256([]byte("wrong"))
	p, err := repo.Create(f.ctx, code[:], proof[:], time.Now().Add(time.Minute))
	requireOK(t, err)
	_, err = repo.Consume(f.ctx, code[:], proof[:])
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unconfirmed pairing consumed")
	}
	_, err = repo.Confirm(f.ctx, code[:], f.user.ID)
	requireOK(t, err)
	_, err = repo.Consume(f.ctx, code[:], wrong[:])
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("wrong proof accepted")
	}
	const n = 12
	errs := make([]error, n)
	values := make([]domain.ExtensionPairing, n)
	var group sync.WaitGroup
	for i := range n {
		group.Go(func() { values[i], errs[i] = repo.Consume(f.ctx, code[:], proof[:]) })
	}
	group.Wait()
	successes := 0
	for i, err := range errs {
		if err == nil {
			successes++
			if values[i].ID != p.ID || values[i].OwnerID == nil || *values[i].OwnerID != f.user.ID {
				t.Fatal("wrong pairing returned")
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("consume succeeded %d times", successes)
	}
	expiredCode := sha256.Sum256([]byte("expired"))
	exp, err := repo.Create(f.ctx, expiredCode[:], proof[:], time.Now().Add(time.Minute))
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.extension_pairings SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, exp.ID)
	requireOK(t, err)
	_, err = repo.Confirm(f.ctx, expiredCode[:], f.user.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expired pairing confirmed")
	}
}

func TestKeysetPaginationAndTransactionRollback(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewImportRepository(f.pool)
	record := f.importRecord(t, "pages")
	for i := range 5 {
		f.item(t, record, fmt.Sprintf("Song %d", i))
	}
	seen := map[domain.ID]bool{}
	var after *domain.ID
	for {
		page, err := repo.ListItems(f.ctx, f.user.ID, record.ID, after, 2)
		requireOK(t, err)
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatal("unbounded page")
		}
		for _, item := range page {
			if seen[item.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[item.ID] = true
		}
		id := page[len(page)-1].ID
		after = &id
	}
	if len(seen) != 5 {
		t.Fatal("pagination lost items")
	}
	if _, err := repo.ListItems(f.ctx, f.user.ID, record.ID, nil, 201); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("oversize page accepted")
	}
	user2, err := postgres.NewUserRepository(f.pool).EnsureTelegram(f.ctx, 99)
	requireOK(t, err)
	hidden, err := repo.ListItems(f.ctx, user2.ID, record.ID, nil, 10)
	requireOK(t, err)
	if len(hidden) != 0 {
		t.Fatal("cross-user page exposed")
	}
	tx, err := f.pool.Begin(f.ctx)
	requireOK(t, err)
	defer tx.Rollback(f.ctx)
	tr := postgres.NewCanonicalTrackRepository(tx)
	ir := postgres.NewImportRepository(tx)
	jr := postgres.NewJobRepository(tx)
	track, err := tr.Ensure(f.ctx, domain.CanonicalTrackInput{OwnerID: f.user.ID, ProfileID: f.profile.ID, Source: f.profile.Source, Metadata: domain.TrackMetadata{Title: "Rollback only", Artists: []string{"Artist"}}})
	requireOK(t, err)
	item, err := ir.AddItem(f.ctx, f.user.ID, record.ID, track.ID, 9)
	requireOK(t, err)
	_, err = jr.Enqueue(f.ctx, domain.ImportJob{OwnerID: f.user.ID, ImportID: record.ID, ItemID: item.ID, Kind: domain.JobMatch, LogicalKey: "rollback", MaxAttempts: -1})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("expected constraint failure")
	}
	requireOK(t, tx.Rollback(f.ctx))
	_, err = postgres.NewCanonicalTrackRepository(f.pool).Get(f.ctx, f.user.ID, track.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("transaction leaked track")
	}
	page, err := repo.ListItems(f.ctx, f.user.ID, record.ID, nil, 10)
	requireOK(t, err)
	if len(page) != 5 {
		t.Fatal("transaction leaked item")
	}
}
