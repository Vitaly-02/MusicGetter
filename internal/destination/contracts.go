// Package destination hides the protocol of the selected music Telegram bot.
package destination

import (
	"context"

	"musicgetter/internal/domain"
)

type Capabilities struct {
	TargetKinds       []domain.DestinationCollectionKind
	Search            bool
	NativeIdempotency bool
	// AtomicEnsureMembership promises set semantics for (account,target,track),
	// including preexisting tracks, concurrent/external writers and retries after
	// arbitrary timeouts. A read-before-add adapter cannot advertise this.
	AtomicEnsureMembership bool
	ReadMembership         bool
	ReconcileOperations    bool
	CreateTargets          bool
}

type EnsureRequest struct {
	OperationKey string // persisted before dispatch; stable across all retries
	Target       domain.Target
	TrackID      string
}

type EffectState string

const (
	EffectApplied        EffectState = "applied"
	EffectAlreadyPresent EffectState = "already_present"
	EffectPending        EffectState = "pending"
	EffectUnknown        EffectState = "unknown"
	EffectRejected       EffectState = "rejected"
)

type Effect struct {
	State   EffectState
	Receipt string
	Code    string // sanitized, stable machine-readable reason
}

// Destination is bound to one authenticated destination connection.
// Context timeout may mean unknown effect; it must never imply safe retry.
type Destination interface {
	Capabilities(context.Context) (Capabilities, error)
	EnsureTrack(context.Context, EnsureRequest) (Effect, error)
}

// Optional interfaces are enabled only by capabilities.
type MembershipReader interface {
	Contains(context.Context, domain.Target, string) (bool, error)
}

type OperationReconciler interface {
	Reconcile(ctx context.Context, operationKey string, receipt string) (Effect, error)
}

type CreateTargetRequest struct {
	OperationKey string
	Kind         domain.DestinationCollectionKind
	Title        string
}

type TargetManager interface {
	EnsureTarget(context.Context, CreateTargetRequest) (domain.Target, Effect, error)
}
