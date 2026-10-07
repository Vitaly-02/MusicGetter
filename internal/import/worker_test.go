package importer

import (
	"testing"
	"time"
)

func TestBackoffBoundsAndSaturation(t *testing.T) {
	for attempt := 1; attempt <= 100; attempt++ {
		for range 100 {
			d := Backoff(attempt, time.Second, time.Minute)
			cap := time.Second
			for n := 1; n < attempt && cap < time.Minute; n++ {
				cap = min(2*cap, time.Minute)
			}
			if d < cap/2 || d >= cap {
				t.Fatalf("attempt %d delay %v cap %v", attempt, d, cap)
			}
		}
	}
	if Backoff(100, time.Duration(1<<62), time.Duration(1<<63-1)) <= 0 {
		t.Fatal("overflow")
	}
}
