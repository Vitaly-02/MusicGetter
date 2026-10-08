package destination

import (
	"context"
	"errors"
	"musicgetter/internal/domain"
	"strings"
	"unicode/utf8"
)

var (
	ErrUnsupported = errors.New("destination_unsupported")
	ErrUnavailable = errors.New("destination_unavailable")
)

// All methods are bound to one authenticated account. IDs/cursors are opaque;
// pages have keyset semantics, not a snapshot of a changing remote library.
type PageRequest struct {
	Cursor string
	Limit  int
}

func (p PageRequest) Validate() error {
	if p.Limit < 1 || p.Limit > 200 || len(p.Cursor) > 2048 {
		return domain.ErrInvalid
	}
	return nil
}

type Collection struct {
	ID    string
	Kind  domain.DestinationCollectionKind
	Title string
}
type CollectionPage struct {
	Items      []Collection
	NextCursor string
}
type TrackPage struct {
	Items      []domain.DestinationTrack
	NextCursor string
}
type FindPlaylistRequest struct {
	Title string
	Page  PageRequest
}

// Names are not unique identifiers. FindPlaylist returns all exact-name matches
// in bounded pages; callers must not silently choose among duplicates.
type CreateCollectionRequest struct {
	OperationKey string
	Title        string
}
type CollectionResult struct {
	Collection Collection
	Effect     Effect
}
type AddItem struct {
	OperationKey string
	TrackID      string
}
type AddRequest struct {
	CollectionID string
	Items        []AddItem
}
type AddResult struct{ Items []ItemResult }
type ItemResult struct {
	OperationKey string
	TrackID      string
	Effect       Effect
}

// Bulk results may be partial/unknown. Every item has its own immutable key;
// a batch transport error is not evidence that no remote effects occurred.
type Favorites interface {
	GetFavorites(context.Context, PageRequest) (TrackPage, error)
	AddToFavorites(context.Context, []AddItem) (AddResult, error)
}
type Playlists interface {
	ListPlaylists(context.Context, PageRequest) (CollectionPage, error)
	FindPlaylist(context.Context, FindPlaylistRequest) (CollectionPage, error)
	CreatePlaylist(context.Context, CreateCollectionRequest) (CollectionResult, error)
	GetPlaylistTracks(context.Context, string, PageRequest) (TrackPage, error)
	AddTracksToPlaylist(context.Context, AddRequest) (AddResult, error)
}

// Albums is optional: collections group tracks, without implying catalog rights
// to publish/edit an artist's actual release.
type Albums interface {
	ListAlbums(context.Context, PageRequest) (CollectionPage, error)
	CreateAlbum(context.Context, CreateCollectionRequest) (CollectionResult, error)
	GetAlbumTracks(context.Context, string, PageRequest) (TrackPage, error)
	AddTracksToAlbum(context.Context, AddRequest) (AddResult, error)
}

func ValidKey(s string) bool {
	return utf8.ValidString(s) && len(s) <= 512 && strings.TrimSpace(s) != "" && !strings.ContainsRune(s, 0)
}
func ValidTitle(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= 1024 && strings.TrimSpace(s) != "" && !strings.ContainsRune(s, 0)
}

// Display only: identity is a persisted operation key, never this potentially
// truncated or non-unique title. No artist aliases/album artist are inferred.
func AlbumPlaylistTitle(artists []string, album string) (string, error) {
	if len(artists) == 0 || strings.TrimSpace(album) == "" || !utf8.ValidString(album) {
		return "", domain.ErrInvalid
	}
	for _, artist := range artists {
		if strings.TrimSpace(artist) == "" || !utf8.ValidString(artist) {
			return "", domain.ErrInvalid
		}
	}
	clip := func(s string, n int) string {
		r := []rune(strings.TrimSpace(s))
		if len(r) > n {
			return string(r[:n-1]) + "…"
		}
		return string(r)
	}
	title := clip(strings.Join(artists, ", "), 510) + " — " + clip(album, 511)
	if !ValidTitle(title) {
		return "", domain.ErrInvalid
	}
	return title, nil
}
