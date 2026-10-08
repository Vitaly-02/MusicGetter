package matcher

import (
	"musicgetter/internal/domain"
	"slices"
)

func cloneMetadata(m domain.TrackMetadata) domain.TrackMetadata {
	m.Artists = slices.Clone(m.Artists)
	if m.DurationMS != nil {
		v := *m.DurationMS
		m.DurationMS = &v
	}
	return m
}
func clonePage(p CandidatePage) CandidatePage {
	out := CandidatePage{NextCursor: p.NextCursor, Candidates: make([]Candidate, len(p.Candidates))}
	for i, c := range p.Candidates {
		c.Track.Metadata = cloneMetadata(c.Track.Metadata)
		c.Reasons = slices.Clone(c.Reasons)
		out.Candidates[i] = c
	}
	return out
}
func sameMetadata(a, b domain.TrackMetadata) bool {
	return a.Title == b.Title && slices.Equal(a.Artists, b.Artists) && a.Album == b.Album && a.Version == b.Version && ((a.DurationMS == nil && b.DurationMS == nil) || (a.DurationMS != nil && b.DurationMS != nil && *a.DurationMS == *b.DurationMS))
}
