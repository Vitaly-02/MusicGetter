package destination_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"musicgetter/internal/destination"
)

func TestCapabilitiesValidation(t *testing.T) {
	for _, c := range []destination.Capabilities{
		{SupportsBulkAdd: true}, {SupportsBulkAdd: true, MaxBulkSize: 201}, {MaxBulkSize: 2}, {MaxBulkSize: -1}, {IdempotentCreatePlaylist: true},
	} {
		if c.Validate() == nil {
			t.Fatalf("accepted inconsistent capabilities: %+v", c)
		}
	}
	for _, c := range []destination.Capabilities{{}, {MaxBulkSize: 1}, {SupportsBulkAdd: true, MaxBulkSize: 200}, {SupportsPlaylists: true, IdempotentCreatePlaylist: true}} {
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestAlbumPlaylistTitle(t *testing.T) {
	for _, tc := range []struct {
		artists     []string
		album, want string
	}{
		{[]string{" Artist "}, " Album ", "Artist — Album"},
		{[]string{"One", "Two"}, "Album", "One, Two — Album"},
	} {
		got, err := destination.AlbumPlaylistTitle(tc.artists, tc.album)
		if err != nil || got != tc.want {
			t.Fatal(got, err)
		}
	}
	for _, artists := range [][]string{nil, {""}, {" "}} {
		if _, err := destination.AlbumPlaylistTitle(artists, "Album"); err == nil {
			t.Fatal("empty artist accepted")
		}
	}
	name, err := destination.AlbumPlaylistTitle([]string{strings.Repeat("Ж", 2000)}, strings.Repeat("Я", 2000))
	if err != nil || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 1024 {
		t.Fatal("invalid bounded Unicode title", err)
	}
}
