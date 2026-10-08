package matcher

import (
	"context"
	"math"
	"slices"
	"strings"

	"musicgetter/internal/domain"
)

const (
	PolicyVersion      = "scored_metadata_v1"
	AutoMatchThreshold = 0.92
	AmbiguousThreshold = 0.70
	AutoMatchMargin    = 0.06
	MaxCandidates      = 200
)

type Scorer struct{}
type scoredCandidate struct {
	candidate Candidate
	auto      bool
}

func (Scorer) Match(ctx context.Context, track domain.ObservedTrack, candidates []Candidate) (Decision, error) {
	d := Decision{Outcome: OutcomeNotFound, PolicyVersion: PolicyVersion}
	if err := ctx.Err(); err != nil {
		return d, err
	}
	if err := domain.ValidateMetadata(track.Metadata); err != nil {
		return d, err
	}
	if len(candidates) > MaxCandidates {
		return d, domain.ErrInvalid
	}
	wanted := normalizeTrack(track.Metadata)
	ranked := make([]scoredCandidate, 0, len(candidates))
	seen := map[string]domain.TrackMetadata{}
	conflicting := false
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		if c.Track.ExternalKey == "" || domain.ValidateMetadata(c.Track.Metadata) != nil {
			continue
		}
		if old, ok := seen[c.Track.ExternalKey]; ok {
			conflicting = conflicting || !sameMetadata(old, c.Track.Metadata)
			continue
		}
		seen[c.Track.ExternalKey] = c.Track.Metadata
		ranked = append(ranked, score(wanted, c.Track))
	}
	slices.SortFunc(ranked, func(a, b scoredCandidate) int {
		if a.candidate.Score > b.candidate.Score {
			return -1
		}
		if a.candidate.Score < b.candidate.Score {
			return 1
		}
		return strings.Compare(a.candidate.Track.ExternalKey, b.candidate.Track.ExternalKey)
	})
	for _, c := range ranked {
		d.Candidates = append(d.Candidates, c.candidate)
	}
	if conflicting {
		d.Outcome = OutcomeAmbiguous
		return d, nil
	}
	if len(ranked) == 0 || ranked[0].candidate.Score < AmbiguousThreshold {
		return d, nil
	}
	d.Outcome = OutcomeAmbiguous
	best := ranked[0]
	if best.auto && best.candidate.Score >= AutoMatchThreshold && (len(ranked) == 1 || best.candidate.Score-ranked[1].candidate.Score >= AutoMatchMargin) {
		d.Outcome = OutcomeMatched
		t := best.candidate.Track
		d.Selected = &t
	}
	return d, nil
}

func score(a normalizedTrack, track domain.DestinationTrack) scoredCandidate {
	b := normalizeTrack(track.Metadata)
	title := textSimilarity(a.title, b.title)
	artists := artistSimilarity(a.artists, b.artists)
	tokens := tokenSimilarity(a.title, b.title)
	sameVersion := slices.Equal(a.versions, b.versions) && slices.Equal(a.descriptors, b.descriptors)
	versions := 0.0
	if sameVersion {
		versions = 1
	}
	total := .40*title + .30*artists + .08*tokens + .05*versions
	weight := .83
	reasons := []string{}
	exact := a.title == b.title && a.title != "" && slices.Equal(a.artists, b.artists)
	if exact {
		reasons = append(reasons, "exact_title_artists")
	}
	if a.album != "" && b.album != "" {
		total += .05 * nameSimilarity(a.album, b.album)
		weight += .05
	}
	gap := int64(-1)
	if a.duration != nil && b.duration != nil {
		gap = *a.duration - *b.duration
		if gap < 0 {
			gap = -gap
		}
		duration := 0.0
		switch {
		case gap <= 2000:
			duration = 1
			reasons = append(reasons, "duration_strong")
		case gap <= 5000:
			duration = .7
			reasons = append(reasons, "duration_close")
		case gap <= 10000:
			duration = .25
			reasons = append(reasons, "duration_mismatch")
		default:
			reasons = append(reasons, "duration_conflict")
		}
		total += .12 * duration
		weight += .12
	} else {
		reasons = append(reasons, "duration_missing")
	}
	value := total / weight
	if exact && sameVersion {
		value = math.Min(1, value+.02)
	}
	auto := title >= .90 && artists >= .90 && sameVersion && (exact || gap >= 0 && gap <= 5000)
	if title < .55 || artists < .55 {
		value = math.Min(value, .50)
		auto = false
		reasons = append(reasons, "identity_conflict")
	}
	if !sameVersion {
		// Remaster can preserve a performance but is still a different requested edition.
		cap := .69
		if onlyRemasterDifference(a, b) {
			cap = .85
		}
		value = math.Min(value, cap)
		auto = false
		reasons = append(reasons, "version_conflict")
	}
	if gap > 5000 {
		auto = false
		value = math.Min(value, .89)
	}
	if gap > 10000 {
		value = math.Min(value, .69)
	}
	return scoredCandidate{candidate: Candidate{Track: track, Score: value, Reasons: reasons}, auto: auto}
}
func onlyRemasterDifference(a, b normalizedTrack) bool {
	for _, set := range [][]string{a.versions, b.versions} {
		for _, v := range set {
			if v != "remaster" {
				return false
			}
		}
	}
	return len(a.versions)+len(b.versions) > 0
}
