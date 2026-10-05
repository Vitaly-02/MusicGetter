package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestJSONLevelAndContext(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, slog.LevelInfo)
	logger.Debug("hidden")
	ctx := WithContext(context.Background(), logger.With("request_id", "request-1"))
	FromContext(ctx).InfoContext(ctx, "visible", "count", 3)
	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["level"] != "INFO" || record["msg"] != "visible" || record["request_id"] != "request-1" || record["service"] != "musicgetter-backend" {
		t.Fatalf("unexpected log: %v", record)
	}
}
