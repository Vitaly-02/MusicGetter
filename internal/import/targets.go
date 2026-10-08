package importer

import (
	"context"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
)

type AlbumFallbackPlan struct {
	OperationKey string
	Title        string
	Resolved     *domain.Target
}
type TargetStore interface {
	PrepareAlbumFallback(context.Context, domain.ImportJob) (AlbumFallbackPlan, error)
	BindAlbumFallback(context.Context, domain.ImportJob, destination.Collection) error
}

func (p *Pipeline) resolveTarget(ctx context.Context, j domain.ImportJob, w Work, b Binding, c destination.Capabilities) (Work, error) {
	if c.Supports(w.Target.Kind) && !w.FallbackPending {
		return w, nil
	}
	if w.Target.Kind != domain.CollectionAlbum || !c.SupportsPlaylists || !c.IdempotentCreatePlaylist {
		return w, ErrUnsupportedDestination
	}
	store, ok := p.Store.(TargetStore)
	if !ok {
		return w, ErrUnsupportedDestination
	}
	playlists, ok := b.Destination.(destination.Playlists)
	if !ok {
		return w, ErrUnsupportedDestination
	}
	// Creation intent is committed before the remote request. Its key/title survive
	// lost ACK, worker restart, cancellation and another import of the same target.
	plan, err := store.PrepareAlbumFallback(ctx, j)
	if err != nil {
		return w, err
	}
	if plan.Resolved == nil {
		if _, err = p.Store.Load(ctx, j); err != nil {
			return w, err
		} // cancellation/fencing before dispatch
		result, err := playlists.CreatePlaylist(ctx, destination.CreateCollectionRequest{OperationKey: plan.OperationKey, Title: plan.Title})
		if err != nil {
			return w, err
		}
		switch result.Effect.State {
		case destination.EffectApplied, destination.EffectAlreadyPresent:
			if result.Collection.Kind != domain.CollectionPlaylist || !destination.ValidKey(result.Collection.ID) {
				return w, domain.ErrInvalid
			}
			if err = store.BindAlbumFallback(ctx, j, result.Collection); err != nil {
				return w, err
			}
		case destination.EffectRejected:
			return w, ErrUnsupportedDestination
		default:
			return w, destination.ErrUnavailable
		}
	}
	return p.Store.Load(ctx, j)
}
