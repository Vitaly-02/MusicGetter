package api

import (
	"bytes"
	"io"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (a *ExtensionAPI) me(w http.ResponseWriter, r *http.Request, s domain.ExtensionSession) {
	user, err := a.reads.User(r.Context(), s.OwnerID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, 200, struct {
		ID         domain.ID `json:"id"`
		TelegramID int64     `json:"telegram_user_id"`
		SessionID  domain.ID `json:"session_id"`
		Expires    time.Time `json:"session_expires_at"`
	}{user.ID, user.TelegramUserID, s.ID, s.ExpiresAt})
}
func (a *ExtensionAPI) destinations(w http.ResponseWriter, r *http.Request, owner domain.ID) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, r, domain.ErrInvalid)
		return
	}
	for key, values := range query {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			writeError(w, r, 400, "invalid_query", "Only cursor and limit are supported")
			return
		}
	}
	limit := 50
	if value, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(value[0])
		if err != nil {
			apiError(w, r, domain.ErrInvalid)
			return
		}
	}
	if limit < 1 || limit > 100 || query.Get("cursor") != "" && !importer.ValidID(query.Get("cursor")) {
		apiError(w, r, domain.ErrInvalid)
		return
	}
	rows, err := a.reads.Destinations(r.Context(), owner, query.Get("cursor"), limit)
	if err != nil {
		apiError(w, r, err)
		return
	}
	if rows == nil {
		rows = []importer.DestinationView{}
	}
	next := ""
	if len(rows) == limit {
		next = string(rows[len(rows)-1].ID)
	}
	writeJSON(w, 200, struct {
		Items []importer.DestinationView `json:"items"`
		Next  string                     `json:"next_cursor,omitempty"`
	}{rows, next})
}
func idempotencyHeader(w http.ResponseWriter, r *http.Request, key string) bool {
	header := r.Header.Get("Idempotency-Key")
	if len(r.Header.Values("Idempotency-Key")) > 1 || len(r.Header.Values("Idempotency-Key")) > 0 && header != key {
		writeError(w, r, 409, "conflict", "Idempotency-Key must match the request key")
		return false
	}
	return true
}
func (a *ExtensionAPI) create(w http.ResponseWriter, r *http.Request, owner domain.ID) {
	var input importer.CreateRequest
	if !strictJSON(w, r, 16*1024, &input) {
		return
	}
	if err := input.Validate(); err != nil {
		apiError(w, r, err)
		return
	}
	if !idempotencyHeader(w, r, input.ClientRequestID) {
		return
	}
	created, err := a.uploads.CreateUpload(r.Context(), owner, input)
	if err != nil {
		apiError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/imports/"+string(created.ID))
	status := 201
	if created.Replay {
		status = 200
	}
	writeJSON(w, status, created)
}
func (a *ExtensionAPI) tracks(w http.ResponseWriter, r *http.Request, owner, id domain.ID) {
	var input importer.ChunkRequest
	if !strictJSON(w, r, importer.MaxPayloadBytes, &input) {
		return
	}
	if err := input.Validate(); err != nil {
		apiError(w, r, err)
		return
	}
	if !idempotencyHeader(w, r, input.IdempotencyKey) {
		return
	}
	receipt, err := a.uploads.AppendChunk(r.Context(), owner, id, input)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, 200, receipt)
}
func (a *ExtensionAPI) complete(w http.ResponseWriter, r *http.Request, owner, id domain.ID) {
	var input importer.CompleteRequest
	if !strictJSON(w, r, 4096, &input) {
		return
	}
	if err := input.Validate(); err != nil {
		apiError(w, r, err)
		return
	}
	if len(r.Header.Values("Idempotency-Key")) > 0 {
		writeError(w, r, 400, "invalid_header", "Complete is idempotent by its immutable body")
		return
	}
	if err := a.uploads.CompleteUpload(r.Context(), owner, id, input); err != nil {
		apiError(w, r, err)
		return
	}
	a.status(w, r, owner, id)
}
func (a *ExtensionAPI) cancel(w http.ResponseWriter, r *http.Request, owner, id domain.ID) {
	// No body means no ignored credential fields. {} is accepted for JSON clients.
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128))
	if err != nil {
		writeError(w, r, 413, "payload_too_large", "Cancel accepts an empty body or {}")
		return
	}
	if len(data) > 0 {
		// Reuse strict decoding without ignoring arbitrary JSON input.
		r.Body = io.NopCloser(bytes.NewReader(data))
		var input struct{}
		if !strictJSON(w, r, 128, &input) {
			return
		}
	}
	if err := a.uploads.CancelUpload(r.Context(), owner, id); err != nil {
		apiError(w, r, err)
		return
	}
	a.status(w, r, owner, id)
}
func (a *ExtensionAPI) status(w http.ResponseWriter, r *http.Request, owner, id domain.ID) {
	v, err := a.uploads.GetUpload(r.Context(), owner, id)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}
