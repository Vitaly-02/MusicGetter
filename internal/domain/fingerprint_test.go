package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func inputTrack() CanonicalTrackInput {
	return CanonicalTrackInput{Source: SourceSpotify, Metadata: TrackMetadata{Title: " Song ", Artists: []string{"Beta", "Alpha"}, Album: "ALBUM", DurationMS: ptr(int64(123000))}}
}
func prepared(t *testing.T, input CanonicalTrackInput) CanonicalTrack {
	t.Helper()
	track, err := PrepareCanonicalTrack(input)
	if err != nil {
		t.Fatal(err)
	}
	return track
}

func TestFingerprintGolden(t *testing.T) {
	track := prepared(t, inputTrack())
	const golden = "v1:91f246cbe4b1c5b74f7024a38f681caa02e9b528cc293640515d5a317112d142"
	if track.Fingerprint != golden {
		t.Fatalf("fingerprint format changed: %s", track.Fingerprint)
	}
	if track.IdentityKey != "fp:"+golden || track.NormalizedTitle != "song" || !slices.Equal(track.NormalizedArtists, []string{"alpha", "beta"}) {
		t.Fatal("invalid normalized identity")
	}
}

func TestNormalizationEquivalentObservations(t *testing.T) {
	a := inputTrack()
	a.Metadata.Title = "  CAFÉ\u00a0 Live  "
	a.Metadata.Artists = []string{"Straße", "ＡＬＰＨＡ"}
	b := inputTrack()
	b.Metadata.Title = "cafe\u0301 live"
	b.Metadata.Artists = []string{"alpha", "STRASSE", "alpha"}
	if prepared(t, a).Fingerprint != prepared(t, b).Fingerprint {
		t.Fatal("Unicode, spacing, case or artist order changed fingerprint")
	}
}

func TestFingerprintKeepsRecordingDistinctions(t *testing.T) {
	base := prepared(t, inputTrack()).Fingerprint
	cases := map[string]func(*CanonicalTrackInput){
		"live":             func(i *CanonicalTrackInput) { i.Metadata.Title = "Song (Live)" },
		"punctuation":      func(i *CanonicalTrackInput) { i.Metadata.Title = "Song!" },
		"album":            func(i *CanonicalTrackInput) { i.Metadata.Album = "Another album" },
		"artist":           func(i *CanonicalTrackInput) { i.Metadata.Artists = []string{"Another artist"} },
		"duration":         func(i *CanonicalTrackInput) { i.Metadata.DurationMS = ptr(int64(123001)) },
		"unknown duration": func(i *CanonicalTrackInput) { i.Metadata.DurationMS = nil },
		"edition":          func(i *CanonicalTrackInput) { i.Metadata.Version = "Remastered 2025" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			input := inputTrack()
			change(&input)
			if prepared(t, input).Fingerprint == base {
				t.Fatal("distinct recording collapsed")
			}
		})
	}
	a := inputTrack()
	a.Metadata.DurationMS = nil
	b := a
	b.Metadata.DurationMS = ptr(int64(0))
	if prepared(t, a).Fingerprint == prepared(t, b).Fingerprint {
		t.Fatal("unknown duration equals zero")
	}
	a = inputTrack()
	a.Metadata.Artists = []string{"a|b", "c"}
	b = a
	b.Metadata.Artists = []string{"a", "b|c"}
	if prepared(t, a).Fingerprint == prepared(t, b).Fingerprint {
		t.Fatal("ambiguous array encoding")
	}
}

func TestStableKeyAndURLAreNotFingerprintInputs(t *testing.T) {
	input := inputTrack()
	base := prepared(t, input)
	input.SourceTrackKey = ptr("track-42")
	input.SourceURL = ptr("https://open.spotify.com/track/42?token=secret#fragment")
	track := prepared(t, input)
	if track.Fingerprint != base.Fingerprint || track.IdentityKey != "key:track-42" || *track.SourceURL != "https://open.spotify.com/track/42" {
		t.Fatal("identity or URL sanitization changed")
	}
	input.Source = SourceVK
	input.SourceURL = nil
	if prepared(t, input).Fingerprint != base.Fingerprint {
		t.Fatal("source belongs to DB scope, not metadata digest")
	}
}

func TestPrepareDoesNotAliasCallerMetadata(t *testing.T) {
	input := inputTrack()
	track := prepared(t, input)
	input.Metadata.Artists[0] = "changed"
	*input.Metadata.DurationMS = 1
	if track.Metadata.Artists[0] != "Beta" || *track.Metadata.DurationMS != 123000 {
		t.Fatal("input aliased stored evidence")
	}
}

func TestInvalidTracksAndURLs(t *testing.T) {
	for _, change := range []func(*CanonicalTrackInput){
		func(i *CanonicalTrackInput) { i.Source = "other" },
		func(i *CanonicalTrackInput) { i.Metadata.Title = " \t " },
		func(i *CanonicalTrackInput) { i.Metadata.Title = "\xff" },
		func(i *CanonicalTrackInput) { i.Metadata.Artists = nil },
		func(i *CanonicalTrackInput) { i.Metadata.Artists = []string{" "} },
		func(i *CanonicalTrackInput) { i.Metadata.DurationMS = ptr(int64(-1)) },
		func(i *CanonicalTrackInput) { i.SourceTrackKey = ptr("") },
	} {
		input := inputTrack()
		change(&input)
		if _, err := PrepareCanonicalTrack(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected validation error, got %v", err)
		}
	}
	for _, u := range []string{"http://open.spotify.com/track/x", "https://user:secret@open.spotify.com/track/x", "https://example.org/track/x", "https://open.spotify.com.evil.test/track/x", "https://open.spotify.com:8443/track/x"} {
		if _, err := CleanSourceURL(SourceSpotify, u); !errors.Is(err, ErrInvalid) {
			t.Fatal("accepted invalid URL")
		}
	}
	if _, err := CleanSourceURL(SourceSpotify, "https://music.yandex.ru/album/1"); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted different source origin")
	}
	if clean, err := CleanSourceURL(SourceVK, "https://vk.com/music?access_token=secret#x"); err != nil || strings.Contains(clean, "secret") {
		t.Fatal("unsafe URL")
	}
}
