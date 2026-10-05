package pairing

import (
	"bytes"
	"context"
	"encoding/base32"
	"musicgetter/internal/domain"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	codeHash, tokenHash []byte
	owner               domain.ID
	err                 error
	calls               int
}

func (f *fakeStore) IssueCode(_ context.Context, o domain.ID, h []byte) (time.Time, error) {
	f.codeHash = h
	f.owner = o
	return time.Now().Add(5 * time.Minute), f.err
}
func (f *fakeStore) RedeemCode(_ context.Context, c, t []byte) (domain.ExtensionSession, error) {
	f.calls++
	if !bytes.Equal(c, f.codeHash) {
		return domain.ExtensionSession{}, domain.ErrNotFound
	}
	f.tokenHash = t
	return domain.ExtensionSession{OwnerID: f.owner}, f.err
}
func (f *fakeStore) Authenticate(_ context.Context, h []byte) (domain.ExtensionSession, error) {
	if !bytes.Equal(h, f.tokenHash) {
		return domain.ExtensionSession{}, domain.ErrNotFound
	}
	return domain.ExtensionSession{OwnerID: f.owner}, nil
}
func (f *fakeStore) RevokeAll(context.Context, domain.ID) error { return f.err }
func TestCredentialsHaveEntropyAndOnlyHashesReachStorage(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{}
	s := New(store)
	code, _, err := s.Create(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(code)
	if err != nil || len(decoded) != 16 || len(store.codeHash) != 32 || !bytes.Equal(store.codeHash, digest(code)) {
		t.Fatal("invalid code or persisted plaintext")
	}
	token, session, err := s.Redeem(ctx, " "+strings.ToLower(code)+" ")
	if err != nil || session.OwnerID != "owner" || len(token) != 47 || len(store.tokenHash) != 32 || !bytes.Equal(store.tokenHash, digest(token)) {
		t.Fatal("invalid token")
	}
	if _, err = s.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, "invalid"); err == nil {
		t.Fatal("invalid bearer accepted")
	}
	code2, _, _ := s.Create(ctx, "owner")
	if code == code2 {
		t.Fatal("reused randomness")
	}
	store.err = domain.ErrStorage
	if token, _, err = s.Redeem(ctx, code2); err == nil || token != "" {
		t.Fatal("failed redemption leaked token")
	}
	if code, _, err = s.Create(ctx, "owner"); err == nil || code != "" {
		t.Fatal("failed issue leaked code")
	}
}
func TestRejectMalformedCodesBeforeStorage(t *testing.T) {
	f := &fakeStore{}
	s := New(f)
	for _, code := range []string{"", "123456", strings.Repeat("A", 25), strings.Repeat("!", 26), strings.Repeat("A", 25) + "B"} {
		if _, _, err := s.Redeem(context.Background(), code); err == nil {
			t.Fatal("invalid code accepted")
		}
	}
	if f.calls != 0 {
		t.Fatal("malformed code queried storage")
	}
}
