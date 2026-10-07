package matcher

import (
	"context"
	"musicgetter/internal/domain"
)

const ExactPolicyVersion = "exact_metadata_v1"

// Exact deliberately ignores untrusted adapter scores. No fuzzy auto-accept:
// identical normalized metadata is evidence, never acoustic identity.
type Exact struct{}

func (Exact) Match(ctx context.Context, track domain.ObservedTrack, candidates []Candidate) (Decision, error) {
	d := Decision{Outcome: OutcomeNotFound, PolicyVersion: ExactPolicyVersion}
	if err := ctx.Err(); err != nil {
		return d, err
	}
	wanted, err := domain.PrepareCanonicalTrack(domain.CanonicalTrackInput{Source: track.Ref.Source, Metadata: track.Metadata})
	if err != nil {
		return d, err
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c.Track.ExternalKey == "" {
			continue
		}
		normalized, err := domain.PrepareCanonicalTrack(domain.CanonicalTrackInput{Source: track.Ref.Source, Metadata: c.Track.Metadata})
		if err != nil {
			continue
		}
		if normalized.Fingerprint != wanted.Fingerprint || seen[c.Track.ExternalKey] {
			continue
		}
		seen[c.Track.ExternalKey] = true
		d.Candidates = append(d.Candidates, c)
	}
	switch len(d.Candidates) {
	case 0:
	case 1:
		d.Outcome = OutcomeMatched
		t := d.Candidates[0].Track
		d.Selected = &t
	default:
		d.Outcome = OutcomeAmbiguous
	}
	return d, nil
}
