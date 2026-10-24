package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// droppingRelay closes the connection as soon as it receives a REQ: a relay
// that goes away before answering.
func droppingRelay(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			_, msg, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var req []json.RawMessage
			if json.Unmarshal(msg, &req) == nil && len(req) >= 1 && string(req[0]) == `"REQ"` {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// A prefs relay that did not answer is not "this user has no preferences":
// the handler must report an error and must not cache "default".
func TestRelayPrefsHandler_NoAnswerIsNotDefault(t *testing.T) {
	for name, relay := range map[string]func(*testing.T) string{
		"unreachable": unreachableRelayURL,
		"muted":       mutedRelay,
		"dropping":    droppingRelay,
	} {
		t.Run(name, func(t *testing.T) {
			server, mr := setupTestServer(t)
			defer mr.Close()
			server.cfg.PrefsRelayURL = relay(t)
			const pubkey = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

			w := httptest.NewRecorder()
			server.RelayPrefsHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+pubkey, nil))

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("status %d, body %s; want 503 when the relay did not answer", w.Code, w.Body.String())
			}
			if cached, _ := server.cache.GetRelayPrefs(context.Background(), pubkey); cached != nil {
				t.Errorf("an unanswered fetch was cached with source %q", cached.Source)
			}
		})
	}
}

// A relay that answers EOSE with nothing gives "default" straight away,
// instead of waiting out the per-query timeout.
func TestRelayPrefsHandler_EOSEAnswersPromptly(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()
	server.cfg.PrefsRelayURL = eoseOnlyRelay(t)
	const pubkey = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	start := time.Now()
	w := httptest.NewRecorder()
	server.RelayPrefsHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+pubkey, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d; want 200", w.Code)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v; an EOSE answer should not wait for the timeout", elapsed)
	}
	if cached, _ := server.cache.GetRelayPrefs(context.Background(), pubkey); cached == nil || cached.Source != "default" {
		t.Errorf("an answered empty fetch should be cached as default, got %+v", cached)
	}
}

// fetchNIP65FromRelay feeds the user relay list and WoT scoring: a dropped
// connection is a failed relay, not "no relay list".
func TestFetchNIP65FromRelay_DroppedIsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	const pubkey = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	if ev, err := fetchNIP65FromRelay(ctx, droppingRelay(t), pubkey); err == nil {
		t.Errorf("dropped connection gave event %v and no error", ev)
	}
	if ev, err := fetchNIP65FromRelay(ctx, eoseOnlyRelay(t), pubkey); err != nil || ev != nil {
		t.Errorf("EOSE with no event gave (%v, %v); want (nil, nil)", ev, err)
	}
}
