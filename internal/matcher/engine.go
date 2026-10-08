package matcher

import (
	"context"
	"errors"
	"strings"
	"time"

	"musicgetter/internal/domain"
)

type Options struct {
	RemoteConcurrency        int
	RemoteTimeout            time.Duration
	CacheTTL, NegativeTTL    time.Duration
	CacheEntries, CacheBytes int
}

func DefaultOptions() Options {
	return Options{RemoteConcurrency: 4, RemoteTimeout: 10 * time.Second, CacheTTL: 5 * time.Minute, NegativeTTL: 30 * time.Second, CacheEntries: 512, CacheBytes: 16 << 20}
}
func (o Options) Validate() error {
	if o.RemoteConcurrency < 1 || o.RemoteConcurrency > 64 || o.RemoteTimeout <= 0 || o.RemoteTimeout > time.Minute || o.CacheTTL <= 0 || o.CacheTTL > time.Hour || o.NegativeTTL <= 0 || o.NegativeTTL > o.CacheTTL || o.CacheEntries < 1 || o.CacheEntries > 10000 || o.CacheBytes < 1<<20 || o.CacheBytes > 256<<20 {
		return domain.ErrInvalid
	}
	return nil
}

type Engine struct{ search *searchCache }

func NewEngine(o Options) (*Engine, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return &Engine{search: newSearchCache(o)}, nil
}

// Match preserves candidate evidence across strategies. title-only is allowed
// only when ALL preceding strategies returned no valid candidates, not merely
// because their scores were low. A strong unambiguous result stops immediately.
func (e *Engine) Match(ctx context.Context, track domain.CanonicalTrack, connection domain.ID, catalog Catalog) (Decision, error) {
	empty := Decision{Outcome: OutcomeNotFound, PolicyVersion: PolicyVersion}
	if track.OwnerID == "" || connection == "" || catalog == nil {
		return empty, domain.ErrInvalid
	}
	if err := domain.ValidateMetadata(track.Metadata); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	queries := Strategies(track.Metadata)
	candidates := make([]Candidate, 0, MaxCandidates)
	seen := map[string]domain.DestinationTrack{}
	incomplete := false
	decision := empty
	for _, q := range queries {
		if q.Strategy == StrategyTitleOnly && (len(candidates) > 0 || incomplete) {
			break
		}
		page, err := e.search.get(ctx, track.OwnerID, connection, catalog, q)
		if err != nil {
			return empty, err
		} // failures are not empty search results
		incomplete = incomplete || page.NextCursor != ""
		for _, c := range page.Candidates {
			if old, ok := seen[c.Track.ExternalKey]; ok {
				if !sameMetadata(old.Metadata, c.Track.Metadata) {
					incomplete = true
				}
				continue
			}
			// Never trust tenant or local IDs supplied by a remote search adapter.
			c.Track.ID = ""
			c.Track.OwnerID = track.OwnerID
			c.Track.ConnectionID = connection
			seen[c.Track.ExternalKey] = c.Track
			candidates = append(candidates, c)
		}
		decision, err = (Scorer{}).Match(ctx, domain.ObservedTrack{Metadata: track.Metadata}, candidates)
		if err != nil {
			return empty, err
		}
		if decision.Outcome == OutcomeMatched && !incomplete {
			return decision, nil
		}
	}
	if incomplete {
		decision.Outcome = OutcomeAmbiguous
		decision.Selected = nil
	}
	return decision, nil
}

func Strategies(m domain.TrackMetadata) []Query {
	n := normalizeTrack(m)
	if n.title == "" || len(n.artists) == 0 {
		return nil
	}
	// Keep edition descriptors in every query; featured artists are included in
	// artist_title, so they need not also be repeated in the query title.
	title := strings.TrimSpace(n.title + " " + strings.Join(n.descriptors, " "))
	choices := []struct {
		s    Strategy
		text string
	}{
		{StrategyArtistTitle, strings.Join(n.artists, " ") + " " + title},
		{StrategyPrimaryTitle, n.primary + " " + title},
	}
	if n.album != "" {
		choices = append(choices, struct {
			s    Strategy
			text string
		}{StrategyTitleAlbum, title + " " + n.album})
	}
	choices = append(choices, struct {
		s    Strategy
		text string
	}{StrategyTitleOnly, title})
	seen := map[string]bool{}
	out := make([]Query, 0, 4)
	for _, c := range choices {
		text := strings.TrimSpace(c.text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, Query{Text: text, Strategy: c.s, Track: m, Limit: 50})
	}
	return out
}

// Adapter failures deliberately exclude arbitrary remote details/credentials.
var ErrSearchUnavailable = errors.New("destination_search_unavailable")
var ErrInvalidSearchPage = errors.New("invalid_destination_search_page")
