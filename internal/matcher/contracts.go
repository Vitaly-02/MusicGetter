// Package matcher defines destination-independent matching policy.
package matcher

import (
	"context"

	"musicgetter/internal/domain"
)

type Query struct {
	Track  domain.TrackMetadata
	Limit  int
	Cursor string
}

type Candidate struct {
	Track   domain.DestinationTrack
	Score   float64
	Reasons []string
}

type CandidatePage struct {
	Candidates []Candidate
	NextCursor string
}

// Catalog is provided by a supported destination integration, never a source API.
type Catalog interface {
	Search(context.Context, Query) (CandidatePage, error)
}

type Outcome string

const (
	OutcomeMatched   Outcome = "matched"
	OutcomeAmbiguous Outcome = "ambiguous"
	OutcomeNotFound  Outcome = "not_found"
)

type Decision struct {
	Outcome       Outcome
	Selected      *domain.DestinationTrack // set only for matched
	Candidates    []Candidate
	PolicyVersion string
}

// Matcher applies a versioned policy to a bounded set of candidates.
type Matcher interface {
	Match(context.Context, domain.ObservedTrack, []Candidate) (Decision, error)
}
