package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestCompareRelaysHandler(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Setup test relays
	relay1 := &cache.RelayEntry{
		URL:             "wss://relay1.example.com",
		Name:            "Relay 1",
		Health:          "online",
		LatencyMs:       50,
		SupportedNIPs:   []int{1, 11, 42, 65},
		CountryCode:     "US",
		PaymentRequired: false,
		AuthRequired:    false,
		LastChecked:     time.Now(),
	}
	relay2 := &cache.RelayEntry{
		URL:             "wss://relay2.example.com",
		Name:            "Relay 2",
		Health:          "online",
		LatencyMs:       150,
		SupportedNIPs:   []int{1, 11, 50, 96},
		CountryCode:     "DE",
		PaymentRequired: true,
		AuthRequired:    false,
		LastChecked:     time.Now(),
	}
	relay3 := &cache.RelayEntry{
		URL:             "wss://relay3.example.com",
		Name:            "Relay 3",
		Health:          "degraded",
		LatencyMs:       300,
		SupportedNIPs:   []int{1, 42},
		CountryCode:     "JP",
		PaymentRequired: false,
		AuthRequired:    true,
		LastChecked:     time.Now(),
	}

	server.cache.SetRelayEntry(ctx, relay1, time.Hour)
	server.cache.SetRelayEntry(ctx, relay2, time.Hour)
	server.cache.SetRelayEntry(ctx, relay3, time.Hour)

	tests := []struct {
		name           string
		method         string
		queryParams    string
		wantStatusCode int
		wantError      string
		checkResponse  func(t *testing.T, resp CompareResponse)
	}{
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			queryParams:    "?urls=wss://relay1.example.com,wss://relay2.example.com",
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "missing urls parameter",
			method:         http.MethodGet,
			queryParams:    "",
			wantStatusCode: http.StatusBadRequest,
			wantError:      "urls parameter required",
		},
		{
			name:           "only one relay",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com",
			wantStatusCode: http.StatusBadRequest,
			wantError:      "at least 2 relay URLs required",
		},
		{
			name:           "compare two relays",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,wss://relay2.example.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				if len(resp.Relays) != 2 {
					t.Errorf("expected 2 relays, got %d", len(resp.Relays))
				}
				// Both should be found
				for _, r := range resp.Relays {
					if !r.Found {
						t.Errorf("relay %s should be found", r.URL)
					}
				}
				// Common NIPs should be 1 and 11
				if len(resp.Comparison.CommonNIPs) != 2 {
					t.Errorf("expected 2 common NIPs, got %d: %v", len(resp.Comparison.CommonNIPs), resp.Comparison.CommonNIPs)
				}
				// Fastest should be relay1 (50ms)
				if resp.Comparison.FastestRelay != "wss://relay1.example.com" {
					t.Errorf("expected relay1 as fastest, got %s", resp.Comparison.FastestRelay)
				}
			},
		},
		{
			name:           "compare three relays",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,wss://relay2.example.com,wss://relay3.example.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				if len(resp.Relays) != 3 {
					t.Errorf("expected 3 relays, got %d", len(resp.Relays))
				}
				// Only NIP 1 is common to all three
				if len(resp.Comparison.CommonNIPs) != 1 || resp.Comparison.CommonNIPs[0] != 1 {
					t.Errorf("expected only NIP 1 as common, got %v", resp.Comparison.CommonNIPs)
				}
				// Health summary should have online: 2, degraded: 1
				if resp.Comparison.HealthySummary["online"] != 2 {
					t.Errorf("expected 2 online relays, got %d", resp.Comparison.HealthySummary["online"])
				}
				if resp.Comparison.HealthySummary["degraded"] != 1 {
					t.Errorf("expected 1 degraded relay, got %d", resp.Comparison.HealthySummary["degraded"])
				}
			},
		},
		{
			name:           "includes non-existent relay",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,wss://nonexistent.relay.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				if len(resp.Relays) != 2 {
					t.Errorf("expected 2 relays, got %d", len(resp.Relays))
				}
				// First should be found
				if !resp.Relays[0].Found {
					t.Error("relay1 should be found")
				}
				// Second should not be found
				if resp.Relays[1].Found {
					t.Error("nonexistent relay should not be found")
				}
				if resp.Relays[1].Relay != nil {
					t.Error("nonexistent relay should have nil Relay")
				}
			},
		},
		{
			name:           "deduplicates URLs",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,wss://relay1.example.com,wss://relay2.example.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				if len(resp.Relays) != 2 {
					t.Errorf("expected 2 relays after dedup, got %d", len(resp.Relays))
				}
			},
		},
		{
			name:           "filters invalid URLs",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,https://invalid.com,wss://relay2.example.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				if len(resp.Relays) != 2 {
					t.Errorf("expected 2 relays (invalid filtered), got %d", len(resp.Relays))
				}
			},
		},
		{
			name:           "checks feature extraction",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,wss://relay2.example.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				// Relay 1 features
				r1 := resp.Relays[0]
				if r1.Features == nil {
					t.Fatal("relay1 should have features")
				}
				if r1.Features.NIPCount != 4 {
					t.Errorf("relay1 NIPCount = %d, want 4", r1.Features.NIPCount)
				}
				if !r1.Features.HasNIP42Auth {
					t.Error("relay1 should have NIP-42")
				}
				if !r1.Features.HasNIP65Lists {
					t.Error("relay1 should have NIP-65")
				}
				if r1.Features.LatencyCategory != "fast" {
					t.Errorf("relay1 latency = %s, want fast", r1.Features.LatencyCategory)
				}
				if r1.Features.AccessType != "free" {
					t.Errorf("relay1 access = %s, want free", r1.Features.AccessType)
				}

				// Relay 2 features
				r2 := resp.Relays[1]
				if r2.Features == nil {
					t.Fatal("relay2 should have features")
				}
				if !r2.Features.HasNIP50Search {
					t.Error("relay2 should have NIP-50")
				}
				if !r2.Features.HasNIP96Media {
					t.Error("relay2 should have NIP-96")
				}
				if r2.Features.LatencyCategory != "medium" {
					t.Errorf("relay2 latency = %s, want medium", r2.Features.LatencyCategory)
				}
				if r2.Features.AccessType != "paid" {
					t.Errorf("relay2 access = %s, want paid", r2.Features.AccessType)
				}
			},
		},
		{
			name:           "NIP coverage includes all relays",
			method:         http.MethodGet,
			queryParams:    "?urls=wss://relay1.example.com,wss://relay2.example.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp CompareResponse) {
				// NIP 42 should only be supported by relay1
				nip42Relays := resp.Comparison.NIPCoverage[42]
				if len(nip42Relays) != 1 {
					t.Errorf("NIP 42 coverage = %v, want 1 relay", nip42Relays)
				}
				// NIP 1 should be supported by both
				nip1Relays := resp.Comparison.NIPCoverage[1]
				if len(nip1Relays) != 2 {
					t.Errorf("NIP 1 coverage = %v, want 2 relays", nip1Relays)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/relays/compare"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.CompareRelaysHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("status = %v, want %v", resp.StatusCode, tt.wantStatusCode)
				return
			}

			if tt.wantStatusCode == http.StatusMethodNotAllowed {
				return
			}

			var compareResp CompareResponse
			if err := json.NewDecoder(resp.Body).Decode(&compareResp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.wantError != "" {
				if compareResp.Error != tt.wantError {
					t.Errorf("error = %q, want %q", compareResp.Error, tt.wantError)
				}
				return
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, compareResp)
			}
		})
	}
}

func TestCompareRelaysHandler_ContentType(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relays/compare?urls=wss://a.com,wss://b.com", nil)
	w := httptest.NewRecorder()

	server.CompareRelaysHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %v, want application/json", contentType)
	}
}

func TestParseRelayURLs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "valid URLs",
			input: "wss://relay1.com,wss://relay2.com",
			want:  []string{"wss://relay1.com", "wss://relay2.com"},
		},
		{
			name:  "with spaces",
			input: "wss://relay1.com, wss://relay2.com , wss://relay3.com",
			want:  []string{"wss://relay1.com", "wss://relay2.com", "wss://relay3.com"},
		},
		{
			name:  "filters invalid protocols",
			input: "wss://valid.com,https://invalid.com,ws://also-valid.com",
			want:  []string{"wss://valid.com", "ws://also-valid.com"},
		},
		{
			name:  "deduplicates",
			input: "wss://relay.com,wss://relay.com,wss://relay.com",
			want:  []string{"wss://relay.com"},
		},
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
		{
			name:  "all invalid",
			input: "https://a.com,http://b.com",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRelayURLs(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseRelayURLs() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseRelayURLs()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractFeatures(t *testing.T) {
	tests := []struct {
		name  string
		entry *cache.RelayEntry
		check func(t *testing.T, f *RelayFeatureSummary)
	}{
		{
			name: "all NIPs present",
			entry: &cache.RelayEntry{
				SupportedNIPs: []int{42, 50, 65, 96},
				LatencyMs:     50,
			},
			check: func(t *testing.T, f *RelayFeatureSummary) {
				if !f.HasNIP42Auth {
					t.Error("expected HasNIP42Auth")
				}
				if !f.HasNIP50Search {
					t.Error("expected HasNIP50Search")
				}
				if !f.HasNIP65Lists {
					t.Error("expected HasNIP65Lists")
				}
				if !f.HasNIP96Media {
					t.Error("expected HasNIP96Media")
				}
			},
		},
		{
			name: "latency categories",
			entry: &cache.RelayEntry{
				LatencyMs: 600,
			},
			check: func(t *testing.T, f *RelayFeatureSummary) {
				if f.LatencyCategory != "slow" {
					t.Errorf("expected slow, got %s", f.LatencyCategory)
				}
			},
		},
		{
			name: "unknown latency",
			entry: &cache.RelayEntry{
				LatencyMs: 0,
			},
			check: func(t *testing.T, f *RelayFeatureSummary) {
				if f.LatencyCategory != "unknown" {
					t.Errorf("expected unknown, got %s", f.LatencyCategory)
				}
			},
		},
		{
			name: "paid+auth access",
			entry: &cache.RelayEntry{
				PaymentRequired: true,
				AuthRequired:    true,
			},
			check: func(t *testing.T, f *RelayFeatureSummary) {
				if f.AccessType != "paid+auth" {
					t.Errorf("expected paid+auth, got %s", f.AccessType)
				}
			},
		},
		{
			name: "auth-required access",
			entry: &cache.RelayEntry{
				AuthRequired: true,
			},
			check: func(t *testing.T, f *RelayFeatureSummary) {
				if f.AccessType != "auth-required" {
					t.Errorf("expected auth-required, got %s", f.AccessType)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features := extractFeatures(tt.entry)
			tt.check(t, features)
		})
	}
}
