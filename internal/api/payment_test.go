package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

// Production 2026-10-29: the UI's Payment dropdown sent ?payment=paid and the
// API ignored it. Paid and free both returned all ~3,200 relays, of which one
// in two hundred actually required payment.
func TestRelaysHandler_Payment(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()
	ctx := context.Background()

	for _, e := range []*cache.RelayEntry{
		{URL: "wss://free-online.example", Health: "online"},
		{URL: "wss://free-offline.example", Health: "offline"},
		{URL: "wss://paid-online.example", Health: "online", PaymentRequired: true},
		{URL: "wss://paid-auth.example", Health: "online", PaymentRequired: true, AuthRequired: true},
	} {
		server.cache.SetRelayEntry(ctx, e, time.Hour)
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"?payment=paid", []string{"wss://paid-auth.example", "wss://paid-online.example"}},
		{"?payment=free", []string{"wss://free-offline.example", "wss://free-online.example"}},
		{"?payment=PAID", []string{"wss://paid-auth.example", "wss://paid-online.example"}}, // case-insensitive
		{"?payment=free&health=online", []string{"wss://free-online.example"}},              // combines with other filters
		{"?payment=", []string{"wss://free-offline.example", "wss://free-online.example", "wss://paid-auth.example", "wss://paid-online.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			server.RelaysHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relays"+tt.query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var resp RelaysResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range resp.Relays {
				got = append(got, r.URL)
			}
			sort.Strings(got)
			if len(got) != len(tt.want) || resp.Total != len(tt.want) {
				t.Fatalf("got %v (total %d), want %v", got, resp.Total, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// A payment value the API cannot answer must say so, not silently return
// every relay as if the filter had matched. NIP-11 only says whether payment
// is required, so there is no "hybrid".
func TestRelaysHandler_PaymentUnknownValue(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	w := httptest.NewRecorder()
	server.RelaysHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relays?payment=hybrid", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}
