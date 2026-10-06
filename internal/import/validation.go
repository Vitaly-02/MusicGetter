package importer

import (
	"musicgetter/internal/domain"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidKey(value string) bool { return keyPattern.MatchString(value) }
func ValidID(value string) bool  { return uuidPattern.MatchString(value) }
func textOK(value string, max int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len([]rune(value)) <= max && strings.TrimSpace(value) != ""
}
func (c CreateRequest) Validate() error {
	s := c.Source
	if !ValidKey(c.ClientRequestID) || !ValidID(string(c.DestinationCollectionID)) || !textOK(s.ProfileKey, 256) || !textOK(s.CollectionKey, 512) || !textOK(s.Title, 1024) {
		return domain.ErrInvalid
	}
	if s.Service != domain.SourceSpotify && s.Service != domain.SourceYandex && s.Service != domain.SourceVK {
		return domain.ErrInvalid
	}
	if s.Kind != domain.SourceFavorites && s.Kind != domain.SourcePlaylist && s.Kind != domain.SourceAlbum && s.Kind != domain.SourceSelection {
		return domain.ErrInvalid
	}
	return nil
}
func (c ChunkRequest) Validate() error {
	if c.SchemaVersion != 1 || !ValidKey(c.IdempotencyKey) || c.Sequence == nil || *c.Sequence < 0 || *c.Sequence >= MaxChunks || len(c.Tracks) < 1 || len(c.Tracks) > MaxChunkTracks {
		return domain.ErrInvalid
	}
	for _, t := range c.Tracks {
		if t.Position == nil || *t.Position < 0 || *t.Position > 9007199254740991 || domain.ValidateMetadata(t.Metadata()) != nil {
			return domain.ErrInvalid
		}
		if t.SourceTrackKey != nil && !textOK(*t.SourceTrackKey, 512) {
			return domain.ErrInvalid
		}
		if t.SourceURL != nil {
			u, err := url.Parse(*t.SourceURL)
			if err != nil || len(*t.SourceURL) > 2048 || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(*t.SourceURL, "#") || u.User != nil {
				return domain.ErrInvalid
			}
		}
	}
	return nil
}
func (c CompleteRequest) Validate() error {
	if c.LastSequence == nil || *c.LastSequence < -1 || *c.LastSequence >= MaxChunks || c.ObservedCount == nil || *c.ObservedCount < 0 || *c.ObservedCount > MaxObservations {
		return domain.ErrInvalid
	}
	if c.Completeness == "complete" && c.Reason == "visible_end_confirmed" {
		return nil
	}
	if c.Completeness == "partial" && (c.Reason == "user_stopped" || c.Reason == "dom_changed" || c.Reason == "unknown_end") {
		return nil
	}
	return domain.ErrInvalid
}
