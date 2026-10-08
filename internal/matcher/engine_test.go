package matcher

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"musicgetter/internal/domain"
)

type searchFunc func(context.Context, Query) (CandidatePage, error)

func (f searchFunc) SearchTracks(ctx context.Context, q Query) (CandidatePage, error) {
	return f(ctx, q)
}
func canonical(m domain.TrackMetadata) domain.CanonicalTrack {
	return domain.CanonicalTrack{ID: "track", CanonicalTrackInput: domain.CanonicalTrackInput{OwnerID: "owner", Source: domain.SourceSpotify, Metadata: m}}
}
func newEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEngineStrategiesAndEarlyExit(t *testing.T) {
	track := canonical(song("Song", "Artist", "Guest"))
	for _, tc := range []struct {
		name    string
		hit     Strategy
		other   bool
		want    []Strategy
		outcome Outcome
	}{
		{"strong first", StrategyArtistTitle, false, []Strategy{StrategyArtistTitle}, OutcomeMatched},
		{"primary fallback", StrategyPrimaryTitle, false, []Strategy{StrategyArtistTitle, StrategyPrimaryTitle}, OutcomeMatched},
		{"album fallback", StrategyTitleAlbum, false, []Strategy{StrategyArtistTitle, StrategyPrimaryTitle, StrategyTitleAlbum}, OutcomeMatched},
		{"title only last", StrategyTitleOnly, false, []Strategy{StrategyArtistTitle, StrategyPrimaryTitle, StrategyTitleAlbum, StrategyTitleOnly}, OutcomeMatched},
		{"no title-only with irrelevant candidates", "", true, []Strategy{StrategyArtistTitle, StrategyPrimaryTitle, StrategyTitleAlbum}, OutcomeNotFound},
		{"not found", "", false, []Strategy{StrategyArtistTitle, StrategyPrimaryTitle, StrategyTitleAlbum, StrategyTitleOnly}, OutcomeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []Strategy
			remote := searchFunc(func(_ context.Context, q Query) (CandidatePage, error) {
				requests = append(requests, q.Strategy)
				if q.Text == "" || q.Limit != 50 {
					t.Fatal("unbounded query", q)
				}
				if q.Strategy == tc.hit {
					return CandidatePage{Candidates: []Candidate{candidate("hit", track.Metadata)}}, nil
				}
				if tc.other {
					return CandidatePage{Candidates: []Candidate{candidate("wrong", song("Completely Unrelated", "Unknown"))}}, nil
				}
				return CandidatePage{}, nil
			})
			d, err := newEngine(t).Match(context.Background(), track, "connection", remote)
			if err != nil {
				t.Fatal(err)
			}
			if d.Outcome != tc.outcome || !reflect.DeepEqual(requests, tc.want) {
				t.Fatalf("got %s %v want %s %v", d.Outcome, requests, tc.outcome, tc.want)
			}
		})
	}
}

func TestEngineEvidenceAndFailureBoundaries(t *testing.T) {
	track := canonical(song("Song", "Artist", "Guest"))
	for _, tc := range []struct {
		name string
		page func(Query) CandidatePage
		err  error
		want Outcome
	}{
		{"truncated", func(q Query) CandidatePage {
			return CandidatePage{Candidates: []Candidate{candidate("a", track.Metadata)}, NextCursor: "more"}
		}, nil, OutcomeAmbiguous},
		{"truncated empty", func(q Query) CandidatePage { return CandidatePage{NextCursor: "more"} }, nil, OutcomeAmbiguous},
		{"duplicate IDs", func(q Query) CandidatePage {
			return CandidatePage{Candidates: []Candidate{candidate("a", track.Metadata), candidate("a", track.Metadata)}}
		}, nil, OutcomeMatched},
		{"distinct recordings", func(q Query) CandidatePage {
			return CandidatePage{Candidates: []Candidate{candidate("a", track.Metadata), candidate("b", track.Metadata)}}
		}, nil, OutcomeAmbiguous},
		{"conflicting same ID", func(q Query) CandidatePage {
			return CandidatePage{Candidates: []Candidate{candidate("a", track.Metadata), candidate("a", song("Song (Live)", "Artist", "Guest"))}}
		}, nil, OutcomeAmbiguous},
		{"failure", func(q Query) CandidatePage { return CandidatePage{} }, errors.New("SECRET"), ""},
		{"invalid candidate", func(q Query) CandidatePage {
			return CandidatePage{Candidates: []Candidate{candidate("a", domain.TrackMetadata{})}}
		}, nil, ""},
		{"oversized page", func(q Query) CandidatePage { return CandidatePage{Candidates: make([]Candidate, 51)} }, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			remote := searchFunc(func(_ context.Context, q Query) (CandidatePage, error) { calls++; return tc.page(q), tc.err })
			d, err := newEngine(t).Match(context.Background(), track, "connection", remote)
			if tc.want == "" {
				if err == nil || err.Error() == "SECRET" || calls != 1 {
					t.Fatal("failure hidden as search miss or leaked", err, calls)
				}
				return
			}
			if err != nil || d.Outcome != tc.want {
				t.Fatal(d, err)
			}
		})
	}
}

func TestQueriesDeduplicateAndRetainVersion(t *testing.T) {
	q := Strategies(song("Song (2011 Remaster)", "Artist"))
	if len(q) != 3 || q[0].Text != "artist song 2011 remaster" || q[1].Strategy != StrategyTitleAlbum || q[2].Strategy != StrategyTitleOnly {
		t.Fatal(q)
	}
	m := song("Song (feat. Guest)", "Artist")
	m.Album = ""
	q = Strategies(m)
	if len(q) != 3 || q[0].Text != "artist guest song" || q[1].Text != "artist song" || q[2].Text != "song" {
		t.Fatal(q)
	}
}

func TestEngineRebindsAccountAndDoesNotMutateInput(t *testing.T) {
	track := canonical(song("Song", "Artist"))
	remote := searchFunc(func(_ context.Context, q Query) (CandidatePage, error) {
		q.Track.Artists[0] = "MUTATED"
		c := candidate("remote", track.Metadata)
		c.Track.OwnerID = "other"
		c.Track.ConnectionID = "other"
		c.Track.ID = "foreign"
		return CandidatePage{Candidates: []Candidate{c}}, nil
	})
	d, err := newEngine(t).Match(context.Background(), track, "connection", remote)
	if err != nil {
		t.Fatal(err)
	}
	if track.Metadata.Artists[0] != "Artist" || d.Selected == nil || d.Selected.OwnerID != "owner" || d.Selected.ConnectionID != "connection" || d.Selected.ID != "" {
		t.Fatal("boundary violated", d)
	}
}

func TestEngineKeepsEarlierCandidatesAndVersionEvidence(t *testing.T) {
	track := canonical(song("Song (Remaster 2011)", "Artist", "Guest"))
	calls := 0
	remote := searchFunc(func(_ context.Context, q Query) (CandidatePage, error) {
		calls++
		if q.Strategy == StrategyArtistTitle {
			return CandidatePage{Candidates: []Candidate{candidate("studio", song("Song", "Artist", "Guest"))}}, nil
		}
		return CandidatePage{Candidates: []Candidate{candidate("correct", track.Metadata)}}, nil
	})
	d, err := newEngine(t).Match(context.Background(), track, "connection", remote)
	if err != nil || d.Outcome != OutcomeMatched || d.Selected.ExternalKey != "correct" || calls != 2 || len(d.Candidates) != 2 {
		t.Fatal(d, err, calls)
	}
	// Degenerate matching metadata must not create an artist-only search.
	track.Metadata.Title = "!!!"
	calls = 0
	d, err = newEngine(t).Match(context.Background(), track, "connection", remote)
	if err != nil || calls != 0 || d.Outcome != OutcomeNotFound {
		t.Fatal(d, err, calls)
	}
}
