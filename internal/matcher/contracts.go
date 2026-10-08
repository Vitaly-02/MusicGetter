// Package matcher defines destination-independent matching policy.
package matcher

import (
	"context"

	"musicgetter/internal/domain"
)

type Strategy string

const (
	StrategyArtistTitle  Strategy = "artist_title"
	StrategyPrimaryTitle Strategy = "primary_artist_title"
	StrategyTitleAlbum   Strategy = "title_album"
	StrategyTitleOnly    Strategy = "title_only"
)

type Query struct {
	Text     string
	Strategy Strategy
	Track    domain.TrackMetadata
	Limit    int
	Cursor   string
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
	SearchTracks(context.Context, Query) (CandidatePage, error)
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

// TrackMatcher orchestrates bounded destination searches for a canonical observation.
// Successful persistent mappings are checked by the fenced pipeline before this port.
type TrackMatcher interface {
	Match(context.Context, domain.CanonicalTrack, domain.ID, Catalog) (Decision, error)
}
