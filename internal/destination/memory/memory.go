// Package memory provides an account-scoped in-memory reference destination.
// It is suitable for local tests, not a durable production music service.
package memory

import (
	"context"
	"fmt"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	"slices"
	"sync"
)

type Config struct {
	OwnerID, ConnectionID domain.ID
	Capabilities          destination.Capabilities
	Tracks                []domain.DestinationTrack
	Collections           []destination.Collection
}
type operation struct {
	signature  string
	effect     destination.Effect
	collection destination.Collection
}
type Destination struct {
	mu                sync.Mutex
	owner, connection domain.ID
	caps              destination.Capabilities
	tracks            map[string]domain.DestinationTrack
	collections       map[string]destination.Collection
	memberships       map[string]map[string]bool
	operations        map[string]operation
	next              uint64
}

func FullCapabilities() destination.Capabilities {
	return destination.Capabilities{Search: true, SupportsFavorites: true, SupportsPlaylists: true, SupportsAlbumCollections: true, SupportsBulkAdd: true, MaxBulkSize: 200, SupportsMembershipLookup: true, AtomicEnsureMembership: true, NativeIdempotency: true, ReconcileOperations: true, IdempotentCreatePlaylist: true}
}
func New(cfg Config) (*Destination, error) {
	if cfg.OwnerID == "" || cfg.ConnectionID == "" {
		return nil, domain.ErrInvalid
	}
	if err := cfg.Capabilities.Validate(); err != nil {
		return nil, err
	}
	d := &Destination{owner: cfg.OwnerID, connection: cfg.ConnectionID, caps: cfg.Capabilities, tracks: map[string]domain.DestinationTrack{}, collections: map[string]destination.Collection{}, memberships: map[string]map[string]bool{}, operations: map[string]operation{}}
	for _, track := range cfg.Tracks {
		if !destination.ValidKey(track.ExternalKey) || domain.ValidateMetadata(track.Metadata) != nil {
			return nil, domain.ErrInvalid
		}
		if _, ok := d.tracks[track.ExternalKey]; ok {
			return nil, domain.ErrConflict
		}
		track.ID = ""
		track.OwnerID = d.owner
		track.ConnectionID = d.connection
		d.tracks[track.ExternalKey] = cloneTrack(track)
	}
	if d.caps.SupportsFavorites {
		d.collections["favorites"] = destination.Collection{ID: "favorites", Kind: domain.CollectionFavorites, Title: "Favorites"}
		d.memberships["favorites"] = map[string]bool{}
	}
	for _, c := range cfg.Collections {
		if !destination.ValidKey(c.ID) || !destination.ValidTitle(c.Title) || !d.caps.Supports(c.Kind) {
			return nil, domain.ErrInvalid
		}
		if _, ok := d.collections[c.ID]; ok {
			return nil, domain.ErrConflict
		}
		if c.Kind == domain.CollectionFavorites {
			return nil, domain.ErrConflict
		}
		d.collections[c.ID] = c
		d.memberships[c.ID] = map[string]bool{}
	}
	return d, nil
}
func (d *Destination) Capabilities(ctx context.Context) (destination.Capabilities, error) {
	return d.caps, ctx.Err()
}
func cloneTrack(t domain.DestinationTrack) domain.DestinationTrack {
	t.Metadata.Artists = slices.Clone(t.Metadata.Artists)
	if t.Metadata.DurationMS != nil {
		v := *t.Metadata.DurationMS
		t.Metadata.DurationMS = &v
	}
	return t
}
func (d *Destination) create(ctx context.Context, r destination.CreateCollectionRequest, kind domain.DestinationCollectionKind) (destination.CollectionResult, error) {
	if err := ctx.Err(); err != nil {
		return destination.CollectionResult{}, err
	}
	if !d.caps.Supports(kind) {
		return destination.CollectionResult{}, destination.ErrUnsupported
	}
	if !destination.ValidKey(r.OperationKey) || !destination.ValidTitle(r.Title) {
		return destination.CollectionResult{}, domain.ErrInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return destination.CollectionResult{}, err
	}
	sig := fmt.Sprintf("create:%s:%s", kind, r.Title)
	if old, ok := d.operations[r.OperationKey]; ok {
		if old.signature != sig {
			return destination.CollectionResult{}, domain.ErrConflict
		}
		return destination.CollectionResult{Collection: old.collection, Effect: old.effect}, nil
	}
	var id string
	for {
		d.next++
		id = fmt.Sprintf("%s-%020d", kind, d.next)
		if _, ok := d.collections[id]; !ok {
			break
		}
	}
	c := destination.Collection{ID: id, Kind: kind, Title: r.Title}
	e := destination.Effect{State: destination.EffectApplied, Receipt: r.OperationKey}
	d.collections[id] = c
	d.memberships[id] = map[string]bool{}
	d.operations[r.OperationKey] = operation{signature: sig, effect: e, collection: c}
	return destination.CollectionResult{Collection: c, Effect: e}, nil
}
func (d *Destination) CreatePlaylist(ctx context.Context, r destination.CreateCollectionRequest) (destination.CollectionResult, error) {
	return d.create(ctx, r, domain.CollectionPlaylist)
}
func (d *Destination) CreateAlbum(ctx context.Context, r destination.CreateCollectionRequest) (destination.CollectionResult, error) {
	return d.create(ctx, r, domain.CollectionAlbum)
}
