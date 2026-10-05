//go:build integration

package postgres_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"musicgetter/internal/domain"
	"musicgetter/internal/pairing"
	"musicgetter/internal/storage/postgres"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBotPairingConcurrencyHashingAndRevoke(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewSessionRepository(f.pool)
	service := pairing.New(repo)
	code, expires, err := service.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	if left := time.Until(expires); left > 5*time.Minute || left < 4*time.Minute {
		t.Fatal("wrong TTL")
	}
	var ttl float64
	var hash []byte
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT code_hash,extract(epoch FROM expires_at-created_at) FROM musicgetter.extension_pairings WHERE owner_id=$1`, f.user.ID).Scan(&hash, &ttl))
	digest := sha256.Sum256([]byte(code))
	if ttl != 300 || !bytes.Equal(hash, digest[:]) {
		t.Fatal("code plaintext or wrong TTL")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	tokens := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, s, err := service.Redeem(f.ctx, code)
			if err == nil {
				successes.Add(1)
				tokens <- token
				if s.OwnerID != f.user.ID {
					t.Error("wrong owner")
				}
			} else if !errors.Is(err, domain.ErrNotFound) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(tokens)
	if successes.Load() != 1 {
		t.Fatalf("redeemed %d times", successes.Load())
	}
	token := <-tokens
	session, err := service.Authenticate(f.ctx, token)
	requireOK(t, err)
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT token_hash FROM musicgetter.extension_sessions WHERE id=$1`, session.ID).Scan(&hash))
	digest = sha256.Sum256([]byte(token))
	if !bytes.Equal(hash, digest[:]) {
		t.Fatal("token not hashed")
	}
	if left := time.Until(session.ExpiresAt); left > 30*24*time.Hour || left < 29*24*time.Hour {
		t.Fatal("wrong session TTL")
	}
	pending, _, err := service.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	other, err := postgres.NewUserRepository(f.pool).EnsureTelegram(f.ctx, 99)
	requireOK(t, err)
	otherCode, _, err := service.Create(f.ctx, other.ID)
	requireOK(t, err)
	otherToken, _, err := service.Redeem(f.ctx, otherCode)
	requireOK(t, err)
	requireOK(t, service.RevokeAll(f.ctx, f.user.ID))
	if _, err = service.Authenticate(f.ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("revoked token accepted", err)
	}
	if _, _, err = service.Redeem(f.ctx, pending); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("pending code restored access", err)
	}
	_, err = service.Authenticate(f.ctx, otherToken)
	requireOK(t, err)
}
func TestPairingExpiryReplacementAndAtomicRollback(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewSessionRepository(f.pool)
	service := pairing.New(repo)
	old, _, err := service.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	code, _, err := service.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	if _, _, err = service.Redeem(f.ctx, old); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("old code accepted")
	}
	hash := sha256.Sum256([]byte(code))
	// Failed session insert must roll back consumption of the code.
	if _, err = repo.RedeemCode(f.ctx, hash[:], []byte("short")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	token, session, err := service.Redeem(f.ctx, code)
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.extension_sessions SET created_at=now()-interval '31 days',expires_at=now()-interval '1 day' WHERE id=$1`, session.ID)
	requireOK(t, err)
	if _, err = service.Authenticate(f.ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	code, _, err = service.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	hash = sha256.Sum256([]byte(code))
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.extension_pairings SET created_at=now()-interval '6 minutes',expires_at=now()-interval '1 minute' WHERE code_hash=$1`, hash[:])
	requireOK(t, err)
	if _, _, err = service.Redeem(f.ctx, code); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expired code accepted")
	}
}
func TestConcurrentRevokeCannotLeaveRedeemedSessionActive(t *testing.T) {
	f := newFixture(t)
	service := pairing.New(postgres.NewSessionRepository(f.pool))
	for i := 0; i < 8; i++ {
		code, _, err := service.Create(f.ctx, f.user.ID)
		requireOK(t, err)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, err := service.Redeem(f.ctx, code)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := service.RevokeAll(f.ctx, f.user.ID); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		count, err := postgres.NewBotQueries(f.pool).Sessions(f.ctx, f.user.ID)
		requireOK(t, err)
		if count != 0 {
			t.Fatal("revocation race left active session")
		}
	}
}
func TestBotViewsOwnerIsolationAndDurableDedup(t *testing.T) {
	f := newFixture(t)
	q := postgres.NewBotQueries(f.pool)
	record := f.importRecord(t, "bot-view")
	f.item(t, record, "Track")
	other, err := postgres.NewUserRepository(f.pool).EnsureTelegram(f.ctx, 123)
	requireOK(t, err)
	rows, err := q.Imports(f.ctx, f.user.ID, "")
	requireOK(t, err)
	if len(rows) != 1 || rows[0].ID != record.ID {
		t.Fatal("missing import")
	}
	status, err := q.Status(f.ctx, f.user.ID, "")
	requireOK(t, err)
	if status.Total != 1 {
		t.Fatal("wrong aggregate")
	}
	if _, err = q.Status(f.ctx, other.ID, string(record.ID)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-owner status")
	}
	rows, err = q.Imports(f.ctx, other.ID, "")
	requireOK(t, err)
	if len(rows) != 0 {
		t.Fatal("cross-owner list")
	}
	collections, err := q.Playlists(f.ctx, other.ID, "")
	requireOK(t, err)
	if len(collections) != 0 {
		t.Fatal("cross-owner playlists")
	}
	yes, err := q.ClaimUpdate(f.ctx, 100, 1)
	requireOK(t, err)
	if !yes {
		t.Fatal("first update skipped")
	}
	yes, err = postgres.NewBotQueries(f.pool).ClaimUpdate(f.ctx, 100, 1)
	requireOK(t, err)
	if yes {
		t.Fatal("duplicate update claimed")
	}
	yes, err = q.ClaimUpdate(f.ctx, 101, 1)
	requireOK(t, err)
	if !yes {
		t.Fatal("different bot not isolated")
	}
}
