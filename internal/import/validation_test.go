package importer

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCanonicalRequestDigestAndValidation(t *testing.T) {
	var first, second ChunkRequest
	if err := json.Unmarshal([]byte(`{"schema_version":1,"idempotency_key":"c","sequence":0,"tracks":[{"title":"Song","artists":["A"],"position":0}]}`), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{ "tracks": [{"artists":["A"],"position":0,"title":"Song"}], "sequence":0,"idempotency_key":"c","schema_version":1}`), &second); err != nil {
		t.Fatal(err)
	}
	if first.Validate() != nil || !bytes.Equal(Digest(first), Digest(second)) {
		t.Fatal("JSON formatting changed idempotency digest")
	}
	second.Tracks[0].Title = "Changed"
	if bytes.Equal(Digest(first), Digest(second)) {
		t.Fatal("metadata change not detected")
	}
	for _, modify := range []func(*ChunkRequest){
		func(c *ChunkRequest) { c.Sequence = nil }, func(c *ChunkRequest) { c.SchemaVersion = 2 }, func(c *ChunkRequest) { c.Tracks = nil }, func(c *ChunkRequest) { c.IdempotencyKey = "unsafe key" }, func(c *ChunkRequest) { c.Tracks = make([]TrackInput, 201) },
	} {
		c := first
		modify(&c)
		if c.Validate() == nil {
			t.Fatal("invalid chunk accepted")
		}
	}
	negative := int64(-1)
	zero := int64(0)
	valid := CompleteRequest{LastSequence: &negative, ObservedCount: &zero, Completeness: "complete", Reason: "visible_end_confirmed"}
	if valid.Validate() != nil {
		t.Fatal("empty complete invalid")
	}
	valid.Reason = "unknown_end"
	if valid.Validate() == nil {
		t.Fatal("unknown end marked complete")
	}
}
