package api

import (
	"context"
	"errors"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"net/http"
	"strings"
	"time"
)

type ExtensionReads interface {
	User(context.Context, domain.ID) (domain.User, error)
	Destinations(context.Context, domain.ID, string, int) ([]importer.DestinationView, error)
}
type ExtensionAPI struct {
	auth         ExtensionAuth
	uploads      importer.UploadStore
	reads        ExtensionReads
	policyConfig ExtensionPolicy
	limiter      *RateLimiter
}

func NewExtensionAPI(auth ExtensionAuth, uploads importer.UploadStore, reads ExtensionReads, policy ExtensionPolicy) *ExtensionAPI {
	return &ExtensionAPI{auth: auth, uploads: uploads, reads: reads, policyConfig: policy.defaults(), limiter: NewRateLimiter(10000)}
}
func (a *ExtensionAPI) Register(mux *http.ServeMux) {
	mux.Handle("/v1/", a.policy(http.HandlerFunc(a.route)))
}
func (a *ExtensionAPI) authenticate(w http.ResponseWriter, r *http.Request) (domain.ExtensionSession, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || len(r.Header.Values("Authorization")) != 1 || !strings.EqualFold(scheme, "Bearer") || len(token) != 47 || !strings.HasPrefix(token, "mge_") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="MusicGetter"`)
		authError(w, r, domain.ErrInvalid)
		return domain.ExtensionSession{}, false
	}
	session, err := a.auth.Authenticate(r.Context(), token)
	if err != nil {
		authError(w, r, err)
		return session, false
	}
	if !a.allow(w, r, "owner:"+string(session.OwnerID), a.policyConfig.OwnerPerMinute) {
		return session, false
	}
	return session, true
}
func method(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed {
		return true
	}
	w.Header().Set("Allow", allowed)
	writeError(w, r, 405, "method_not_allowed", "Method not allowed")
	return false
}
func (a *ExtensionAPI) route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/v1/pair/claim" || path == "/v1/pairings/redeem" {
		if method(w, r, "POST") {
			a.claim(w, r)
		}
		return
	}
	session, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	switch path {
	case "/v1/me":
		if method(w, r, "GET") {
			a.me(w, r, session)
		}
		return
	case "/v1/extension/session":
		if method(w, r, "GET") {
			writeJSON(w, 200, session)
		}
		return
	case "/v1/destinations":
		if method(w, r, "GET") {
			a.destinations(w, r, session.OwnerID)
		}
		return
	case "/v1/imports":
		if method(w, r, "POST") {
			a.create(w, r, session.OwnerID)
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "imports" || !importer.ValidID(parts[1]) {
		writeError(w, r, 404, "not_found", "Route not found")
		return
	}
	id := domain.ID(parts[1])
	if len(parts) == 2 {
		if method(w, r, "GET") {
			a.status(w, r, session.OwnerID, id)
		}
		return
	}
	if parts[2] != "tracks" && parts[2] != "complete" && parts[2] != "cancel" {
		writeError(w, r, 404, "not_found", "Route not found")
		return
	}
	if !method(w, r, "POST") {
		return
	}
	switch parts[2] {
	case "tracks":
		a.tracks(w, r, session.OwnerID, id)
	case "complete":
		a.complete(w, r, session.OwnerID, id)
	case "cancel":
		a.cancel(w, r, session.OwnerID, id)
	}
}
func (a *ExtensionAPI) claim(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		writeError(w, r, 400, "credentials_not_allowed", "Pairing accepts a code, not an Authorization header")
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	if !strictJSON(w, r, 1024, &input) {
		return
	}
	token, session, err := a.auth.Redeem(r.Context(), input.Code)
	if err != nil {
		authError(w, r, err)
		return
	}
	if r.URL.Path == "/v1/pairings/redeem" {
		writeJSON(w, 201, struct {
			Token   string                  `json:"token"`
			Type    string                  `json:"tokenType"`
			Session domain.ExtensionSession `json:"session"`
		}{token, "Bearer", session})
		return
	}
	writeJSON(w, 201, struct {
		Token   string    `json:"token"`
		Type    string    `json:"token_type"`
		Expires time.Time `json:"expires_at"`
	}{token, "Bearer", session.ExpiresAt})
}
func apiError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		writeError(w, r, 422, "validation_failed", "Request fields or import limits are invalid")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, 404, "not_found", "Resource not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, r, 409, "conflict", "Idempotency payload, import state or chunk range conflicts")
	default:
		w.Header().Set("Retry-After", "2")
		writeError(w, r, 503, "unavailable", "Service unavailable; retry with the same idempotency key")
	}
}
