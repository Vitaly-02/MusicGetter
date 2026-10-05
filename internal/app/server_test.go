package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"musicgetter/internal/logging"
)

func TestGracefulShutdownWaitsForActiveRequest(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "propagated"))
	defer cancel()
	entered, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(key{}) != "propagated" {
			t.Error("missing base context value")
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			t.Error("active request cancelled during graceful drain")
		}
		_, _ = w.Write([]byte("finished"))
	})}
	listener := listen(t)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- Serve(ctx, server, listener, time.Second, func() { close(drained) }, testLogger())
	}()
	response := getAsync("http://" + listener.Addr().String())
	wait(t, entered)
	cancel()
	wait(t, drained)
	select {
	case err := <-serverDone:
		t.Fatalf("shutdown finished before handler: %v", err)
	default:
	}
	close(release)
	select {
	case got := <-response:
		if got.err != nil || got.body != "finished" {
			t.Fatalf("response: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("response timeout")
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timeout")
	}
}

func TestShutdownDeadlineCancelsRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, cancelled := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	})}
	listener := listen(t)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, server, listener, 30*time.Millisecond, func() {}, testLogger()) }()
	response := getAsync("http://" + listener.Addr().String())
	wait(t, entered)
	cancel()
	wait(t, cancelled)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected shutdown deadline error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown stuck")
	}
	select {
	case <-response:
	case <-time.After(3 * time.Second):
		t.Fatal("client stuck")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	listener := listen(t)
	_ = listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Serve(ctx, &http.Server{}, listener, time.Second, func() {}, testLogger()); err == nil {
		t.Fatal("missing serve error")
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func wait(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")
	}
}

type result struct {
	body string
	err  error
}

func getAsync(url string) <-chan result {
	done := make(chan result, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		response, err := client.Get(url)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		done <- result{body: string(body), err: err}
	}()
	return done
}

func testLogger() *slog.Logger { return logging.New(io.Discard, slog.LevelDebug) }
