package api

import (
	"context"
	"errors"
	"musicgetter/internal/domain"
	"net/http"
)

type ExtensionAuth interface {
	Redeem(context.Context, string) (string, domain.ExtensionSession, error)
	Authenticate(context.Context, string) (domain.ExtensionSession, error)
}

func authError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrNotFound) {
		writeError(w, r, 401, "invalid_credentials", "Credentials expired, revoked or invalid")
		return
	}
	writeError(w, r, 503, "unavailable", "Try again later")
}
