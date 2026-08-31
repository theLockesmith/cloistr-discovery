package api

import "net/http"

// WithCORS wraps a public API handler so browsers may read its response
// cross-origin. Without it the service answers correctly and the browser
// discards the body — a 200 that never reaches the page. cloistr-space hit
// this on /api/v1/relay-prefs/ and silently fell back to querying a relay
// directly, so nothing looked broken from outside a browser console.
//
// The origin is a wildcard rather than a *.cloistr.xyz allowlist because
// every route this wraps is an unauthenticated GET over public Nostr data.
// The wildcard is also the safer of the two: it cannot be combined with
// Access-Control-Allow-Credentials, so a browser can never attach the
// router's affinity cookie to a cross-origin read. An allowlist would carry
// no benefit for public data and would invite adding credentials later.
//
// Admin routes are deliberately not wrapped — they are authenticated and
// must stay same-origin.
func WithCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Preflight has to terminate here. Every wrapped handler rejects a
		// non-GET method with 405, which fails the preflight and takes the
		// real request down with it — so a route that grows a custom header
		// later would break even though the simple GET case works today.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}
