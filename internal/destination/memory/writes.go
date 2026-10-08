package memory

import (
	"context"
	"encoding/json"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
)

func (d *Destination) EnsureTrack(ctx context.Context, r destination.EnsureRequest) (destination.Effect, error) {
	if r.Target.ConnectionID != d.connection {
		return destination.Effect{}, domain.ErrNotFound
	}
	if !d.caps.Supports(r.Target.Kind) {
		return destination.Effect{}, destination.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return destination.Effect{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ensure(ctx, r)
}
func (d *Destination) ensure(ctx context.Context, r destination.EnsureRequest) (destination.Effect, error) {
	if err := ctx.Err(); err != nil {
		return destination.Effect{}, err
	}
	if !destination.ValidKey(r.OperationKey) || !destination.ValidKey(r.TrackID) {
		return destination.Effect{}, domain.ErrInvalid
	}
	c, ok := d.collections[r.Target.ExternalID]
	if !ok || c.Kind != r.Target.Kind {
		return destination.Effect{}, domain.ErrNotFound
	}
	if _, ok := d.tracks[r.TrackID]; !ok {
		return destination.Effect{}, domain.ErrNotFound
	}
	// JSON tuple avoids delimiter ambiguity in opaque track/collection identifiers.
	encoded, _ := json.Marshal([]string{"add", c.ID, string(c.Kind), r.TrackID})
	sig := string(encoded)
	if old, ok := d.operations[r.OperationKey]; ok {
		if old.signature != sig {
			return destination.Effect{}, domain.ErrConflict
		}
		return old.effect, nil
	}
	state := destination.EffectApplied
	if d.memberships[c.ID][r.TrackID] {
		state = destination.EffectAlreadyPresent
	}
	d.memberships[c.ID][r.TrackID] = true
	e := destination.Effect{State: state, Receipt: r.OperationKey}
	d.operations[r.OperationKey] = operation{signature: sig, effect: e}
	return e, nil
}
func (d *Destination) add(ctx context.Context, r destination.AddRequest, kind domain.DestinationCollectionKind) (destination.AddResult, error) {
	if err := ctx.Err(); err != nil {
		return destination.AddResult{}, err
	}
	if !d.caps.Supports(kind) {
		return destination.AddResult{}, destination.ErrUnsupported
	}
	limit := 1
	if d.caps.SupportsBulkAdd {
		limit = d.caps.MaxBulkSize
	}
	if len(r.Items) < 1 || len(r.Items) > limit {
		return destination.AddResult{}, domain.ErrInvalid
	}
	out := destination.AddResult{Items: make([]destination.ItemResult, 0, len(r.Items))}
	for _, item := range r.Items {
		effect, err := d.EnsureTrack(ctx, destination.EnsureRequest{OperationKey: item.OperationKey, TrackID: item.TrackID, Target: domain.Target{ConnectionID: d.connection, ExternalID: r.CollectionID, Kind: kind}})
		if err != nil {
			return out, err
		} // explicitly partial: caller must reconcile/replay per item
		out.Items = append(out.Items, destination.ItemResult{OperationKey: item.OperationKey, TrackID: item.TrackID, Effect: effect})
	}
	return out, nil
}
func (d *Destination) AddToFavorites(ctx context.Context, items []destination.AddItem) (destination.AddResult, error) {
	return d.add(ctx, destination.AddRequest{CollectionID: "favorites", Items: items}, domain.CollectionFavorites)
}
func (d *Destination) AddTracksToPlaylist(ctx context.Context, r destination.AddRequest) (destination.AddResult, error) {
	return d.add(ctx, r, domain.CollectionPlaylist)
}
func (d *Destination) AddTracksToAlbum(ctx context.Context, r destination.AddRequest) (destination.AddResult, error) {
	return d.add(ctx, r, domain.CollectionAlbum)
}
func (d *Destination) Contains(ctx context.Context, target domain.Target, track string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !d.caps.SupportsMembershipLookup {
		return false, destination.ErrUnsupported
	}
	if target.ConnectionID != d.connection {
		return false, domain.ErrNotFound
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.collections[target.ExternalID]
	if !ok || c.Kind != target.Kind {
		return false, domain.ErrNotFound
	}
	return d.memberships[c.ID][track], nil
}
func (d *Destination) Reconcile(ctx context.Context, key, receipt string) (destination.Effect, error) {
	if err := ctx.Err(); err != nil {
		return destination.Effect{}, err
	}
	if !d.caps.ReconcileOperations {
		return destination.Effect{}, destination.ErrUnsupported
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if op, ok := d.operations[key]; ok {
		return op.effect, nil
	}
	return destination.Effect{State: destination.EffectUnknown}, nil
}
