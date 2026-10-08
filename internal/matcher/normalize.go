package matcher

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"musicgetter/internal/domain"
)

// Matching normalization is deliberately separate from canonical fingerprint v1.
func Normalize(text string) string {
	text = strings.ReplaceAll(domain.NormalizeMetadataText(text), "ё", "е")
	var out strings.Builder
	for _, r := range text {
		switch {
		case r == '\'' || r == '’' || r == 'ʼ': // don't == dont; retain boundaries for other punctuation
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			out.WriteRune(r)
		default:
			out.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

type normalizedTrack struct {
	title       string
	artists     []string
	primary     string
	album       string
	duration    *int64
	versions    []string
	descriptors []string
}

var featureRE = regexp.MustCompile(`(?i)(?:^|[\s(\[])(?:feat(?:uring)?|ft)\.?\s+([^\)\]]+)[\)\]]?`)
var bracketRE = regexp.MustCompile(`\([^()]*\)|\[[^\[\]]*\]`)
var separatorRE = regexp.MustCompile(`\s+[-–—:]\s+`)
var suffixRE = regexp.MustCompile(`(?i)\s+((?:\d{4}\s+)?(?:remix|remaster(?:ed)?|radio edit|sped up|slowed(?: down)?|instrumental|acoustic)(?:\s+\d{4})?)$`)

func normalizeTrack(m domain.TrackMetadata) normalizedTrack {
	n := normalizedTrack{album: Normalize(m.Album), duration: m.DurationMS}
	addArtist := func(value string) {
		if v := Normalize(value); v != "" {
			n.artists = append(n.artists, v)
		}
	}
	title := domain.NormalizeMetadataText(m.Title)
	// Only explicit annotation boundaries/suffixes are treated as editions.
	// "Live Forever" and "Acoustic Soul" remain ordinary titles.
	addVersion := func(value string) bool {
		markers, descriptor := version(value)
		if len(markers) == 0 {
			return false
		}
		n.versions = append(n.versions, markers...)
		n.descriptors = append(n.descriptors, descriptor)
		return true
	}
	consumeEdition := func(part string) bool {
		if loc := featureRE.FindStringSubmatchIndex(part); loc != nil {
			// A remix annotation may carry featured artists; an artist named
			// Remix is not itself evidence of a remix edition.
			if addVersion(part[:loc[0]]) {
				addArtist(part[loc[2]:loc[3]])
				return true
			}
			return false
		}
		return addVersion(part)
	}
	title = bracketRE.ReplaceAllStringFunc(title, func(part string) string {
		if consumeEdition(part) {
			return " "
		}
		return part
	})
	parts := separatorRE.Split(title, -1)
	if len(parts) > 1 {
		kept := []string{parts[0]}
		for _, part := range parts[1:] {
			if !consumeEdition(part) {
				kept = append(kept, part)
			}
		}
		title = strings.Join(kept, " ")
	}
	if m.Version != "" {
		if !addVersion(m.Version) {
			n.descriptors = append(n.descriptors, Normalize(m.Version))
			n.versions = append(n.versions, "other")
		}
	}
	// Artist arrays are authoritative. Do not invent aliases, transliterate or
	// split arbitrary '&', '/' or commas in band names.
	for _, artist := range m.Artists {
		text := domain.NormalizeMetadataText(artist)
		text = featureRE.ReplaceAllStringFunc(text, func(part string) string { match := featureRE.FindStringSubmatch(part); addArtist(match[1]); return " " })
		if n.primary == "" {
			n.primary = Normalize(text)
		}
		addArtist(text)
	}
	title = featureRE.ReplaceAllStringFunc(title, func(part string) string { match := featureRE.FindStringSubmatch(part); addArtist(match[1]); return " " })
	title = strings.TrimSpace(title)
	if loc := suffixRE.FindStringSubmatchIndex(title); loc != nil {
		if addVersion(title[loc[2]:loc[3]]) {
			title = title[:loc[0]]
		}
	}
	n.title = Normalize(title)
	n.artists = unique(n.artists)
	n.versions = unique(n.versions)
	n.descriptors = unique(n.descriptors)
	return n
}
func unique(v []string) []string { slices.Sort(v); return slices.Compact(v) }

func version(value string) ([]string, string) {
	text := Normalize(value)
	// Normalize spelling of edition labels, not artist aliases.
	text = strings.ReplaceAll(text, "remastered", "remaster")
	text = strings.ReplaceAll(text, "slowed down", "slowed")
	tokens := " " + text + " "
	markers := []string{}
	for _, marker := range []string{"live", "remix", "remaster", "radio edit", "sped up", "slowed", "instrumental", "acoustic"} {
		if strings.Contains(tokens, " "+marker+" ") {
			markers = append(markers, marker)
		}
	}
	if len(markers) > 0 {
		text = " " + text + " "
		text = strings.ReplaceAll(text, " version ", " ")
	}
	return markers, strings.Join(unique(strings.Fields(text)), " ")
}
