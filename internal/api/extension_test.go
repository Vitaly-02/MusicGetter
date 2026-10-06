package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

const testImportID = "00000000-0000-0000-0000-000000000001"
const testBearer = "Bearer mge_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testOrigin = "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const goodCreate = `{"client_request_id":"request-1","source":{"service":"spotify","profile_key":"main","collection_key":"liked","kind":"favorites","title":"Liked"},"destination_collection_id":"` + testImportID + `"}`
const goodChunk = `{"schema_version":1,"idempotency_key":"chunk-0","sequence":0,"tracks":[{"title":"Song","artists":["Artist"],"position":0}]}`

type fakeUploads struct {
	calls int
	owner domain.ID
	err   error
}

func (f *fakeUploads) CreateUpload(_ context.Context, o domain.ID, _ importer.CreateRequest) (importer.Created, error) {
	f.calls++
	f.owner = o
	return importer.Created{ID: testImportID}, f.err
}
func (f *fakeUploads) AppendChunk(_ context.Context, o, id domain.ID, c importer.ChunkRequest) (importer.ChunkReceipt, error) {
	f.calls++
	f.owner = o
	return importer.ChunkReceipt{Sequence: *c.Sequence}, f.err
}
func (f *fakeUploads) CompleteUpload(_ context.Context, o, id domain.ID, _ importer.CompleteRequest) error {
	f.calls++
	f.owner = o
	return f.err
}
func (f *fakeUploads) CancelUpload(_ context.Context, o, id domain.ID) error {
	f.calls++
	f.owner = o
	return f.err
}
func (f *fakeUploads) GetUpload(_ context.Context, o, id domain.ID) (importer.UploadView, error) {
	f.calls++
	f.owner = o
	return importer.UploadView{ID: id}, f.err
}
func (f *fakeUploads) User(_ context.Context, o domain.ID) (domain.User, error) {
	f.calls++
	f.owner = o
	return domain.User{ID: o, TelegramUserID: 42}, f.err
}
func (f *fakeUploads) Destinations(_ context.Context, o domain.ID, _ string, _ int) ([]importer.DestinationView, error) {
	f.calls++
	f.owner = o
	return nil, f.err
}
func apiTestHandler(f *fakeUploads, p ExtensionPolicy) *ExtensionAPI {
	return NewExtensionAPI(&fakeAuth{}, f, f, p)
}
func serveAPI(a *ExtensionAPI, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.Register(mux)
	h := Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, mux)
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", testBearer)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestExtensionRoutesOwnershipAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		status, calls            int
	}{
		{"me", "GET", "/v1/me", "", 200, 1},
		{"destinations", "GET", "/v1/destinations?limit=10", "", 200, 1},
		{"create", "POST", "/v1/imports", goodCreate, 201, 1},
		{"tracks", "POST", "/v1/imports/" + testImportID + "/tracks", goodChunk, 200, 1},
		{"complete", "POST", "/v1/imports/" + testImportID + "/complete", `{"last_sequence":0,"observed_count":1,"completeness":"partial","reason":"unknown_end"}`, 200, 2},
		{"cancel", "POST", "/v1/imports/" + testImportID + "/cancel", "", 200, 2},
		{"cancel JSON", "POST", "/v1/imports/" + testImportID + "/cancel", "{}", 200, 2},
		{"status", "GET", "/v1/imports/" + testImportID, "", 200, 1},
		{"wrong method", "DELETE", "/v1/imports/" + testImportID, "", 405, 0},
		{"bad UUID", "GET", "/v1/imports/bad", "", 404, 0},
		{"no sequence", "POST", "/v1/imports/" + testImportID + "/tracks", strings.Replace(goodChunk, `"sequence":0,`, "", 1), 422, 0},
		{"unknown field", "POST", "/v1/imports", strings.Replace(goodCreate, `"client_request_id"`, `"cookies":"SECRET","client_request_id"`, 1), 400, 0},
		{"nested token", "POST", "/v1/imports", strings.Replace(goodCreate, `"service"`, `"access_token":"SECRET","service"`, 1), 400, 0},
		{"duplicate key", "POST", "/v1/imports", strings.Replace(goodCreate, `"client_request_id"`, `"client_request_id":"evil","client_request_id"`, 1), 400, 0},
		{"case folded field", "POST", "/v1/imports", strings.Replace(goodCreate, "client_request_id", "CLIENT_REQUEST_ID", 1), 400, 0},
		{"null input", "POST", "/v1/imports", "null", 400, 0},
		{"double JSON", "POST", "/v1/imports", goodCreate + "{}", 400, 0},
		{"source URL token", "POST", "/v1/imports/" + testImportID + "/tracks", strings.Replace(goodChunk, `"title"`, `"source_url":"https://open.spotify.com/track/1?access_token=SECRET","title"`, 1), 422, 0},
		{"cancel credentials", "POST", "/v1/imports/" + testImportID + "/cancel", `{"cookies":"SECRET"}`, 400, 0},
		{"GET credentials body", "GET", "/v1/me", `{"cookies":"SECRET"}`, 400, 0},
		{"query token", "GET", "/v1/me?access_token=SECRET", "", 400, 0},
		{"destination token", "GET", "/v1/destinations?token=SECRET", "", 400, 0},
		{"duplicate query", "GET", "/v1/destinations?limit=1&limit=2", "", 400, 0},
		{"oversize", "POST", "/v1/imports/" + testImportID + "/tracks", strings.Repeat(" ", importer.MaxPayloadBytes+1), 413, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeUploads{}
			w := serveAPI(apiTestHandler(f, ExtensionPolicy{}), tc.method, tc.path, tc.body, nil)
			if w.Code != tc.status || f.calls != tc.calls {
				t.Fatalf("got %d calls %d: %s", w.Code, f.calls, w.Body)
			}
			if f.calls > 0 && f.owner != "owner" {
				t.Fatal("owner not from session")
			}
			if w.Code >= 400 && (!strings.Contains(w.Body.String(), `"requestId":`) || strings.Contains(w.Body.String(), "SECRET")) {
				t.Fatal("unsafe/missing error envelope")
			}
		})
	}
}
func TestExtensionHeadersAndCORS(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		headers            map[string]string
		status             int
	}{
		{"allowed origin", "GET", "/v1/me", map[string]string{"Origin": testOrigin}, 200},
		{"website origin", "GET", "/v1/me", map[string]string{"Origin": "https://example.com"}, 403},
		{"wrong extension", "GET", "/v1/me", map[string]string{"Origin": "chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, 403},
		{"null origin", "GET", "/v1/me", map[string]string{"Origin": "null"}, 403},
		{"cookie", "GET", "/v1/me", map[string]string{"Cookie": "SID=SECRET"}, 400},
		{"source header", "GET", "/v1/me", map[string]string{"X-Spotify-Token": "SECRET"}, 400},
		{"no bearer", "GET", "/v1/me", map[string]string{"Authorization": ""}, 401},
		{"stream bearer", "GET", "/v1/me", map[string]string{"Authorization": "Bearer STREAM_SECRET"}, 401},
		{"preflight", "OPTIONS", "/v1/imports", map[string]string{"Origin": testOrigin, "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,content-type,idempotency-key"}, 204},
		{"bad preflight", "OPTIONS", "/v1/imports", map[string]string{"Origin": testOrigin, "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "x-spotify-token"}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeUploads{}
			w := serveAPI(apiTestHandler(f, ExtensionPolicy{Origins: []string{testOrigin}}), tc.method, tc.path, "", tc.headers)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if w.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("cookies enabled")
			}
			if tc.status == 204 && (f.calls != 0 || w.Header().Get("Access-Control-Allow-Origin") != testOrigin) {
				t.Fatal("preflight reached app or lacked CORS")
			}
		})
	}
	f := &fakeUploads{}
	a := apiTestHandler(f, ExtensionPolicy{})
	w := serveAPI(a, "POST", "/v1/imports", goodCreate, map[string]string{"Idempotency-Key": "different"})
	if w.Code != 409 || f.calls != 0 {
		t.Fatal("header/body conflict ignored")
	}
	w = serveAPI(a, "POST", "/v1/imports", goodCreate, map[string]string{"Content-Type": "text/plain"})
	if w.Code != 415 {
		t.Fatal("wrong media type accepted")
	}
	w = serveAPI(a, "POST", "/v1/imports", goodCreate, map[string]string{"Content-Encoding": "gzip"})
	if w.Code != 415 {
		t.Fatal("compressed upload accepted")
	}
}
func TestRateLimiterBoundsConcurrencyAndExpiry(t *testing.T) {
	l := NewRateLimiter(2)
	now := time.Now()
	l.now = func() time.Time { return now }
	var n atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("user", 5); ok {
				n.Add(1)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 5 {
		t.Fatal("racy limit")
	}
	l.Allow("second", 1)
	if ok, _ := l.Allow("third", 1); ok {
		t.Fatal("unbounded keys")
	}
	now = now.Add(time.Minute)
	if ok, _ := l.Allow("third", 1); !ok {
		t.Fatal("expired keys not reclaimed")
	}
}
func TestRateLimitsReturnRetryAfterAndIgnoreForwardedHeaders(t *testing.T) {
	f := &fakeUploads{}
	a := apiTestHandler(f, ExtensionPolicy{IPPerMinute: 1})
	if w := serveAPI(a, "GET", "/v1/me", "", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := serveAPI(a, "GET", "/v1/me", "", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || f.calls != 1 {
		t.Fatal("rate bypass")
	}
	a = apiTestHandler(f, ExtensionPolicy{ClaimPerMinute: 1})
	for i, want := range []int{201, 429} {
		path := "/v1/pair/claim"
		if i == 1 {
			path = "/v1/pairings/redeem"
		}
		w = serveAPI(a, "POST", path, `{"code":"x"}`, map[string]string{"Authorization": ""})
		if w.Code != want {
			t.Fatal("claim alias bypass", w.Code)
		}
	}
}

func TestOwnerQuotaSharedByDifferentTokens(t *testing.T) {
	f := &fakeUploads{}
	a := apiTestHandler(f, ExtensionPolicy{OwnerPerMinute: 1})
	if w := serveAPI(a, "GET", "/v1/me", "", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := serveAPI(a, "GET", "/v1/me", "", map[string]string{"Authorization": "Bearer mge_" + strings.Repeat("b", 43)})
	if w.Code != 429 || f.calls != 1 {
		t.Fatal("new session bypassed owner quota")
	}
}
