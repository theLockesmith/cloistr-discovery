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

// Production 2026-10-16: the UI sent ?search=damus and the API ignored it,
// returning all 3,529 relays, so searching did nothing.
func TestRelaysHandler_Search(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()
	ctx := context.Background()

	for _, e := range []*cache.RelayEntry{
		{URL: "wss://relay.damus.io", Name: "damus", Description: "the damus relay", Health: "online"},
		{URL: "wss://nos.lol", Name: "nos.lol", Description: "Generally accepts notes, except spammy ones.", Health: "online"},
		{URL: "wss://paid.example", Name: "Paid Hivetalk Relay", Description: "Mirrors DAMUS users", Health: "offline"},
		{URL: "wss://pyramid.example", Name: "", Description: "", Health: "online"},
	} {
		server.cache.SetRelayEntry(ctx, e, time.Hour)
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"?search=damus", []string{"wss://paid.example", "wss://relay.damus.io"}}, // name, URL, description; case-insensitive
		{"?search=DAMUS&health=online", []string{"wss://relay.damus.io"}},         // combines with other filters
		{"?search=spammy", []string{"wss://nos.lol"}},                             // description only
		{"?search=pyramid", []string{"wss://pyramid.example"}},                    // URL only, no name
		{"?search=%20hivetalk%20", []string{"wss://paid.example"}},                // trimmed
		{"?search=nomatch", nil},
		{"?search=", []string{"wss://nos.lol", "wss://paid.example", "wss://pyramid.example", "wss://relay.damus.io"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			server.RelaysHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/relays"+tt.query, nil))
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
