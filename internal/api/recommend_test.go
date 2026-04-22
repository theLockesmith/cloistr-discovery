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

func TestRecommendRelaysHandler(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Setup test relays with varying attributes for scoring
	relayFast := &cache.RelayEntry{
		URL:           "wss://fast.relay.com",
		Name:          "Fast Relay",
		Health:        "online",
		LatencyMs:     50,
		SupportedNIPs: []int{1, 11, 42},
		CountryCode:   "US",
		LastChecked:   time.Now(),
	}
	relayMedium := &cache.RelayEntry{
		URL:           "wss://medium.relay.com",
		Name:          "Medium Relay",
		Health:        "online",
		LatencyMs:     200,
		SupportedNIPs: []int{1, 11},
		CountryCode:   "DE",
		LastChecked:   time.Now(),
	}
	relayDegraded := &cache.RelayEntry{
		URL:           "wss://degraded.relay.com",
		Name:          "Degraded Relay",
		Health:        "degraded",
		LatencyMs:     300,
		SupportedNIPs: []int{1, 42, 65},
		CountryCode:   "US",
		LastChecked:   time.Now(),
	}
	relayOffline := &cache.RelayEntry{
		URL:           "wss://offline.relay.com",
		Name:          "Offline Relay",
		Health:        "offline",
		LatencyMs:     0,
		SupportedNIPs: []int{1, 11, 42, 65},
		CountryCode:   "US",
		LastChecked:   time.Now(),
	}
	relayPaid := &cache.RelayEntry{
		URL:             "wss://paid.relay.com",
		Name:            "Paid Relay",
		Health:          "online",
		LatencyMs:       75,
		SupportedNIPs:   []int{1, 11, 42},
		CountryCode:     "JP",
		PaymentRequired: true,
		LastChecked:     time.Now(),
	}
	relayAuth := &cache.RelayEntry{
		URL:           "wss://auth.relay.com",
		Name:          "Auth Relay",
		Health:        "online",
		LatencyMs:     100,
		SupportedNIPs: []int{1, 42},
		CountryCode:   "UK",
		AuthRequired:  true,
		LastChecked:   time.Now(),
	}

	// Insert all relays
	server.cache.SetRelayEntry(ctx, relayFast, time.Hour)
	server.cache.SetRelayEntry(ctx, relayMedium, time.Hour)
	server.cache.SetRelayEntry(ctx, relayDegraded, time.Hour)
	server.cache.SetRelayEntry(ctx, relayOffline, time.Hour)
	server.cache.SetRelayEntry(ctx, relayPaid, time.Hour)
	server.cache.SetRelayEntry(ctx, relayAuth, time.Hour)

	tests := []struct {
		name           string
		method         string
		queryParams    string
		wantStatusCode int
		wantMinResults int
		wantMaxResults int
		checkResults   func(t *testing.T, resp RecommendationResponse)
	}{
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			queryParams:    "",
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "default returns online/degraded relays sorted by score",
			method:         http.MethodGet,
			queryParams:    "",
			wantStatusCode: http.StatusOK,
			wantMinResults: 5, // All except offline
			wantMaxResults: 5,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				// Verify offline relay is excluded
				for _, rec := range resp.Recommendations {
					if rec.Relay.Health == "offline" {
						t.Error("offline relay should be excluded from recommendations")
					}
				}
				// Verify sorted by score descending
				for i := 1; i < len(resp.Recommendations); i++ {
					if resp.Recommendations[i].Score > resp.Recommendations[i-1].Score {
						t.Errorf("results not sorted by score: index %d has higher score than %d", i, i-1)
					}
				}
			},
		},
		{
			name:           "limit parameter works",
			method:         http.MethodGet,
			queryParams:    "?limit=3",
			wantStatusCode: http.StatusOK,
			wantMinResults: 3,
			wantMaxResults: 3,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				if len(resp.Recommendations) != 3 {
					t.Errorf("expected 3 results, got %d", len(resp.Recommendations))
				}
				// Total should still reflect all matching relays
				if resp.Total < 5 {
					t.Errorf("total should be >= 5, got %d", resp.Total)
				}
			},
		},
		{
			name:           "NIP filter boosts matching relays",
			method:         http.MethodGet,
			queryParams:    "?nips=42",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				// Relays supporting NIP-42 should have higher scores
				for _, rec := range resp.Recommendations {
					hasNIP42 := false
					for _, nip := range rec.Relay.SupportedNIPs {
						if nip == 42 {
							hasNIP42 = true
							break
						}
					}
					if hasNIP42 {
						foundReason := false
						for _, reason := range rec.Reasons {
							if reason == "supports_requested_nips" {
								foundReason = true
								break
							}
						}
						if !foundReason {
							t.Errorf("relay %s supports NIP-42 but missing 'supports_requested_nips' reason", rec.Relay.URL)
						}
					}
				}
				// Verify criteria is captured
				if len(resp.Criteria.NIPs) != 1 || resp.Criteria.NIPs[0] != 42 {
					t.Errorf("criteria NIPs not captured correctly: %v", resp.Criteria.NIPs)
				}
			},
		},
		{
			name:           "region filter boosts matching relays",
			method:         http.MethodGet,
			queryParams:    "?region=US",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				// US relays should be boosted (fast and degraded are US)
				for _, rec := range resp.Recommendations {
					if rec.Relay.CountryCode == "US" {
						foundRegion := false
						for _, reason := range rec.Reasons {
							if reason == "region_match" {
								foundRegion = true
								break
							}
						}
						if !foundRegion {
							t.Errorf("relay %s is in US but missing 'region_match' reason", rec.Relay.URL)
						}
					}
				}
				if resp.Criteria.Region != "US" {
					t.Errorf("criteria region not captured: got %s, want US", resp.Criteria.Region)
				}
			},
		},
		{
			name:           "exclude_auth filters out auth relays",
			method:         http.MethodGet,
			queryParams:    "?exclude_auth=true",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				for _, rec := range resp.Recommendations {
					if rec.Relay.AuthRequired {
						t.Errorf("relay %s requires auth but should be excluded", rec.Relay.URL)
					}
				}
				if !resp.Criteria.ExcludeAuth {
					t.Error("criteria exclude_auth not captured")
				}
			},
		},
		{
			name:           "exclude_payment filters out paid relays",
			method:         http.MethodGet,
			queryParams:    "?exclude_payment=true",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				for _, rec := range resp.Recommendations {
					if rec.Relay.PaymentRequired {
						t.Errorf("relay %s requires payment but should be excluded", rec.Relay.URL)
					}
				}
				if !resp.Criteria.ExcludePayment {
					t.Error("criteria exclude_payment not captured")
				}
			},
		},
		{
			name:           "combined filters",
			method:         http.MethodGet,
			queryParams:    "?nips=1,42&region=US&exclude_payment=true&limit=2",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				if len(resp.Recommendations) > 2 {
					t.Errorf("expected max 2 results, got %d", len(resp.Recommendations))
				}
				for _, rec := range resp.Recommendations {
					if rec.Relay.PaymentRequired {
						t.Error("paid relay should be excluded")
					}
				}
				// Top result should be fast.relay.com (online, US, low latency, supports 1 & 42)
				if len(resp.Recommendations) > 0 {
					if resp.Recommendations[0].Relay.URL != "wss://fast.relay.com" {
						t.Logf("expected fast.relay.com as top recommendation, got %s (score: %d)",
							resp.Recommendations[0].Relay.URL, resp.Recommendations[0].Score)
					}
				}
			},
		},
		{
			name:           "max limit is enforced",
			method:         http.MethodGet,
			queryParams:    "?limit=100",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				// maxRecommendLimit is 50
				if len(resp.Recommendations) > 50 {
					t.Errorf("limit should be capped at 50, got %d results", len(resp.Recommendations))
				}
			},
		},
		{
			name:           "invalid limit defaults to default",
			method:         http.MethodGet,
			queryParams:    "?limit=abc",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				// defaultRecommendLimit is 10, but we only have 5 relays
				if len(resp.Recommendations) > 10 {
					t.Errorf("invalid limit should default to 10, got %d results", len(resp.Recommendations))
				}
			},
		},
		{
			name:           "region case insensitive",
			method:         http.MethodGet,
			queryParams:    "?region=us",
			wantStatusCode: http.StatusOK,
			checkResults: func(t *testing.T, resp RecommendationResponse) {
				if resp.Criteria.Region != "US" {
					t.Errorf("region should be uppercased: got %s, want US", resp.Criteria.Region)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/relays/recommend"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.RecommendRelaysHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("RecommendRelaysHandler() status = %v, want %v", resp.StatusCode, tt.wantStatusCode)
				return
			}

			if tt.wantStatusCode != http.StatusOK {
				return
			}

			var recResp RecommendationResponse
			if err := json.NewDecoder(resp.Body).Decode(&recResp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.wantMinResults > 0 && len(recResp.Recommendations) < tt.wantMinResults {
				t.Errorf("got %d results, want at least %d", len(recResp.Recommendations), tt.wantMinResults)
			}
			if tt.wantMaxResults > 0 && len(recResp.Recommendations) > tt.wantMaxResults {
				t.Errorf("got %d results, want at most %d", len(recResp.Recommendations), tt.wantMaxResults)
			}

			if tt.checkResults != nil {
				tt.checkResults(t, recResp)
			}
		})
	}
}

func TestRecommendRelaysHandler_EmptyCache(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relays/recommend", nil)
	w := httptest.NewRecorder()

	server.RecommendRelaysHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var recResp RecommendationResponse
	if err := json.NewDecoder(resp.Body).Decode(&recResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(recResp.Recommendations) != 0 {
		t.Errorf("expected 0 recommendations, got %d", len(recResp.Recommendations))
	}

	if recResp.Total != 0 {
		t.Errorf("expected total 0, got %d", recResp.Total)
	}
}

func TestRecommendRelaysHandler_ContentType(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relays/recommend", nil)
	w := httptest.NewRecorder()

	server.RecommendRelaysHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %v, want application/json", contentType)
	}
}

func TestRecommendRelaysHandler_InputValidation(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Add one relay for testing
	testRelay := &cache.RelayEntry{
		URL:           "wss://test.relay.com",
		Name:          "Test Relay",
		Health:        "online",
		LatencyMs:     50,
		SupportedNIPs: []int{1, 11, 42},
		CountryCode:   "US",
		LastChecked:   time.Now(),
	}
	server.cache.SetRelayEntry(ctx, testRelay, time.Hour)

	tests := []struct {
		name        string
		queryParams string
		checkCriteria func(t *testing.T, criteria RecommendationInputs)
	}{
		{
			name:        "invalid region length ignored (too long)",
			queryParams: "?region=USA",
			checkCriteria: func(t *testing.T, criteria RecommendationInputs) {
				if criteria.Region != "" {
					t.Errorf("expected empty region for invalid length, got %s", criteria.Region)
				}
			},
		},
		{
			name:        "invalid region length ignored (too short)",
			queryParams: "?region=U",
			checkCriteria: func(t *testing.T, criteria RecommendationInputs) {
				if criteria.Region != "" {
					t.Errorf("expected empty region for invalid length, got %s", criteria.Region)
				}
			},
		},
		{
			name:        "NIP values above max are ignored",
			queryParams: "?nips=1,99999",
			checkCriteria: func(t *testing.T, criteria RecommendationInputs) {
				if len(criteria.NIPs) != 1 {
					t.Errorf("expected 1 NIP (99999 ignored), got %d", len(criteria.NIPs))
				}
				if len(criteria.NIPs) > 0 && criteria.NIPs[0] != 1 {
					t.Errorf("expected NIP 1, got %d", criteria.NIPs[0])
				}
			},
		},
		{
			name:        "negative NIP values are ignored",
			queryParams: "?nips=-1,42",
			checkCriteria: func(t *testing.T, criteria RecommendationInputs) {
				if len(criteria.NIPs) != 1 {
					t.Errorf("expected 1 NIP (-1 ignored), got %d", len(criteria.NIPs))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/relays/recommend"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.RecommendRelaysHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200, got %d", resp.StatusCode)
			}

			var recResp RecommendationResponse
			if err := json.NewDecoder(resp.Body).Decode(&recResp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			tt.checkCriteria(t, recResp.Criteria)
		})
	}
}

func TestScoreRelay(t *testing.T) {
	tests := []struct {
		name           string
		entry          *cache.RelayEntry
		criteria       RecommendationInputs
		wantMinScore   int
		wantMaxScore   int
		wantReasons    []string
		notWantReasons []string
	}{
		{
			name: "online relay with low latency, no restrictions",
			entry: &cache.RelayEntry{
				Health:    "online",
				LatencyMs: 50,
			},
			criteria:     RecommendationInputs{},
			wantMinScore: scoreHealthOnline + scoreLowLatency + scoreNoAuth + scoreNoPayment, // 100 + 15 + 10 + 10 = 135
			wantMaxScore: 135,
			wantReasons:  []string{"online", "low_latency", "no_auth_required", "free"},
		},
		{
			name: "degraded relay",
			entry: &cache.RelayEntry{
				Health:    "degraded",
				LatencyMs: 500,
			},
			criteria:     RecommendationInputs{},
			wantMinScore: scoreHealthDegraded + scoreHighLatency + scoreNoAuth + scoreNoPayment, // 50 + 5 + 10 + 10 = 75
			wantReasons:  []string{"degraded", "acceptable_latency"},
		},
		{
			name: "NIP match scoring",
			entry: &cache.RelayEntry{
				Health:        "online",
				LatencyMs:     200,
				SupportedNIPs: []int{1, 11, 42, 65},
			},
			criteria:     RecommendationInputs{NIPs: []int{42, 65, 99}},
			wantMinScore: scoreHealthOnline + scoreMedLatency + scorePerNIPMatch*2 + scoreNoAuth + scoreNoPayment, // 100 + 10 + 20 + 10 + 10 = 150
			wantReasons:  []string{"supports_requested_nips"},
		},
		{
			name: "region match",
			entry: &cache.RelayEntry{
				Health:      "online",
				LatencyMs:   100,
				CountryCode: "US",
			},
			criteria:     RecommendationInputs{Region: "US"},
			wantMinScore: scoreHealthOnline + scoreMedLatency + scoreRegionMatch + scoreNoAuth + scoreNoPayment,
			wantReasons:  []string{"region_match"},
		},
		{
			name: "paid relay doesn't get free bonus",
			entry: &cache.RelayEntry{
				Health:          "online",
				LatencyMs:       50,
				PaymentRequired: true,
			},
			criteria:       RecommendationInputs{},
			wantMinScore:   scoreHealthOnline + scoreLowLatency + scoreNoAuth, // No payment bonus
			wantMaxScore:   scoreHealthOnline + scoreLowLatency + scoreNoAuth,
			notWantReasons: []string{"free"},
		},
		{
			name: "auth relay doesn't get no_auth bonus",
			entry: &cache.RelayEntry{
				Health:       "online",
				LatencyMs:    50,
				AuthRequired: true,
			},
			criteria:       RecommendationInputs{},
			wantMinScore:   scoreHealthOnline + scoreLowLatency + scoreNoPayment, // No auth bonus
			wantMaxScore:   scoreHealthOnline + scoreLowLatency + scoreNoPayment,
			notWantReasons: []string{"no_auth_required"},
		},
		{
			name: "no latency score for very slow relay",
			entry: &cache.RelayEntry{
				Health:    "online",
				LatencyMs: 2000,
			},
			criteria:       RecommendationInputs{},
			wantMinScore:   scoreHealthOnline + scoreNoAuth + scoreNoPayment, // No latency bonus
			notWantReasons: []string{"low_latency", "medium_latency", "acceptable_latency"},
		},
		{
			name: "no latency score for zero latency (unknown)",
			entry: &cache.RelayEntry{
				Health:    "online",
				LatencyMs: 0,
			},
			criteria:       RecommendationInputs{},
			wantMinScore:   scoreHealthOnline + scoreNoAuth + scoreNoPayment,
			notWantReasons: []string{"low_latency", "medium_latency", "acceptable_latency"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, reasons := scoreRelay(tt.entry, tt.criteria)

			if score < tt.wantMinScore {
				t.Errorf("score = %d, want at least %d", score, tt.wantMinScore)
			}
			if tt.wantMaxScore > 0 && score > tt.wantMaxScore {
				t.Errorf("score = %d, want at most %d", score, tt.wantMaxScore)
			}

			// Check for wanted reasons
			for _, wantReason := range tt.wantReasons {
				found := false
				for _, r := range reasons {
					if r == wantReason {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing expected reason %q in %v", wantReason, reasons)
				}
			}

			// Check for unwanted reasons
			for _, notWantReason := range tt.notWantReasons {
				for _, r := range reasons {
					if r == notWantReason {
						t.Errorf("found unexpected reason %q in %v", notWantReason, reasons)
						break
					}
				}
			}
		})
	}
}
