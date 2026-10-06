//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"musicgetter/internal/api"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/pairing"
	"musicgetter/internal/storage/postgres"
)

func TestExtensionAPIEndToEnd(t *testing.T) {
	f := newFixture(t)
	auth := pairing.New(postgres.NewSessionRepository(f.pool))
	uploads := postgres.NewUploadRepository(f.pool)
	routes := api.NewExtensionAPI(auth, uploads, uploads, api.ExtensionPolicy{})
	h := api.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), api.NewHealth(func(context.Context) error { return nil }, time.Second), 10*time.Second, routes.Register)
	request := func(method, path string, body any, token string, want int) []byte {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, want, w.Body)
		}
		return w.Body.Bytes()
	}
	code, _, err := auth.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	var claim struct {
		Token string `json:"token"`
	}
	requireOK(t, json.Unmarshal(request("POST", "/v1/pair/claim", map[string]string{"code": code}, "", 201), &claim))
	request("POST", "/v1/pair/claim", map[string]string{"code": code}, "", 401)
	me := request("GET", "/v1/me", nil, claim.Token, 200)
	if !bytes.Contains(me, []byte(`"telegram_user_id":42`)) {
		t.Fatal("wrong me identity")
	}
	dest := request("GET", "/v1/destinations", nil, claim.Token, 200)
	if !bytes.Contains(dest, []byte(f.destination.ID)) {
		t.Fatal("destination missing")
	}
	var created importer.Created
	requireOK(t, json.Unmarshal(request("POST", "/v1/imports", uploadRequest(f, "http"), claim.Token, 201), &created))
	request("POST", "/v1/imports", uploadRequest(f, "http"), claim.Token, 200)
	path := "/v1/imports/" + string(created.ID)
	chunk := uploadChunk(0, "http-chunk")
	for i := 0; i < 200; i++ {
		chunk.Tracks = append(chunk.Tracks, importer.TrackInput{Title: fmt.Sprintf("Track %d", i), Artists: []string{"Artist"}, Position: ptr(int64(i))})
	}
	var receipt importer.ChunkReceipt
	requireOK(t, json.Unmarshal(request("POST", path+"/tracks", chunk, claim.Token, 200), &receipt))
	if receipt.Added != 200 {
		t.Fatal("chunk not accepted")
	}
	requireOK(t, json.Unmarshal(request("POST", path+"/tracks", chunk, claim.Token, 200), &receipt))
	if !receipt.Replay || receipt.Added != 200 {
		t.Fatal("network retry not idempotent")
	}
	chunk.Tracks[0].Title = "Modified"
	request("POST", path+"/tracks", chunk, claim.Token, 409)
	request("POST", path+"/complete", uploadComplete(0, 200), claim.Token, 200)
	request("POST", path+"/complete", uploadComplete(0, 200), claim.Token, 200)
	var view importer.UploadView
	requireOK(t, json.Unmarshal(request("GET", path, nil, claim.Token, 200), &view))
	if view.TotalTracks != 200 || view.State != domain.ImportQueued {
		t.Fatal("wrong API progress")
	}
	request("POST", path+"/cancel", nil, claim.Token, 200)
	requireOK(t, auth.RevokeAll(f.ctx, f.user.ID))
	request("GET", "/v1/me", nil, claim.Token, 401)
}
