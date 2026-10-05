package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeSource struct {
	calls   int
	offsets []int64
	cancel  context.CancelFunc
	err     error
}

func (f *fakeSource) Updates(_ context.Context, offset int64) ([]Update, error) {
	f.offsets = append(f.offsets, offset)
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.calls == 1 {
		return []Update{{ID: 1}, {ID: 1}, {ID: 2}}, nil
	}
	f.cancel()
	return nil, nil
}

type fakeUpdateStore struct {
	claimed map[int64]bool
	err     error
}

func (f *fakeUpdateStore) ClaimUpdate(_ context.Context, _ int64, id int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.claimed[id] {
		return false, nil
	}
	f.claimed[id] = true
	return true, nil
}

type fakeUpdateHandler struct {
	calls int
	panic bool
}

func (f *fakeUpdateHandler) Handle(context.Context, Update) error {
	f.calls++
	if f.panic {
		panic("SECRET")
	}
	return errors.New("SECRET")
}
func TestPollDedupAndFailureNoReplay(t *testing.T) {
	for _, panics := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		source := &fakeSource{cancel: cancel}
		store := &fakeUpdateStore{claimed: map[int64]bool{1: true}}
		handler := &fakeUpdateHandler{panic: panics}
		if err := Poll(ctx, source, store, handler, 42, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
			t.Fatal(err)
		}
		if handler.calls != 1 || source.offsets[1] != 3 {
			t.Fatal("replayed claimed update or failed command")
		}
	}
}
func TestPollStopsOnClaimFailureOrConflict(t *testing.T) {
	for _, apiFailure := range []bool{false, true} {
		source := &fakeSource{}
		if apiFailure {
			source.err = &APIError{Code: 409}
		}
		store := &fakeUpdateStore{err: errors.New("database unavailable")}
		handler := &fakeUpdateHandler{}
		if err := Poll(context.Background(), source, store, handler, 42, slog.Default()); err == nil {
			t.Fatal("expected failure")
		}
		if handler.calls != 0 || source.calls != 1 {
			t.Fatal("unexpected effect/retry")
		}
	}
}
