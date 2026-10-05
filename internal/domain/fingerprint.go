package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const FingerprintVersion = "v1"

// NormalizeMetadataText preserves punctuation, diacritics and version qualifiers.
// A fresh Caser per call avoids mutable transformer sharing between goroutines.
func NormalizeMetadataText(value string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(cases.Fold().String(norm.NFKC.String(value)))), " ")
}

func PrepareCanonicalTrack(input CanonicalTrackInput) (CanonicalTrack, error) {
	if input.Source != SourceSpotify && input.Source != SourceYandex && input.Source != SourceVK {
		return CanonicalTrack{}, fmt.Errorf("%w: source", ErrInvalid)
	}
	if err := ValidateMetadata(input.Metadata); err != nil {
		return CanonicalTrack{}, err
	}
	track := CanonicalTrack{CanonicalTrackInput: input}
	track.Metadata.Artists = slices.Clone(input.Metadata.Artists)
	if input.Metadata.DurationMS != nil {
		value := *input.Metadata.DurationMS
		track.Metadata.DurationMS = &value
	}
	track.NormalizedTitle = NormalizeMetadataText(input.Metadata.Title)
	track.NormalizedArtists = make([]string, len(input.Metadata.Artists))
	for i, artist := range input.Metadata.Artists {
		track.NormalizedArtists[i] = NormalizeMetadataText(artist)
		if !validText(track.NormalizedArtists[i], 512, true) {
			return CanonicalTrack{}, fmt.Errorf("%w: normalized artist length", ErrInvalid)
		}
	}
	slices.Sort(track.NormalizedArtists)
	track.NormalizedArtists = slices.Compact(track.NormalizedArtists)
	// Ordered JSON fields + string arrays prevent delimiter ambiguity.
	payload, _ := json.Marshal(struct {
		Version    string   `json:"version"`
		Title      string   `json:"title"`
		Artists    []string `json:"artists"`
		Album      string   `json:"album"`
		DurationMS *int64   `json:"duration_ms"`
		Edition    string   `json:"edition"`
	}{FingerprintVersion, track.NormalizedTitle, track.NormalizedArtists,
		NormalizeMetadataText(input.Metadata.Album), input.Metadata.DurationMS, NormalizeMetadataText(input.Metadata.Version)})
	digest := sha256.Sum256(payload)
	track.Fingerprint = FingerprintVersion + ":" + hex.EncodeToString(digest[:])
	track.IdentityKey = "fp:" + track.Fingerprint
	if input.SourceTrackKey != nil {
		key := strings.TrimSpace(*input.SourceTrackKey)
		if !validText(key, 512, true) {
			return CanonicalTrack{}, fmt.Errorf("%w: source track key", ErrInvalid)
		}
		track.SourceTrackKey = &key
		track.IdentityKey = "key:" + key
	}
	if input.SourceURL != nil {
		safe, err := CleanSourceURL(input.Source, *input.SourceURL)
		if err != nil {
			return CanonicalTrack{}, err
		}
		track.SourceURL = &safe
	}
	return track, nil
}

func ValidateMetadata(m TrackMetadata) error {
	if !validText(m.Title, 1024, true) || !validText(m.Album, 1024, false) || !validText(m.Version, 512, false) {
		return fmt.Errorf("%w: track metadata text", ErrInvalid)
	}
	if len(m.Artists) == 0 || len(m.Artists) > 32 {
		return fmt.Errorf("%w: artists", ErrInvalid)
	}
	for _, artist := range m.Artists {
		if !validText(artist, 512, true) {
			return fmt.Errorf("%w: artist", ErrInvalid)
		}
	}
	if m.DurationMS != nil && (*m.DurationMS < 0 || *m.DurationMS > 9007199254740991) {
		return fmt.Errorf("%w: duration", ErrInvalid)
	}
	return nil
}

func validText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= max && (!required || NormalizeMetadataText(s) != "")
}

// CleanSourceURL never fetches the URL. Only approved rendered-link origins are
// allowed; query/fragment (potential credentials) are always discarded.
func CleanSourceURL(source Source, value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" {
		return "", fmt.Errorf("%w: source URL", ErrInvalid)
	}
	host := strings.ToLower(u.Hostname())
	allowed := source == SourceSpotify && host == "open.spotify.com" ||
		source == SourceYandex && (host == "music.yandex.ru" || host == "music.yandex.com" || host == "music.yandex.kz") ||
		source == SourceVK && (host == "vk.com" || host == "music.vk.com")
	if !allowed {
		return "", fmt.Errorf("%w: source URL origin", ErrInvalid)
	}
	u.Host, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = host, "", "", "", false
	if u.Path == "" {
		u.Path = "/"
	}
	clean := u.String()
	if len(clean) > 2048 {
		return "", fmt.Errorf("%w: source URL length", ErrInvalid)
	}
	return clean, nil
}
