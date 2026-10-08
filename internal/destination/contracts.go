// Package destination hides the protocol of the selected music Telegram bot.
package destination

import (
	"context"

	"musicgetter/internal/domain"
	"musicgetter/internal/matcher"
)

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

// TrackSearcher is the optional search side of a destination, enabled by Search.
// Keep it separate from the delivery interface: adapters may expose both ports.
type TrackSearcher interface {
	SearchTracks(context.Context, matcher.Query) (matcher.CandidatePage, error)
}
