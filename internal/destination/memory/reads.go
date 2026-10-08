package memory

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	"musicgetter/internal/matcher"
)

// Cursor scope includes connection, operation and arguments. It is not auth.
type cursor struct{ Scope, After string }

func after(p destination.PageRequest, scope string) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if p.Cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
	if err != nil {
		return "", domain.ErrInvalid
	}
	var c cursor
	if json.Unmarshal(raw, &c) != nil || c.Scope != scope || c.After == "" {
		return "", domain.ErrInvalid
	}
	key, err := base64.RawURLEncoding.DecodeString(c.After)
	if err != nil || !destination.ValidKey(string(key)) {
		return "", domain.ErrInvalid
	}
	return string(key), nil
}
func next(scope, key string) string {
	raw, _ := json.Marshal(cursor{Scope: scope, After: base64.RawURLEncoding.EncodeToString([]byte(key))})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (d *Destination) scope(parts ...string) string {
	raw, _ := json.Marshal(append([]string{string(d.connection)}, parts...))
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func (d *Destination) collectionsPage(ctx context.Context, p destination.PageRequest, kind domain.DestinationCollectionKind, title *string) (destination.CollectionPage, error) {
	if err := ctx.Err(); err != nil {
		return destination.CollectionPage{}, err
	}
	if !d.caps.Supports(kind) {
		return destination.CollectionPage{}, destination.ErrUnsupported
	}
	filter := ""
	if title != nil {
		filter = *title
	}
	scope := d.scope("collections", string(kind), filter)
	last, err := after(p, scope)
	if err != nil {
		return destination.CollectionPage{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := []string{}
	for id, c := range d.collections {
		if id > last && c.Kind == kind && (title == nil || c.Title == *title) {
			ids = boundedIDs(ids, id, p.Limit+1)
		}
	}
	out := destination.CollectionPage{Items: []destination.Collection{}}
	for _, id := range ids[:min(len(ids), p.Limit)] {
		out.Items = append(out.Items, d.collections[id])
	}
	if len(ids) > p.Limit {
		out.NextCursor = next(scope, ids[p.Limit-1])
	}
	return out, nil
}
func (d *Destination) ListPlaylists(ctx context.Context, p destination.PageRequest) (destination.CollectionPage, error) {
	return d.collectionsPage(ctx, p, domain.CollectionPlaylist, nil)
}
func (d *Destination) FindPlaylist(ctx context.Context, r destination.FindPlaylistRequest) (destination.CollectionPage, error) {
	if !destination.ValidTitle(r.Title) {
		return destination.CollectionPage{}, domain.ErrInvalid
	}
	return d.collectionsPage(ctx, r.Page, domain.CollectionPlaylist, &r.Title)
}
func (d *Destination) ListAlbums(ctx context.Context, p destination.PageRequest) (destination.CollectionPage, error) {
	return d.collectionsPage(ctx, p, domain.CollectionAlbum, nil)
}
func (d *Destination) tracksPage(ctx context.Context, id string, p destination.PageRequest, kind domain.DestinationCollectionKind) (destination.TrackPage, error) {
	if err := ctx.Err(); err != nil {
		return destination.TrackPage{}, err
	}
	if !d.caps.Supports(kind) {
		return destination.TrackPage{}, destination.ErrUnsupported
	}
	scope := d.scope("tracks", id, string(kind))
	last, err := after(p, scope)
	if err != nil {
		return destination.TrackPage{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.collections[id]
	if !ok || c.Kind != kind {
		return destination.TrackPage{}, domain.ErrNotFound
	}
	ids := []string{}
	for key := range d.memberships[id] {
		if key > last {
			ids = boundedIDs(ids, key, p.Limit+1)
		}
	}
	out := destination.TrackPage{Items: []domain.DestinationTrack{}}
	for _, key := range ids[:min(len(ids), p.Limit)] {
		out.Items = append(out.Items, cloneTrack(d.tracks[key]))
	}
	if len(ids) > p.Limit {
		out.NextCursor = next(scope, ids[p.Limit-1])
	}
	return out, nil
}
func (d *Destination) GetFavorites(ctx context.Context, p destination.PageRequest) (destination.TrackPage, error) {
	return d.tracksPage(ctx, "favorites", p, domain.CollectionFavorites)
}
func (d *Destination) GetPlaylistTracks(ctx context.Context, id string, p destination.PageRequest) (destination.TrackPage, error) {
	return d.tracksPage(ctx, id, p, domain.CollectionPlaylist)
}
func (d *Destination) GetAlbumTracks(ctx context.Context, id string, p destination.PageRequest) (destination.TrackPage, error) {
	return d.tracksPage(ctx, id, p, domain.CollectionAlbum)
}
func (d *Destination) SearchTracks(ctx context.Context, q matcher.Query) (matcher.CandidatePage, error) {
	if err := ctx.Err(); err != nil {
		return matcher.CandidatePage{}, err
	}
	if !d.caps.Search {
		return matcher.CandidatePage{}, destination.ErrUnsupported
	}
	text := matcher.Normalize(q.Text)
	if text == "" {
		return matcher.CandidatePage{}, domain.ErrInvalid
	}
	scope := d.scope("search", text)
	last, err := after(destination.PageRequest{Limit: q.Limit, Cursor: q.Cursor}, scope)
	if err != nil {
		return matcher.CandidatePage{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := []string{}
	for id, t := range d.tracks {
		if err := ctx.Err(); err != nil {
			return matcher.CandidatePage{}, err
		}
		if id <= last {
			continue
		}
		haystack := matcher.Normalize(t.Metadata.Title + " " + strings.Join(t.Metadata.Artists, " ") + " " + t.Metadata.Album + " " + t.Metadata.Version)
		found := true
		for _, term := range strings.Fields(text) {
			if !strings.Contains(haystack, term) {
				found = false
				break
			}
		}
		if found {
			ids = boundedIDs(ids, id, q.Limit+1)
		}
	}
	out := matcher.CandidatePage{Candidates: []matcher.Candidate{}}
	for _, id := range ids[:min(len(ids), q.Limit)] {
		out.Candidates = append(out.Candidates, matcher.Candidate{Track: cloneTrack(d.tracks[id])})
	}
	if len(ids) > q.Limit {
		out.NextCursor = next(scope, ids[q.Limit-1])
	}
	return out, nil
}

// Keep only the next page and one lookahead key while scanning the reference store.
func boundedIDs(ids []string, id string, limit int) []string {
	at, _ := slices.BinarySearch(ids, id)
	if at >= limit {
		return ids
	}
	ids = slices.Insert(ids, at, id)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}
