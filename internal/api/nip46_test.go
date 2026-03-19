package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
)

func setupNIP46TestServer(t *testing.T) (*Server, *cache.Client, *miniredis.Miniredis) {
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

	return server, cacheClient, mr
}

func TestNIP46ScoreHandler_NoURL(t *testing.T) {
	server, _, mr := setupNIP46TestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Error == "" {
		t.Error("expected error message for missing URL")
	}
}

func TestNIP46ScoreHandler_InvalidURL(t *testing.T) {
	server, _, mr := setupNIP46TestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=https://not-websocket.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Error == "" {
		t.Error("expected error for invalid URL scheme")
	}
}

func TestNIP46ScoreHandler_RelayNotFound(t *testing.T) {
	server, _, mr := setupNIP46TestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://unknown.relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, resp.StatusCode)
	}

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Score != 0 {
		t.Errorf("expected score 0 for unknown relay, got %d", result.Score)
	}
	if result.Recommendation != "unknown" {
		t.Errorf("expected recommendation 'unknown', got %s", result.Recommendation)
	}
}

func TestNIP46ScoreHandler_NoNIP46Support(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Relay without NIP-46 (like damus.io)
	entry := &cache.RelayEntry{
		URL:           "wss://relay.damus.io",
		Name:          "damus.io",
		Health:        "online",
		SupportedNIPs: []int{1, 2, 4, 9, 11, 22, 28, 40, 70, 77}, // No NIP-46
		LatencyMs:     100,
		LastChecked:   time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://relay.damus.io", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Score != 0 {
		t.Errorf("expected score 0 for relay without NIP-46, got %d", result.Score)
	}
	if result.Recommendation != "avoid" {
		t.Errorf("expected recommendation 'avoid', got %s", result.Recommendation)
	}
	if len(result.Reasons) == 0 || result.Reasons[0] != "relay does not advertise NIP-46 support" {
		t.Errorf("expected NIP-46 missing reason, got %v", result.Reasons)
	}
}

func TestNIP46ScoreHandler_WithNIP46Support(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Relay with NIP-46 support (like cloistr relay)
	entry := &cache.RelayEntry{
		URL:           "wss://relay.cloistr.xyz",
		Name:          "Cloistr Relay",
		Health:        "online",
		SupportedNIPs: []int{1, 9, 11, 13, 22, 33, 40, 42, 45, 46, 50, 57, 59, 66, 70, 77, 86, 94},
		LatencyMs:     50,
		LastChecked:   time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://relay.cloistr.xyz", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Score < 80 {
		t.Errorf("expected high score for NIP-46 capable relay, got %d", result.Score)
	}
	if result.Recommendation != "recommended" {
		t.Errorf("expected recommendation 'recommended', got %s", result.Recommendation)
	}
}

func TestNIP46ScoreHandler_OfflineRelay(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	entry := &cache.RelayEntry{
		URL:           "wss://offline.relay.com",
		Name:          "Offline Relay",
		Health:        "offline",
		SupportedNIPs: []int{1, 46},
		LatencyMs:     0,
		LastChecked:   time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://offline.relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Score != 0 {
		t.Errorf("expected score 0 for offline relay, got %d", result.Score)
	}
	if result.Recommendation != "avoid" {
		t.Errorf("expected recommendation 'avoid', got %s", result.Recommendation)
	}
}

func TestNIP46ScoreHandler_DegradedRelay(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	entry := &cache.RelayEntry{
		URL:           "wss://degraded.relay.com",
		Name:          "Degraded Relay",
		Health:        "degraded",
		SupportedNIPs: []int{1, 46},
		LatencyMs:     100,
		LastChecked:   time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://degraded.relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// Should have 100 - 20 (degraded) = 80
	if result.Score != 80 {
		t.Errorf("expected score 80 for degraded relay with NIP-46, got %d", result.Score)
	}
	if result.Recommendation != "recommended" {
		t.Errorf("expected recommendation 'recommended' (score=80), got %s", result.Recommendation)
	}
}

func TestNIP46ScoreHandler_HighLatencyRelay(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	entry := &cache.RelayEntry{
		URL:           "wss://slow.relay.com",
		Name:          "Slow Relay",
		Health:        "online",
		SupportedNIPs: []int{1, 46},
		LatencyMs:     1500, // Very high latency
		LastChecked:   time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://slow.relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// Should have 100 - 30 (latency > 1000ms) = 70
	if result.Score != 70 {
		t.Errorf("expected score 70 for high latency relay, got %d", result.Score)
	}
	if result.Recommendation != "acceptable" {
		t.Errorf("expected recommendation 'acceptable' (score=70), got %s", result.Recommendation)
	}
}

func TestNIP46ScoreHandler_PaymentRequired(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	entry := &cache.RelayEntry{
		URL:             "wss://paid.relay.com",
		Name:            "Paid Relay",
		Health:          "online",
		SupportedNIPs:   []int{1, 46},
		LatencyMs:       50,
		PaymentRequired: true,
		LastChecked:     time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://paid.relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// Should have 100 - 15 (payment) = 85
	if result.Score != 85 {
		t.Errorf("expected score 85 for payment-required relay, got %d", result.Score)
	}
}

func TestNIP46ScoreHandler_AuthRequired(t *testing.T) {
	server, cacheClient, mr := setupNIP46TestServer(t)
	defer mr.Close()

	ctx := context.Background()

	entry := &cache.RelayEntry{
		URL:           "wss://auth.relay.com",
		Name:          "Auth Relay",
		Health:        "online",
		SupportedNIPs: []int{1, 42, 46},
		LatencyMs:     50,
		AuthRequired:  true,
		LastChecked:   time.Now(),
	}
	cacheClient.SetRelayEntry(ctx, entry, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/nip46-score?url=wss://auth.relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var result NIP46ScoreResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// Should have 100 - 5 (auth) = 95
	if result.Score != 95 {
		t.Errorf("expected score 95 for auth-required relay, got %d", result.Score)
	}
}

func TestNIP46ScoreHandler_MethodNotAllowed(t *testing.T) {
	server, _, mr := setupNIP46TestServer(t)
	defer mr.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/relay/nip46-score?url=wss://relay.com", nil)
	w := httptest.NewRecorder()

	server.NIP46ScoreHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d for POST, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

func TestCalculateNIP46Score(t *testing.T) {
	tests := []struct {
		name       string
		entry      *cache.RelayEntry
		wantScore  int
		wantReason string
	}{
		{
			name: "perfect relay",
			entry: &cache.RelayEntry{
				URL:           "wss://perfect.relay.com",
				Health:        "online",
				SupportedNIPs: []int{1, 46},
				LatencyMs:     50,
			},
			wantScore:  100,
			wantReason: "relay advertises NIP-46 support",
		},
		{
			name: "no NIP-46",
			entry: &cache.RelayEntry{
				URL:           "wss://no-nip46.relay.com",
				Health:        "online",
				SupportedNIPs: []int{1, 11},
				LatencyMs:     50,
			},
			wantScore:  0,
			wantReason: "relay does not advertise NIP-46 support",
		},
		{
			name: "offline",
			entry: &cache.RelayEntry{
				URL:           "wss://offline.relay.com",
				Health:        "offline",
				SupportedNIPs: []int{1, 46},
			},
			wantScore:  0,
			wantReason: "relay is offline",
		},
		{
			name: "all penalties",
			entry: &cache.RelayEntry{
				URL:             "wss://bad.relay.com",
				Health:          "degraded",
				SupportedNIPs:   []int{1, 46},
				LatencyMs:       1500,
				PaymentRequired: true,
				AuthRequired:    true,
			},
			wantScore: 30, // 100 - 20 (degraded) - 30 (latency) - 15 (payment) - 5 (auth)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, reasons := calculateNIP46Score(tt.entry)
			if score != tt.wantScore {
				t.Errorf("calculateNIP46Score() score = %d, want %d (reasons: %v)", score, tt.wantScore, reasons)
			}
			if tt.wantReason != "" {
				found := false
				for _, r := range reasons {
					if r == tt.wantReason {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("calculateNIP46Score() reasons = %v, want to contain %q", reasons, tt.wantReason)
				}
			}
		})
	}
}

func TestScoreToRecommendation(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{100, "recommended"},
		{80, "recommended"},
		{79, "acceptable"},
		{50, "acceptable"},
		{49, "warning"},
		{20, "warning"},
		{19, "avoid"},
		{0, "avoid"},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.score)), func(t *testing.T) {
			got := scoreToRecommendation(tt.score)
			if got != tt.want {
				t.Errorf("scoreToRecommendation(%d) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}
