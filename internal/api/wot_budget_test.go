package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

// silentRelay accepts TCP connections and never answers, like a relay that
// hangs mid-handshake. Returns its ws:// URL.
func silentRelay(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		for _, c := range conns {
			c.Close()
		}
	})
	return "ws://" + ln.Addr().String()
}

// Production 2026-10-16: recommend?pubkey=<jb55> answered 502 after ~21s.
// Fetching the contact list (10s) and then the follows' relay lists (15s)
// ran back to back with no overall bound, past the edge's timeout. The
// recommendation must answer within the WoT budget, with whatever follow
// data arrived, and must not cache that partial result as complete.
func TestRecommend_WoTIsBoundedAndPartial(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()
	server.cfg.SeedRelays = []string{silentRelay(t), silentRelay(t)}
	ctx := context.Background()

	server.cache.SetRelayEntry(ctx, &cache.RelayEntry{URL: "wss://a.example", Name: "A", Health: "online", LastChecked: time.Now()}, time.Hour)

	const user = "32e1827635450ebb3c5a7d12c1f8e7b2b514439ac10a67eef3d9fd9c5c68e245"
	follows := make([]string, 300)
	for i := range follows {
		follows[i] = fmt.Sprintf("%064x", i+1)
	}
	server.cache.SetUserContacts(ctx, &cache.UserContactsEntry{Pubkey: user, Follows: follows, FetchedAt: time.Now()})

	start := time.Now()
	w := httptest.NewRecorder()
	server.RecommendRelaysHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relays/recommend?pubkey="+user, nil))
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if elapsed > wotBudget+2*time.Second {
		t.Fatalf("took %v; the WoT work must stop at its %v budget", elapsed, wotBudget)
	}
	var resp RecommendationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Recommendations) != 1 {
		t.Fatalf("want the 1 relay recommended without WoT, got %d", len(resp.Recommendations))
	}
	if !resp.Criteria.WoTPartial {
		t.Error("criteria.wot_partial must say the follow data was cut short")
	}
	if cached, _ := server.cache.GetWoTRelayScores(ctx, user); cached != nil {
		t.Error("a partial WoT result must not be cached as complete")
	}
}

// The contact list fetch itself is bounded by the same budget.
func TestRecommend_ContactFetchIsBounded(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()
	server.cfg.SeedRelays = []string{silentRelay(t)}
	server.cache.SetRelayEntry(context.Background(), &cache.RelayEntry{URL: "wss://a.example", Health: "online", LastChecked: time.Now()}, time.Hour)

	start := time.Now()
	w := httptest.NewRecorder()
	server.RecommendRelaysHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relays/recommend?pubkey=32e1827635450ebb3c5a7d12c1f8e7b2b514439ac10a67eef3d9fd9c5c68e245", nil))
	if elapsed := time.Since(start); elapsed > wotBudget+2*time.Second || w.Code != http.StatusOK {
		t.Fatalf("status %d after %v; want 200 within the %v budget", w.Code, elapsed, wotBudget)
	}
	// cloistr-pr on !26: a cut-off contact fetch must say so, or a client
	// cannot tell "no WoT because of the timeout" from "no WoT at all".
	var resp RecommendationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Criteria.WoTPartial {
		t.Error("criteria.wot_partial must be set when the contact list fetch was cut off by the budget")
	}
}

// mutedRelay completes the WebSocket handshake, then never sends anything:
// a relay that is up but does not answer REQs.
func mutedRelay(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// A contact fetch where no relay answered (each gave up after its own wait,
// inside the budget) is not "this user follows nobody": it must set
// wot_partial and must not be cached as an empty contact list.
func TestRecommend_ContactFetchNoAnswerIsPartial(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()
	server.cfg.SeedRelays = []string{mutedRelay(t), mutedRelay(t)}
	ctx := context.Background()
	server.cache.SetRelayEntry(ctx, &cache.RelayEntry{URL: "wss://a.example", Health: "online", LastChecked: time.Now()}, time.Hour)
	const user = "32e1827635450ebb3c5a7d12c1f8e7b2b514439ac10a67eef3d9fd9c5c68e245"

	w := httptest.NewRecorder()
	server.RecommendRelaysHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relays/recommend?pubkey="+user, nil))
	var resp RecommendationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(resp.Recommendations) != 1 {
		t.Fatalf("status %d, %d recommendations; want 200 with the relay recommended without WoT", w.Code, len(resp.Recommendations))
	}
	if !resp.Criteria.WoTPartial {
		t.Error("criteria.wot_partial must be set when no relay answered the contact list request")
	}
	if cached, _ := server.cache.GetUserContacts(ctx, user); cached != nil {
		t.Errorf("an unanswered contact fetch was cached as %d follows", len(cached.Follows))
	}
}
