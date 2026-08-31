package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCORSSetsHeadersOnGET(t *testing.T) {
	called := false
	h := WithCORS(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/abc", nil)
	req.Header.Set("Origin", "https://space.cloistr.xyz")
	h(rec, req)

	if !called {
		t.Fatal("wrapped handler was not called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// The wildcard origin only stays safe as long as credentials are never
// allowed alongside it; a browser would reject the pair outright.
func TestWithCORSDoesNotAllowCredentials(t *testing.T) {
	h := WithCORS(func(w http.ResponseWriter, r *http.Request) {})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/v1/relays", nil))

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want unset", got)
	}
}

// Preflight must be answered by the middleware. The handlers underneath all
// reject non-GET with 405, which would fail the preflight.
func TestWithCORSPreflightShortCircuits(t *testing.T) {
	called := false
	h := WithCORS(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/relays", nil)
	req.Header.Set("Origin", "https://space.cloistr.xyz")
	req.Header.Set("Access-Control-Request-Method", "GET")
	h(rec, req)

	if called {
		t.Error("preflight reached the wrapped handler; it must terminate in the middleware")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods unset on preflight response")
	}
}
