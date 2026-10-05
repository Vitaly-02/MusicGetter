package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"musicgetter/internal/domain"
	"net/http"
	"strings"
)

type ExtensionAuth interface {
	Redeem(context.Context, string) (string, domain.ExtensionSession, error)
	Authenticate(context.Context, string) (domain.ExtensionSession, error)
}

func ExtensionRoutes(auth ExtensionAuth) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/pairings/redeem", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				writeError(w, r, 405, "method_not_allowed", "Method not allowed")
				return
			}
			if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
				writeError(w, r, 415, "unsupported_media_type", "Use application/json")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1024)
			var input struct {
				Code string `json:"code"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				writeError(w, r, 400, "invalid_request", "Invalid JSON")
				return
			}
			if err := decoder.Decode(&struct{}{}); err != io.EOF {
				writeError(w, r, 400, "invalid_request", "Invalid JSON")
				return
			}
			token, session, err := auth.Redeem(r.Context(), input.Code)
			if err != nil {
				authError(w, r, err)
				return
			}
			writeJSON(w, 201, struct {
				Token     string                  `json:"token"`
				TokenType string                  `json:"tokenType"`
				Session   domain.ExtensionSession `json:"session"`
			}{token, "Bearer", session})
		})
		mux.HandleFunc("/v1/extension/session", getOnly(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			value := r.Header.Get("Authorization")
			scheme, token, ok := strings.Cut(value, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") {
				authError(w, r, domain.ErrInvalid)
				return
			}
			session, err := auth.Authenticate(r.Context(), token)
			if err != nil {
				authError(w, r, err)
				return
			}
			writeJSON(w, 200, session)
		}))
	}
}
func authError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrNotFound) {
		writeError(w, r, 401, "invalid_credentials", "Credentials expired, revoked or invalid")
		return
	}
	writeError(w, r, 503, "unavailable", "Try again later")
}
