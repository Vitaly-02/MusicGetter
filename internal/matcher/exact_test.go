package matcher

import (
	"context"
	"musicgetter/internal/domain"
	"testing"
)

func TestExactConservativeDecisions(t *testing.T) {
	base := domain.TrackMetadata{Title: " Song ", Artists: []string{"ARTIST"}, Album: "Album"}
	track := domain.ObservedTrack{Ref: domain.SourceRef{Source: domain.SourceSpotify}, Metadata: base}
	candidate := func(key string, m domain.TrackMetadata) Candidate {
		return Candidate{Track: domain.DestinationTrack{ExternalKey: key, Metadata: m}, Score: 1}
	}
	changed := base
	changed.Title = "Song (Live)"
	for _, tc := range []struct {
		name string
		c    []Candidate
		want Outcome
	}{
		{"empty", nil, OutcomeNotFound}, {"single", []Candidate{candidate("1", base)}, OutcomeMatched},
		{"duplicate search result", []Candidate{candidate("1", base), candidate("1", base)}, OutcomeMatched},
		{"two recordings", []Candidate{candidate("1", base), candidate("2", base)}, OutcomeAmbiguous},
		{"version", []Candidate{candidate("1", changed)}, OutcomeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := (Exact{}).Match(context.Background(), track, tc.c)
			if err != nil || d.Outcome != tc.want {
				t.Fatalf("%+v %v", d, err)
			}
		})
	}
}
