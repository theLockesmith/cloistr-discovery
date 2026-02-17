package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
)

// setupTestServer creates a test API server with miniredis.
func setupTestServer(t *testing.T) (*Server, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to create cache client: %v", err)
	}

	cfg := &config.Config{
		Port:     8080,
		LogLevel: "info",
		CacheURL: "redis://" + mr.Addr(),
	}

	server := New(cfg, cacheClient)

	return server, mr
}

func TestMetricsHandler(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	tests := []struct {
		name           string
		method         string
		wantStatusCode int
		wantContentType string
	}{
		{
			name:           "GET request returns metrics",
			method:         http.MethodGet,
			wantStatusCode: http.StatusOK,
			wantContentType: "text/plain",
		},
		{
			name:           "POST request also works (prometheus handler accepts it)",
			method:         http.MethodPost,
			wantStatusCode: http.StatusOK,
			wantContentType: "text/plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/metrics", nil)
			w := httptest.NewRecorder()

			server.MetricsHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("MetricsHandler() status = %v, want %v", resp.StatusCode, tt.wantStatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, tt.wantContentType) {
				t.Errorf("MetricsHandler() Content-Type = %v, want prefix %v", contentType, tt.wantContentType)
			}
		})
	}
}

func TestRelaysHandler(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Setup test data
	relay1 := &cache.RelayEntry{
		URL:           "wss://relay1.example.com",
		Name:          "Relay 1",
		Description:   "Test relay 1",
		Health:        "online",
		SupportedNIPs: []int{1, 11, 42},
		CountryCode:   "US",
		LatencyMs:     50,
		LastChecked:   time.Now(),
	}
	relay2 := &cache.RelayEntry{
		URL:           "wss://relay2.example.com",
		Name:          "Relay 2",
		Description:   "Test relay 2",
		Health:        "degraded",
		SupportedNIPs: []int{1, 11},
		CountryCode:   "US",
		LatencyMs:     150,
		LastChecked:   time.Now(),
	}
	relay3 := &cache.RelayEntry{
		URL:           "wss://relay3.example.com",
		Name:          "Relay 3",
		Description:   "Test relay 3",
		Health:        "online",
		SupportedNIPs: []int{42, 50},
		CountryCode:   "UK",
		LatencyMs:     75,
		LastChecked:   time.Now(),
	}
	relay4 := &cache.RelayEntry{
		URL:           "wss://relay4.example.com",
		Name:          "Relay 4",
		Description:   "Test relay 4",
		Health:        "offline",
		SupportedNIPs: []int{1},
		CountryCode:   "JP",
		LatencyMs:     200,
		LastChecked:   time.Now(),
	}

	server.cache.SetRelayEntry(ctx, relay1, time.Hour)
	server.cache.SetRelayEntry(ctx, relay2, time.Hour)
	server.cache.SetRelayEntry(ctx, relay3, time.Hour)
	server.cache.SetRelayEntry(ctx, relay4, time.Hour)

	tests := []struct {
		name           string
		method         string
		queryParams    string
		wantStatusCode int
		wantMinRelays  int
		wantMaxRelays  int
		checkRelays    func(t *testing.T, relays []cache.RelayEntry)
	}{
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			queryParams:    "",
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "no filters returns all relays",
			method:         http.MethodGet,
			queryParams:    "",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  4,
			wantMaxRelays:  4,
		},
		{
			name:           "health filter only returns matching relays",
			method:         http.MethodGet,
			queryParams:    "?health=online",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  2,
			wantMaxRelays:  2,
			checkRelays: func(t *testing.T, relays []cache.RelayEntry) {
				for _, r := range relays {
					if r.Health != "online" {
						t.Errorf("relay %s health = %s, want online", r.URL, r.Health)
					}
				}
			},
		},
		{
			name:           "filter by single NIP",
			method:         http.MethodGet,
			queryParams:    "?nips=1",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  3,
			wantMaxRelays:  3,
			checkRelays: func(t *testing.T, relays []cache.RelayEntry) {
				for _, r := range relays {
					hasNIP1 := false
					for _, nip := range r.SupportedNIPs {
						if nip == 1 {
							hasNIP1 = true
							break
						}
					}
					if !hasNIP1 {
						t.Errorf("relay %s does not support NIP-1", r.URL)
					}
				}
			},
		},
		{
			name:           "filter by multiple NIPs (intersection)",
			method:         http.MethodGet,
			queryParams:    "?nips=1,11",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  2,
			wantMaxRelays:  2,
			checkRelays: func(t *testing.T, relays []cache.RelayEntry) {
				for _, r := range relays {
					hasNIP1 := false
					hasNIP11 := false
					for _, nip := range r.SupportedNIPs {
						if nip == 1 {
							hasNIP1 = true
						}
						if nip == 11 {
							hasNIP11 = true
						}
					}
					if !hasNIP1 || !hasNIP11 {
						t.Errorf("relay %s does not support both NIP-1 and NIP-11", r.URL)
					}
				}
			},
		},
		{
			name:           "filter by location",
			method:         http.MethodGet,
			queryParams:    "?location=US",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  2,
			wantMaxRelays:  2,
			checkRelays: func(t *testing.T, relays []cache.RelayEntry) {
				for _, r := range relays {
					if r.CountryCode != "US" {
						t.Errorf("relay %s country = %s, want US", r.URL, r.CountryCode)
					}
				}
			},
		},
		{
			name:           "filter by health status",
			method:         http.MethodGet,
			queryParams:    "?nips=1&health=online",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  1,
			wantMaxRelays:  1,
			checkRelays: func(t *testing.T, relays []cache.RelayEntry) {
				for _, r := range relays {
					if r.Health != "online" {
						t.Errorf("relay %s health = %s, want online", r.URL, r.Health)
					}
				}
			},
		},
		{
			name:           "filter by NIP and location (intersection)",
			method:         http.MethodGet,
			queryParams:    "?nips=42&location=UK",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  1,
			wantMaxRelays:  1,
			checkRelays: func(t *testing.T, relays []cache.RelayEntry) {
				if len(relays) != 1 {
					return
				}
				if relays[0].URL != "wss://relay3.example.com" {
					t.Errorf("expected relay3.example.com, got %s", relays[0].URL)
				}
			},
		},
		{
			name:           "filter with no matches",
			method:         http.MethodGet,
			queryParams:    "?nips=999",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  0,
			wantMaxRelays:  0,
		},
		{
			name:           "invalid NIP value is skipped",
			method:         http.MethodGet,
			queryParams:    "?nips=abc,1",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  3,
			wantMaxRelays:  3,
		},
		{
			name:           "multiple NIPs with spaces",
			method:         http.MethodGet,
			queryParams:    "?nips=1,%2042",
			wantStatusCode: http.StatusOK,
			wantMinRelays:  1,
			wantMaxRelays:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/relays"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.RelaysHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("RelaysHandler() status = %v, want %v", resp.StatusCode, tt.wantStatusCode)
				return
			}

			if tt.wantStatusCode != http.StatusOK {
				return
			}

			var relaysResp RelaysResponse
			if err := json.NewDecoder(resp.Body).Decode(&relaysResp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if relaysResp.Total < tt.wantMinRelays || relaysResp.Total > tt.wantMaxRelays {
				t.Errorf("RelaysHandler() total = %v, want between %v and %v", relaysResp.Total, tt.wantMinRelays, tt.wantMaxRelays)
			}

			if len(relaysResp.Relays) != relaysResp.Total {
				t.Errorf("RelaysHandler() len(relays) = %v, want %v", len(relaysResp.Relays), relaysResp.Total)
			}

			if tt.checkRelays != nil {
				tt.checkRelays(t, relaysResp.Relays)
			}
		})
	}
}

func TestIntersect(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: nil,
		},
		{
			name: "first empty",
			a:    []string{},
			b:    []string{"a", "b"},
			want: nil,
		},
		{
			name: "second empty",
			a:    []string{"a", "b"},
			b:    []string{},
			want: nil,
		},
		{
			name: "no intersection",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
			want: nil,
		},
		{
			name: "full intersection",
			a:    []string{"a", "b", "c"},
			b:    []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "partial intersection",
			a:    []string{"a", "b", "c"},
			b:    []string{"b", "c", "d"},
			want: []string{"b", "c"},
		},
		{
			name: "single element intersection",
			a:    []string{"a", "b", "c"},
			b:    []string{"b"},
			want: []string{"b"},
		},
		{
			name: "duplicates in first slice",
			a:    []string{"a", "a", "b"},
			b:    []string{"a", "b"},
			want: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intersect(tt.a, tt.b)

			if len(got) != len(tt.want) {
				t.Errorf("intersect() length = %v, want %v", len(got), len(tt.want))
				return
			}

			// Check that all elements in got are in want
			for _, g := range got {
				found := false
				for _, w := range tt.want {
					if g == w {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("intersect() contains unexpected element: %v", g)
				}
			}

			// Check that all elements in want are in got
			for _, w := range tt.want {
				found := false
				for _, g := range got {
					if w == g {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("intersect() missing expected element: %v", w)
				}
			}
		})
	}
}

func TestRelaysHandler_ContentType(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relays", nil)
	w := httptest.NewRecorder()

	server.RelaysHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("RelaysHandler() Content-Type = %v, want application/json", contentType)
	}
}

func TestNew(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to create cache client: %v", err)
	}
	defer cacheClient.Close()

	cfg := &config.Config{
		Port:     8080,
		LogLevel: "info",
	}

	server := New(cfg, cacheClient)

	if server == nil {
		t.Fatal("New() returned nil server")
	}

	if server.cfg != cfg {
		t.Error("New() did not set config properly")
	}

	if server.cache != cacheClient {
		t.Error("New() did not set cache client properly")
	}
}
