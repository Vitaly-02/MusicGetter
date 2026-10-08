package importer

import (
	"context"
	"errors"
	"slices"
	"time"

	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	"musicgetter/internal/matcher"
)

// Work is one bounded item; source credentials never enter the pipeline.
type Work struct {
	Import      domain.Import
	Item        domain.ImportItem
	Track       domain.CanonicalTrack
	Connection  domain.DestinationConnection
	Target      domain.Target
	Cached      *domain.TrackMapping
	CachedTrack *domain.DestinationTrack
	Selected    *domain.DestinationTrack
}

// PipelineStore commits each transition under the current job lease and owner.
// No method holds a transaction across destination I/O.
type PipelineStore interface {
	Load(context.Context, domain.ImportJob) (Work, error)
	Matched(context.Context, domain.ImportJob, domain.DestinationTrack, string) (domain.DestinationTrack, error)
	Intent(context.Context, domain.ImportJob) (domain.DestinationMembership, error)
	Finish(context.Context, domain.ImportJob, domain.ImportItemState, string) error
	Reschedule(context.Context, domain.ImportJob, time.Time, string, bool) error
}

type Binding struct {
	Destination destination.Destination
	Catalog     matcher.Catalog
}
type Resolver interface {
	Resolve(context.Context, domain.DestinationConnection) (Binding, error)
}

var ErrUnsupportedDestination = errors.New("unsupported_destination")

// Registry contains trusted application factories, not user-supplied URLs.
type Factory func(context.Context, domain.DestinationConnection) (Binding, error)
type Registry map[string]Factory

func (r Registry) Resolve(ctx context.Context, c domain.DestinationConnection) (Binding, error) {
	f, ok := r[c.Adapter]
	if !ok {
		return Binding{}, ErrUnsupportedDestination
	}
	return f(ctx, c)
}

type Pipeline struct {
	Store    PipelineStore
	Resolver Resolver
	Matcher  matcher.TrackMatcher
}

// Process advances a durable item. Only explicit EffectApplied/AlreadyPresent
// confirms membership. A transport error after send is always uncertain.
func (p *Pipeline) Process(ctx context.Context, j domain.ImportJob) error {
	w, err := p.Store.Load(ctx, j)
	if err != nil {
		return err
	}
	b, err := p.Resolver.Resolve(ctx, w.Connection)
	if errors.Is(err, ErrUnsupportedDestination) {
		return p.Store.Finish(ctx, j, domain.ItemFailed, "unsupported_destination")
	}
	if err != nil {
		return err
	}
	if b.Destination == nil {
		return p.Store.Finish(ctx, j, domain.ItemFailed, "unsupported_destination")
	}
	caps, err := b.Destination.Capabilities(ctx)
	if err != nil {
		return err
	}
	// Atomic ensure must cover pre-existing membership, concurrent writers, and
	// repeated calls after arbitrary timeout/restart. Request dedup alone is weaker.
	if !caps.AtomicEnsureMembership || !slices.Contains(caps.TargetKinds, w.Target.Kind) {
		return p.Store.Finish(ctx, j, domain.ItemFailed, "unsafe_destination")
	}
	if w.Selected == nil {
		selected, decision, err := p.search(ctx, w, b, caps)
		if err != nil {
			return err
		}
		if selected == nil {
			state := domain.ItemNotFound
			if decision.Outcome == matcher.OutcomeAmbiguous {
				state = domain.ItemAmbiguous
			}
			return p.Store.Finish(ctx, j, state, "")
		}
		track, err := p.Store.Matched(ctx, j, *selected, decision.PolicyVersion)
		if err != nil {
			return err
		}
		w.Selected = &track
	}
	m, err := p.Store.Intent(ctx, j)
	if err != nil {
		return err
	}
	if m.State == domain.MembershipApplied {
		return p.Store.Finish(ctx, j, domain.ItemAlreadyPresent, "")
	}
	// Persisted intent is unknown before any outbound call, including the first. Read
	// evidence may prove presence, but absence is never proof a previous send failed.
	if reader, ok := b.Destination.(destination.MembershipReader); ok && caps.ReadMembership {
		present, err := reader.Contains(ctx, w.Target, w.Selected.ExternalKey)
		if err != nil {
			return err
		}
		if present {
			return p.Store.Finish(ctx, j, domain.ItemAlreadyPresent, "")
		}
	}
	if reconciler, ok := b.Destination.(destination.OperationReconciler); ok && caps.ReconcileOperations && m.State == domain.MembershipUnknown {
		effect, err := reconciler.Reconcile(ctx, string(m.OperationKey), "")
		if err != nil {
			return err
		}
		switch effect.State {
		case destination.EffectApplied, destination.EffectAlreadyPresent:
			return p.Store.Finish(ctx, j, domain.ItemAlreadyPresent, "")
		case destination.EffectPending:
			return errors.New("destination_pending")
		case destination.EffectRejected:
			return p.Store.Finish(ctx, j, domain.ItemFailed, "destination_rejected")
		}
	}
	// Recheck cancellation/fencing immediately before dispatch. Cancellation cannot
	// undo an already accepted remote request; Finish still records its evidence.
	if _, err = p.Store.Load(ctx, j); err != nil {
		return err
	}
	effect, err := b.Destination.EnsureTrack(ctx, destination.EnsureRequest{OperationKey: string(m.OperationKey), Target: w.Target, TrackID: w.Selected.ExternalKey})
	if err != nil {
		return err
	} // persisted unknown survives; safe retry only via atomic ensure
	switch effect.State {
	case destination.EffectApplied:
		return p.Store.Finish(ctx, j, domain.ItemAdded, "")
	case destination.EffectAlreadyPresent:
		return p.Store.Finish(ctx, j, domain.ItemAlreadyPresent, "")
	case destination.EffectRejected:
		return p.Store.Finish(ctx, j, domain.ItemFailed, "destination_rejected")
	default:
		return errors.New("destination_unknown")
	}
}

func (p *Pipeline) search(ctx context.Context, w Work, b Binding, caps destination.Capabilities) (*domain.DestinationTrack, matcher.Decision, error) {
	if w.Cached != nil && w.CachedTrack != nil {
		// Successful immutable mappings are persistent evidence, including metadata
		// fallback identities. Policy upgrades never silently overwrite a mapping.
		return w.CachedTrack, matcher.Decision{Outcome: matcher.OutcomeMatched, PolicyVersion: w.Cached.PolicyVersion}, nil
	}
	catalog := b.Catalog
	if catalog == nil {
		if searcher, ok := b.Destination.(destination.TrackSearcher); ok {
			catalog = searcher
		}
	}
	if !caps.Search || catalog == nil {
		return nil, matcher.Decision{}, ErrUnsupportedDestination
	}
	d, err := p.Matcher.Match(ctx, w.Track, w.Import.ConnectionID, catalog)
	if err != nil {
		return nil, d, err
	}
	switch d.Outcome {
	case matcher.OutcomeAmbiguous, matcher.OutcomeNotFound:
		return nil, d, nil
	case matcher.OutcomeMatched:
	default:
		return nil, d, domain.ErrInvalid
	}
	if d.Selected == nil {
		return nil, d, domain.ErrInvalid
	}
	// Adapter output cannot choose tenant, connection or local primary key.
	t := *d.Selected
	t.ID = ""
	t.OwnerID = w.Import.OwnerID
	t.ConnectionID = w.Import.ConnectionID
	return &t, d, nil
}
