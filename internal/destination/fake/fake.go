// Package fake adds deterministic failures to the in-memory reference destination.
// Never register it in the production worker: its remote state is test-owned RAM.
package fake

import (
	"context"
	"musicgetter/internal/destination"
	"musicgetter/internal/destination/memory"
	"musicgetter/internal/matcher"
	"sync"
)

type Operation string

const (
	Search         Operation = "search"
	Ensure         Operation = "ensure"
	CreatePlaylist Operation = "create_playlist"
	CreateAlbum    Operation = "create_album"
	AddFavorites   Operation = "add_favorites"
	AddPlaylist    Operation = "add_playlist"
	AddAlbum       Operation = "add_album"
)

type Fault struct{ AfterApply bool }
type Destination struct {
	*memory.Destination
	mu     sync.Mutex
	faults map[Operation][]Fault
	calls  map[Operation]int
}

func New(remote *memory.Destination) *Destination {
	return &Destination{Destination: remote, faults: map[Operation][]Fault{}, calls: map[Operation]int{}}
}
func (d *Destination) FailNext(op Operation, f Fault) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.faults[op] = append(d.faults[op], f)
}
func (d *Destination) Calls(op Operation) int { d.mu.Lock(); defer d.mu.Unlock(); return d.calls[op] }
func invoke[T any](d *Destination, op Operation, fn func() (T, error)) (T, error) {
	d.mu.Lock()
	d.calls[op]++
	var fault *Fault
	if queue := d.faults[op]; len(queue) > 0 {
		f := queue[0]
		fault = &f
		d.faults[op] = queue[1:]
	}
	d.mu.Unlock()
	var empty T
	if fault != nil && !fault.AfterApply {
		return empty, destination.ErrUnavailable
	}
	value, err := fn()
	if err == nil && fault != nil {
		return empty, destination.ErrUnavailable
	}
	return value, err
}
func (d *Destination) SearchTracks(ctx context.Context, q matcher.Query) (matcher.CandidatePage, error) {
	return invoke(d, Search, func() (matcher.CandidatePage, error) { return d.Destination.SearchTracks(ctx, q) })
}
func (d *Destination) EnsureTrack(ctx context.Context, r destination.EnsureRequest) (destination.Effect, error) {
	return invoke(d, Ensure, func() (destination.Effect, error) { return d.Destination.EnsureTrack(ctx, r) })
}
func (d *Destination) CreatePlaylist(ctx context.Context, r destination.CreateCollectionRequest) (destination.CollectionResult, error) {
	return invoke(d, CreatePlaylist, func() (destination.CollectionResult, error) { return d.Destination.CreatePlaylist(ctx, r) })
}
func (d *Destination) CreateAlbum(ctx context.Context, r destination.CreateCollectionRequest) (destination.CollectionResult, error) {
	return invoke(d, CreateAlbum, func() (destination.CollectionResult, error) { return d.Destination.CreateAlbum(ctx, r) })
}
func (d *Destination) AddToFavorites(ctx context.Context, r []destination.AddItem) (destination.AddResult, error) {
	return invoke(d, AddFavorites, func() (destination.AddResult, error) { return d.Destination.AddToFavorites(ctx, r) })
}
func (d *Destination) AddTracksToPlaylist(ctx context.Context, r destination.AddRequest) (destination.AddResult, error) {
	return invoke(d, AddPlaylist, func() (destination.AddResult, error) { return d.Destination.AddTracksToPlaylist(ctx, r) })
}
func (d *Destination) AddTracksToAlbum(ctx context.Context, r destination.AddRequest) (destination.AddResult, error) {
	return invoke(d, AddAlbum, func() (destination.AddResult, error) { return d.Destination.AddTracksToAlbum(ctx, r) })
}
