//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"musicgetter/internal/api"
	"musicgetter/internal/destination"
	"musicgetter/internal/destination/fake"
	"musicgetter/internal/destination/memory"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/matcher"
	"musicgetter/internal/pairing"
	"musicgetter/internal/storage/postgres"
	"net/http/httptest"
	"testing"
	"time"
)

func destinationPipeline(t *testing.T, f fixture, d destination.Destination) *importer.Pipeline {
	t.Helper()
	engine, err := matcher.NewEngine(matcher.DefaultOptions())
	requireOK(t, err)
	return &importer.Pipeline{Store: postgres.NewPipelineRepository(f.pool), Matcher: engine, Resolver: importer.Registry{"fake": func(ctx context.Context, c domain.DestinationConnection) (importer.Binding, error) {
		if c.OwnerID != f.user.ID || c.ID != f.connection.ID {
			return importer.Binding{}, domain.ErrNotFound
		}
		caps, err := d.Capabilities(ctx)
		if err != nil {
			return importer.Binding{}, err
		}
		if err = destination.CheckAdapter(d, caps); err != nil {
			return importer.Binding{}, err
		}
		return importer.Binding{Destination: d}, nil
	}}}
}
func referenceDestination(t *testing.T, f fixture, albums bool) *fake.Destination {
	t.Helper()
	caps := memory.FullCapabilities()
	caps.SupportsAlbumCollections = albums
	collections := []destination.Collection{{ID: "playlist", Kind: domain.CollectionPlaylist, Title: "Playlist"}}
	if albums {
		collections = append(collections, destination.Collection{ID: "album", Kind: domain.CollectionAlbum, Title: "Album"})
	}
	d, err := memory.New(memory.Config{OwnerID: f.user.ID, ConnectionID: f.connection.ID, Capabilities: caps, Collections: collections, Tracks: []domain.DestinationTrack{
		{ExternalKey: "a", Metadata: domain.TrackMetadata{Title: "Song A", Artists: []string{"Artist"}, Album: "Album"}},
		{ExternalKey: "b", Metadata: domain.TrackMetadata{Title: "Song B", Artists: []string{"Artist"}, Album: "Album"}},
	}})
	requireOK(t, err)
	return fake.New(d)
}
func httpClient(t *testing.T, f fixture) func(string, string, any, int) []byte {
	t.Helper()
	auth := pairing.New(postgres.NewSessionRepository(f.pool))
	uploads := postgres.NewUploadRepository(f.pool)
	routes := api.NewExtensionAPI(auth, uploads, uploads, api.ExtensionPolicy{})
	handler := api.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), api.NewHealth(func(context.Context) error { return nil }, time.Second), 10*time.Second, routes.Register)
	token := ""
	request := func(method, path string, body any, want int) []byte {
		t.Helper()
		raw, err := json.Marshal(body)
		requireOK(t, err)
		if body == nil {
			raw = nil
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d", method, path, w.Code, want)
		}
		return w.Body.Bytes()
	}
	code, _, err := auth.Create(f.ctx, f.user.ID)
	requireOK(t, err)
	var claim struct {
		Token string `json:"token"`
	}
	requireOK(t, json.Unmarshal(request("POST", "/v1/pair/claim", map[string]string{"code": code}, 201), &claim))
	token = claim.Token
	return request
}
func TestDestinationHTTPWorkerEndToEnd(t *testing.T) {
	for _, kind := range []string{"favorites", "playlist", "album", "album_fallback", "selection"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			remote := referenceDestination(t, f, kind != "album_fallback")
			targetKind := domain.CollectionFavorites
			external := "favorites"
			sourceKind := domain.SourceFavorites
			switch kind {
			case "playlist":
				targetKind = domain.CollectionPlaylist
				external = "playlist"
				sourceKind = domain.SourcePlaylist
			case "album", "album_fallback":
				targetKind = domain.CollectionAlbum
				external = "album"
				sourceKind = domain.SourceAlbum
			case "selection":
				sourceKind = domain.SourceSelection
			}
			target, err := postgres.NewDestinationCollectionRepository(f.pool).Ensure(f.ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: external, Kind: targetKind, Title: "Album"})
			requireOK(t, err)
			if kind == "album_fallback" {
				remote.FailNext(fake.CreatePlaylist, fake.Fault{AfterApply: true})
				remote.FailNext(fake.Ensure, fake.Fault{AfterApply: true})
			}
			p := destinationPipeline(t, f, remote)
			worker := importer.Worker{Queue: postgres.NewJobRepository(f.pool), Repairer: postgres.NewPipelineRepository(f.pool), Pipeline: p, Options: importer.WorkerOptions{ID: "e2e", Concurrency: 2, Lease: time.Second, PollInterval: 10 * time.Millisecond, JobTimeout: 5 * time.Second, RetryBase: time.Millisecond, RetryMax: 10 * time.Millisecond}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			ctx, cancel := context.WithCancel(f.ctx)
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx) }()
			defer func() { cancel(); <-done }()
			request := httpClient(t, f)
			for repeat := range 2 {
				input := uploadRequest(f, fmt.Sprintf("request-%d", repeat))
				input.DestinationCollectionID = target.ID
				input.Source.Kind = sourceKind
				input.Source.CollectionKey = "e2e"
				input.Source.Title = "Album"
				var created importer.Created
				requireOK(t, json.Unmarshal(request("POST", "/v1/imports", input, 201), &created))
				path := "/v1/imports/" + string(created.ID)
				chunk := uploadChunk(0, "chunk", "Song A", "Song B", "Song A")
				for n := range chunk.Tracks {
					chunk.Tracks[n].Album = "Album"
				}
				request("POST", path+"/tracks", chunk, 200)
				request("POST", path+"/tracks", chunk, 200)
				request("POST", path+"/complete", uploadComplete(0, 3), 200)
				deadline := time.Now().Add(5 * time.Second)
				for {
					record, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, created.ID)
					requireOK(t, err)
					if record.State == domain.ImportCompleted {
						break
					}
					if record.State == domain.ImportCompletedWithErrors || time.Now().After(deadline) {
						t.Fatal("pipeline did not complete", record.State)
					}
					time.Sleep(10 * time.Millisecond)
				}
				var progress importer.UploadView
				requireOK(t, json.Unmarshal(request("GET", path, nil, 200), &progress))
				if progress.TotalTracks != 2 || progress.Added+progress.AlreadyPresent != 2 || progress.Failed != 0 {
					t.Fatal("incorrect progress", progress)
				}
				if repeat == 1 && progress.AlreadyPresent != 2 {
					t.Fatal("repeated import added tracks")
				}
				request("POST", "/v1/imports", input, 200) // original target/digest still immutable
				record, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, created.ID)
				requireOK(t, err)
				if record.DestinationCollectionID != target.ID {
					t.Fatal("requested target overwritten")
				}
				if kind == "album_fallback" {
					if record.ResolvedDestinationCollectionID == nil {
						t.Fatal("no fallback binding")
					}
					resolved, err := postgres.NewDestinationCollectionRepository(f.pool).Get(f.ctx, f.user.ID, *record.ResolvedDestinationCollectionID)
					requireOK(t, err)
					if resolved.Kind != domain.CollectionPlaylist || resolved.Title != "Artist — Album" {
						t.Fatal("bad fallback", resolved.Kind, resolved.Title)
					}
					external = resolved.ExternalKey
				}
			}
			var page destination.TrackPage
			switch targetKind {
			case domain.CollectionFavorites:
				page, err = remote.GetFavorites(f.ctx, destination.PageRequest{Limit: 10})
			case domain.CollectionPlaylist:
				page, err = remote.GetPlaylistTracks(f.ctx, external, destination.PageRequest{Limit: 10})
			case domain.CollectionAlbum:
				if kind == "album_fallback" {
					page, err = remote.GetPlaylistTracks(f.ctx, external, destination.PageRequest{Limit: 10})
				} else {
					page, err = remote.GetAlbumTracks(f.ctx, external, destination.PageRequest{Limit: 10})
				}
			}
			requireOK(t, err)
			if len(page.Items) != 2 {
				t.Fatal("remote duplicates/missing tracks")
			}
			var memberships int
			requireOK(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM musicgetter.destination_memberships WHERE owner_id=$1`, f.user.ID).Scan(&memberships))
			if memberships != 2 {
				t.Fatal("DB membership duplicated")
			}
			if kind == "album_fallback" {
				found, err := remote.FindPlaylist(f.ctx, destination.FindPlaylistRequest{Title: "Artist — Album", Page: destination.PageRequest{Limit: 10}})
				requireOK(t, err)
				if len(found.Items) != 1 {
					t.Fatal("create retry duplicated playlist")
				}
			}
		})
	}
}
