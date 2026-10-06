package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

type ExtensionPolicy struct {
	Origins                                     []string
	IPPerMinute, OwnerPerMinute, ClaimPerMinute int
}

func (p ExtensionPolicy) defaults() ExtensionPolicy {
	if p.IPPerMinute <= 0 {
		p.IPPerMinute = 120
	}
	if p.OwnerPerMinute <= 0 {
		p.OwnerPerMinute = 60
	}
	if p.ClaimPerMinute <= 0 {
		p.ClaimPerMinute = 5
	}
	return p
}
func (a *ExtensionAPI) policy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if len(r.Header.Values("Origin")) > 1 {
			writeError(w, r, 403, "origin_not_allowed", "Origin is not allowed")
			return
		}
		if origin != "" {
			allowed := false
			for _, candidate := range a.policyConfig.Origins {
				if candidate == origin {
					allowed = true
					break
				}
			}
			if !allowed {
				writeError(w, r, 403, "origin_not_allowed", "Origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After, Location")
		}
		// Do not trust forwarded headers; deployment may provide a trusted proxy layer.
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = "unknown"
		}
		if !a.allow(w, r, "ip:"+ip, a.policyConfig.IPPerMinute) {
			return
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				writeError(w, r, 403, "origin_not_allowed", "Extension Origin required for preflight")
				return
			}
			requested := r.Header.Get("Access-Control-Request-Method")
			if requested != "GET" && requested != "POST" {
				writeError(w, r, 403, "cors_rejected", "Method is not allowed")
				return
			}
			for _, v := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "", "authorization", "content-type", "idempotency-key", "x-request-id":
				default:
					writeError(w, r, 403, "cors_rejected", "Header is not allowed")
					return
				}
			}
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(204)
			return
		}
		for name := range r.Header {
			lower := strings.ToLower(name)
			if lower != "authorization" && (strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "api-key") || strings.Contains(lower, "authorization")) {
				writeError(w, r, 400, "credentials_not_allowed", "Only MusicGetter Bearer authentication is supported")
				return
			}
		}
		if r.URL.Path != "/v1/destinations" && r.URL.RawQuery != "" {
			writeError(w, r, 400, "invalid_query", "Query parameters are not supported")
			return
		}
		if r.Method == http.MethodGet && (r.ContentLength != 0 || len(r.TransferEncoding) > 0) {
			writeError(w, r, 400, "invalid_body", "GET requests must not contain a body")
			return
		}
		claim := r.URL.Path == "/v1/pair/claim" || r.URL.Path == "/v1/pairings/redeem"
		if claim && !a.allow(w, r, "claim:"+ip, a.policyConfig.ClaimPerMinute) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *ExtensionAPI) allow(w http.ResponseWriter, r *http.Request, key string, limit int) bool {
	ok, retry := a.limiter.Allow(key, limit)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, r, 429, "rate_limited", "Too many requests; retry after the indicated delay")
	}
	return ok
}
