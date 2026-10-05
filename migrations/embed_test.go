package migrations

import (
	"strconv"
	"strings"
	"testing"
)

func TestReadinessVersionMatchesEmbeddedMigrations(t *testing.T) {
	entries, err := FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var latest int64
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			t.Fatalf("invalid migration name: %s", entry.Name())
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if version > latest {
			latest = version
		}
	}
	if latest != Version {
		t.Fatalf("readiness expects %d, latest migration is %d", Version, latest)
	}
}
