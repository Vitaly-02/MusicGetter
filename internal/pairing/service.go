// Package pairing handles only MusicGetter credentials, never streaming credentials.
package pairing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"strings"
	"time"

	"musicgetter/internal/domain"
)

type Store interface {
	IssueCode(context.Context, domain.ID, []byte) (time.Time, error)
	RedeemCode(context.Context, []byte, []byte) (domain.ExtensionSession, error)
	Authenticate(context.Context, []byte) (domain.ExtensionSession, error)
	RevokeAll(context.Context, domain.ID) error
}
type Service struct{ store Store }

func New(store Store) *Service   { return &Service{store: store} }
func digest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func (s *Service) Create(ctx context.Context, owner domain.ID) (string, time.Time, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, err
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
	expires, err := s.store.IssueCode(ctx, owner, digest(code))
	if err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}
func (s *Service) Redeem(ctx context.Context, code string) (string, domain.ExtensionSession, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(code)
	if err != nil || len(raw) != 16 || base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw) != code {
		return "", domain.ExtensionSession{}, domain.ErrInvalid
	}
	var tokenBytes [32]byte
	if _, err = rand.Read(tokenBytes[:]); err != nil {
		return "", domain.ExtensionSession{}, err
	}
	token := "mge_" + base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	session, err := s.store.RedeemCode(ctx, digest(code), digest(token))
	if err != nil {
		return "", domain.ExtensionSession{}, err
	}
	return token, session, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (domain.ExtensionSession, error) {
	if len(token) != 47 || !strings.HasPrefix(token, "mge_") {
		return domain.ExtensionSession{}, domain.ErrInvalid
	}
	return s.store.Authenticate(ctx, digest(token))
}
func (s *Service) RevokeAll(ctx context.Context, owner domain.ID) error {
	return s.store.RevokeAll(ctx, owner)
}
