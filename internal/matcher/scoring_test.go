package matcher

import (
	"context"
	"slices"
	"testing"

	"musicgetter/internal/domain"
)

func ms(v int64) *int64 { return &v }
func song(title string, artists ...string) domain.TrackMetadata {
	return domain.TrackMetadata{Title: title, Artists: artists, Album: "Album", DurationMS: ms(180000)}
}
func candidate(key string, m domain.TrackMetadata) Candidate {
	return Candidate{Track: domain.DestinationTrack{ExternalKey: key, Metadata: m}, Score: 100, Reasons: []string{"untrusted"}}
}
func scoreDecision(t *testing.T, a domain.TrackMetadata, cs ...Candidate) Decision {
	t.Helper()
	d, err := (Scorer{}).Match(context.Background(), domain.ObservedTrack{Metadata: a}, cs)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestScoringCases(t *testing.T) {
	base := song("Song", "Artist")
	changed := func(m domain.TrackMetadata, f func(*domain.TrackMetadata)) domain.TrackMetadata { f(&m); return m }
	noDuration := func(m *domain.TrackMetadata) { m.DurationMS = nil }
	noAlbum := func(m *domain.TrackMetadata) { m.Album = "" }
	cases := []struct {
		name string
		a, b domain.TrackMetadata
		want Outcome
	}{
		{"ordinary", base, base, OutcomeMatched},
		{"case whitespace", base, song("  SONG  ", " ARTIST "), OutcomeMatched},
		{"unicode NFKC", song("Ｆｌｏｗｅｒ", "Artist"), song("Flower", "Artist"), OutcomeMatched},
		{"unicode composed", song("Cafe\u0301", "Björk"), song("Café", "Björk"), OutcomeMatched},
		{"punctuation", song("Hello, World!", "Artist"), song("Hello—World", "Artist"), OutcomeMatched},
		{"apostrophes", song("Don't Stop", "Artist"), song("Dont Stop", "Artist"), OutcomeMatched},
		{"russian ё е", song("Ёлки, зелёные!", "Пётр"), song("елки зеленые", "Петр"), OutcomeMatched},
		{"russian typo", song("Последняя любовь", "Исполнитель"), song("Последняя любвь", "Исполнитель"), OutcomeMatched},
		{"feat title", song("Song (feat. Guest)", "Artist"), song("Song", "Artist", "Guest"), OutcomeMatched},
		{"ft title", song("Song ft. Guest", "Artist"), song("Song featuring Guest", "Artist"), OutcomeMatched},
		{"feat artist", song("Song", "Artist featuring Guest"), song("Song", "Artist", "Guest"), OutcomeMatched},
		{"feat lost", song("Song (feat. Guest)", "Artist"), base, OutcomeAmbiguous},
		{"artist order", song("Song", "Artist", "Guest"), song("Song", "Guest", "Artist"), OutcomeMatched},
		{"band ampersand", song("Song", "Earth, Wind & Fire"), song("Song", "Earth Wind and Fire"), OutcomeAmbiguous},
		{"no invented aliases", song("Song", "P!nk"), song("Song", "Pink"), OutcomeAmbiguous},
		{"no transliteration", song("Song", "Кино"), song("Song", "Kino"), OutcomeNotFound},
		{"same title wrong artist", base, song("Song", "Other Band"), OutcomeNotFound},
		{"live same", song("Song (Live)", "Artist"), song("Song - LIVE", "Artist"), OutcomeMatched},
		{"live versus studio", song("Song (Live)", "Artist"), base, OutcomeNotFound},
		{"studio versus live", base, song("Song - Live", "Artist"), OutcomeNotFound},
		{"live venue differs", song("Song (Live at Wembley)", "Artist"), song("Song (Live at Paris)", "Artist"), OutcomeNotFound},
		{"live ordinary title", song("Live Forever", "Artist"), song("Live Forever", "Artist"), OutcomeMatched},
		{"live not stripped from ordinary title", song("Live Forever", "Artist"), song("Forever", "Artist"), OutcomeAmbiguous},
		{"remix same", song("Song (Club Remix)", "Artist"), song("Song - Club Remix", "Artist"), OutcomeMatched},
		{"remix artist differs", song("Song (Alice Remix)", "Artist"), song("Song (Bob Remix)", "Artist"), OutcomeNotFound},
		{"remix studio", song("Song Remix", "Artist"), base, OutcomeNotFound},
		{"remaster spelling year", song("Song - 2011 Remaster", "Artist"), song("Song (Remastered 2011)", "Artist"), OutcomeMatched},
		{"remaster versus original", song("Song (Remastered)", "Artist"), base, OutcomeAmbiguous},
		{"remaster year differs", song("Song (2011 Remaster)", "Artist"), song("Song (2018 Remaster)", "Artist"), OutcomeAmbiguous},
		{"radio edit same", song("Song (Radio Edit)", "Artist"), song("Song Radio Edit", "Artist"), OutcomeMatched},
		{"radio edit original", song("Song (Radio Edit)", "Artist"), base, OutcomeNotFound},
		{"sped up", song("Song (Sped Up)", "Artist"), base, OutcomeNotFound},
		{"slowed equivalent", song("Song (Slowed Down)", "Artist"), song("Song (Slowed)", "Artist"), OutcomeMatched},
		{"slowed versus sped", song("Song (Slowed)", "Artist"), song("Song (Sped Up)", "Artist"), OutcomeNotFound},
		{"instrumental", song("Song (Instrumental)", "Artist"), base, OutcomeNotFound},
		{"acoustic", song("Song (Acoustic)", "Artist"), base, OutcomeNotFound},
		{"edition field", changed(base, func(m *domain.TrackMetadata) { m.Version = "Live" }), song("Song (Live)", "Artist"), OutcomeMatched},
		{"unknown edition retained", changed(base, func(m *domain.TrackMetadata) { m.Version = "Demo" }), base, OutcomeNotFound},
		{"duration 2 seconds", base, changed(base, func(m *domain.TrackMetadata) { m.DurationMS = ms(182000) }), OutcomeMatched},
		{"duration 5 seconds", base, changed(base, func(m *domain.TrackMetadata) { m.DurationMS = ms(185000) }), OutcomeMatched},
		{"duration 6 seconds", base, changed(base, func(m *domain.TrackMetadata) { m.DurationMS = ms(186000) }), OutcomeAmbiguous},
		{"duration 20 seconds", base, changed(base, func(m *domain.TrackMetadata) { m.DurationMS = ms(200000) }), OutcomeNotFound},
		{"missing duration", changed(base, noDuration), base, OutcomeMatched},
		{"both missing duration", changed(base, noDuration), changed(base, noDuration), OutcomeMatched},
		{"missing album", changed(base, noAlbum), base, OutcomeMatched},
		{"both missing album", changed(base, noAlbum), changed(base, noAlbum), OutcomeMatched},
		{"transposed typo", song("Beautiful Morning", "Artist"), song("Beautiful Mornign", "Artist"), OutcomeMatched},
		{"typo title with duration", song("Beautiful Morning", "Artist"), song("Beautiful Mornng", "Artist"), OutcomeMatched},
		{"typo without duration cautious", changed(song("Beautiful Morning", "Artist"), noDuration), changed(song("Beautiful Mornng", "Artist"), noDuration), OutcomeAmbiguous},
		{"short title typo cautious", song("One", "Artist"), song("Once", "Artist"), OutcomeAmbiguous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := scoreDecision(t, tc.a, candidate("1", tc.b))
			if d.Outcome != tc.want {
				t.Fatalf("got %s want %s candidates=%+v normalized=%+v / %+v", d.Outcome, tc.want, d.Candidates, normalizeTrack(tc.a), normalizeTrack(tc.b))
			}
			if len(d.Candidates) > 0 && slices.Contains(d.Candidates[0].Reasons, "untrusted") {
				t.Fatal("trusted remote scoring")
			}
		})
	}
}

func TestScoringAmbiguityAndDurationRanking(t *testing.T) {
	m := song("Song", "Artist")
	d := scoreDecision(t, m, candidate("b", m), candidate("a", m))
	if d.Outcome != OutcomeAmbiguous || d.Selected != nil || d.Candidates[0].Track.ExternalKey != "a" {
		t.Fatal(d)
	}
	distant := m
	distant.DurationMS = ms(190000)
	d = scoreDecision(t, m, candidate("distant", distant), candidate("best", m))
	if d.Outcome != OutcomeMatched || d.Selected.ExternalKey != "best" {
		t.Fatal(d)
	}
	earlier := 2.0
	for _, gap := range []int64{0, 2001, 5001, 10001} {
		m2 := m
		m2.DurationMS = ms(180000 + gap)
		score := scoreDecision(t, m, candidate("a", m2)).Candidates[0].Score
		if score >= earlier {
			t.Fatal("duration signal not monotonic", gap, score, earlier)
		}
		earlier = score
	}
}

func TestNormalizationDoesNotModifyCanonicalIdentity(t *testing.T) {
	a := song("Ёлка!", "Artist")
	b := song("Елка", "Artist")
	x, err := domain.PrepareCanonicalTrack(domain.CanonicalTrackInput{Source: domain.SourceYandex, Metadata: a})
	if err != nil {
		t.Fatal(err)
	}
	y, err := domain.PrepareCanonicalTrack(domain.CanonicalTrackInput{Source: domain.SourceYandex, Metadata: b})
	if err != nil {
		t.Fatal(err)
	}
	if x.Fingerprint == y.Fingerprint {
		t.Fatal("changed canonical identity")
	}
	if Normalize(a.Title) != Normalize(b.Title) {
		t.Fatal("matching did not normalize ё/punctuation")
	}
}

func TestAdditionalVersionAndSparseEvidenceCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b domain.TrackMetadata
		want Outcome
	}{
		{"acoustic label", song("Song (Acoustic Version)", "Artist"), song("Song (Acoustic)", "Artist"), OutcomeMatched},
		{"sped label punctuation", song("Song (Sped-Up)", "Artist"), song("Song (Sped Up)", "Artist"), OutcomeMatched},
		{"feat artist named Remix", song("Song feat. Remix", "Artist"), song("Song", "Artist", "Remix"), OutcomeMatched},
		{"feat after remix separator", song("Song - Club Remix feat. Guest", "Artist"), song("Song (Club Remix)", "Artist", "Guest"), OutcomeMatched},
		{"remix suffix before feature", song("Song Remix feat. Guest", "Artist"), song("Song (Remix)", "Artist", "Guest"), OutcomeMatched},
		{"feat artist named Live", song("Song (feat. Live)", "Artist"), song("Song", "Artist", "Live"), OutcomeMatched},
		{"feat inside remix annotation", song("Song (Club Remix feat. Guest)", "Artist"), song("Song (Club Remix)", "Artist", "Guest"), OutcomeMatched},
		{"feat and remix", song("Song (feat. Guest) - Club Remix", "Artist"), song("Song (Club Remix)", "Artist", "Guest"), OutcomeMatched},
		{"accent not stripped", song("Song", "José"), song("Song", "Jose"), OutcomeAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := scoreDecision(t, tc.a, candidate("1", tc.b))
			if d.Outcome != tc.want {
				t.Fatal(d)
			}
		})
	}
	m := song("Song", "Artist")
	d := scoreDecision(t, m, candidate("a", m), candidate("a", song("Song (Live)", "Artist")))
	if d.Outcome != OutcomeAmbiguous {
		t.Fatal("conflicting duplicate accepted")
	}
	// The scorer never accepts a title alone without artist evidence.
	d = scoreDecision(t, m, candidate("a", song("Song", "Someone Completely Else")))
	if d.Outcome == OutcomeMatched {
		t.Fatal("wrong artist accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Scorer{}).Match(ctx, domain.ObservedTrack{Metadata: m}, nil); err == nil {
		t.Fatal("context ignored")
	}
}

func BenchmarkScorer50Candidates(b *testing.B) {
	m := song("Beautiful Morning", "Artist")
	candidates := make([]Candidate, 50)
	for i := range candidates {
		other := song("Other Song "+string(rune('a'+i)), "Other Artist")
		candidates[i] = candidate(string(rune('a'+i)), other)
	}
	candidates[49] = candidate("exact", m)
	for b.Loop() {
		_, err := (Scorer{}).Match(context.Background(), domain.ObservedTrack{Metadata: m}, candidates)
		if err != nil {
			b.Fatal(err)
		}
	}
}
