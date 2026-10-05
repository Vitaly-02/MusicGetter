package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"musicgetter/internal/domain"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAuth struct {
	calls       int
	code, token string
	err         error
}

func (f *fakeAuth) Redeem(ctx context.Context, code string) (string, domain.ExtensionSession, error) {
	f.calls++
	f.code = code
	if _, ok := ctx.Deadline(); !ok {
		panic("missing deadline")
	}
	return "TOKEN", domain.ExtensionSession{OwnerID: "owner"}, f.err
}
func (f *fakeAuth) Authenticate(_ context.Context, token string) (domain.ExtensionSession, error) {
	f.calls++
	f.token = token
	return domain.ExtensionSession{OwnerID: "owner"}, f.err
}
func TestPairingRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, auth string
		err                            error
		want, calls                    int
	}{
		{name: "redeem", method: "POST", path: "/v1/pairings/redeem", body: `{"code":"CODE"}`, want: 201, calls: 1},
		{name: "unknown field", method: "POST", path: "/v1/pairings/redeem", body: `{"code":"CODE","ownerId":"victim"}`, want: 400},
		{name: "extra JSON", method: "POST", path: "/v1/pairings/redeem", body: `{"code":"CODE"}{}`, want: 400},
		{name: "oversize", method: "POST", path: "/v1/pairings/redeem", body: `{"code":"` + strings.Repeat("x", 1024) + `"}`, want: 400},
		{name: "wrong method", method: "GET", path: "/v1/pairings/redeem", want: 405},
		{name: "expired", method: "POST", path: "/v1/pairings/redeem", body: `{"code":"CODE"}`, want: 401, calls: 1, err: domain.ErrNotFound},
		{name: "unavailable", method: "POST", path: "/v1/pairings/redeem", body: `{"code":"CODE"}`, want: 503, calls: 1, err: errors.New("SECRET")},
		{name: "session", method: "GET", path: "/v1/extension/session", auth: "Bearer token", want: 200, calls: 1},
		{name: "no auth", method: "GET", path: "/v1/extension/session", want: 401},
		{name: "wrong scheme", method: "GET", path: "/v1/extension/session", auth: "Basic token", want: 401},
		{name: "revoked", method: "GET", path: "/v1/extension/session", auth: "Bearer token", want: 401, calls: 1, err: domain.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAuth{err: tc.err}
			h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), NewHealth(func(context.Context) error { return nil }, time.Second), 2*time.Second, ExtensionRoutes(f))
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", tc.auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || f.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "SECRET") {
				t.Fatal("unsafe response")
			}
			if tc.want == 201 && !strings.Contains(w.Body.String(), `"token":"TOKEN"`) {
				t.Fatal("missing bearer")
			}
		})
	}
}
