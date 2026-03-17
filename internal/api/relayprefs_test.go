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
	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
)

func TestRelayPrefsHandler_InvalidPubkey(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	tests := []struct {
		name    string
		pubkey  string
		wantErr string
	}{
		{
			name:    "empty pubkey",
			pubkey:  "",
			wantErr: "pubkey is required",
		},
		{
			name:    "short pubkey",
			pubkey:  "abc123",
			wantErr: "pubkey must be 64 hex characters",
		},
		{
			name:    "invalid hex",
			pubkey:  "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg",
			wantErr: "pubkey must be valid hex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+tt.pubkey, nil)
			w := httptest.NewRecorder()

			server.RelayPrefsHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", resp.StatusCode)
			}

			var errResp map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}

			if errResp["error"] != tt.wantErr {
				t.Errorf("expected error %q, got %q", tt.wantErr, errResp["error"])
			}
		})
	}
}

func TestRelayPrefsHandler_MethodNotAllowed(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	pubkey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	req := httptest.NewRequest(http.MethodPost, "/api/v1/relay-prefs/"+pubkey, nil)
	w := httptest.NewRecorder()

	server.RelayPrefsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestRelayPrefsHandler_CacheHit(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	// Pre-populate cache
	cachedEntry := &cache.RelayPrefsEntry{
		Pubkey:   pubkey,
		Source:   "cloistr-relays",
		CachedAt: time.Now().Add(-1 * time.Minute),
		Relays: []cache.UserRelayData{
			{URL: "wss://relay1.example.com", Read: true, Write: true},
			{URL: "wss://relay2.example.com", Read: true, Write: false},
		},
	}
	if err := server.cache.SetRelayPrefs(ctx, cachedEntry); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+pubkey, nil)
	w := httptest.NewRecorder()

	server.RelayPrefsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var prefsResp RelayPrefsResponse
	if err := json.NewDecoder(resp.Body).Decode(&prefsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if prefsResp.Pubkey != pubkey {
		t.Errorf("expected pubkey %s, got %s", pubkey, prefsResp.Pubkey)
	}
	if prefsResp.Source != "cloistr-relays" {
		t.Errorf("expected source cloistr-relays, got %s", prefsResp.Source)
	}
	if len(prefsResp.Relays) != 2 {
		t.Errorf("expected 2 relays, got %d", len(prefsResp.Relays))
	}
	if prefsResp.Relays[0].URL != "wss://relay1.example.com" {
		t.Errorf("expected first relay URL wss://relay1.example.com, got %s", prefsResp.Relays[0].URL)
	}
	if !prefsResp.Relays[0].Read || !prefsResp.Relays[0].Write {
		t.Error("expected first relay to have read and write")
	}
	if !prefsResp.Relays[1].Read || prefsResp.Relays[1].Write {
		t.Error("expected second relay to have read only")
	}
}

func TestRelayPrefsHandler_DefaultResponse(t *testing.T) {
	// Create a test server with a non-existent relay URL to force default response
	mr := miniredis.RunT(t)
	defer mr.Close()

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

	pubkey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+pubkey, nil)
	w := httptest.NewRecorder()

	// This will fail to connect to relay.cloistr.xyz in tests but should return default
	server.RelayPrefsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var prefsResp RelayPrefsResponse
	if err := json.NewDecoder(resp.Body).Decode(&prefsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if prefsResp.Pubkey != pubkey {
		t.Errorf("expected pubkey %s, got %s", pubkey, prefsResp.Pubkey)
	}
	if prefsResp.Source != "default" {
		t.Errorf("expected source default, got %s", prefsResp.Source)
	}
	if len(prefsResp.Relays) != 0 {
		t.Errorf("expected 0 relays for default, got %d", len(prefsResp.Relays))
	}
}

func TestRelayPrefsHandler_ContentType(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	// Pre-populate cache so we get a response
	cachedEntry := &cache.RelayPrefsEntry{
		Pubkey:   pubkey,
		Source:   "default",
		CachedAt: time.Now(),
		Relays:   nil,
	}
	server.cache.SetRelayPrefs(ctx, cachedEntry)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+pubkey, nil)
	w := httptest.NewRecorder()

	server.RelayPrefsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}
}

func TestRelayPrefsHandler_EmptyRelaysIsArrayNotNull(t *testing.T) {
	// Verify that empty relays returns [] not null in JSON
	// This is important for client compatibility
	mr := miniredis.RunT(t)
	defer mr.Close()

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

	pubkey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay-prefs/"+pubkey, nil)
	w := httptest.NewRecorder()

	server.RelayPrefsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Read raw body to check JSON format
	body := w.Body.String()
	if !strings.Contains(body, `"relays":[]`) {
		t.Errorf("expected JSON to contain \"relays\":[], got: %s", body)
	}
	if strings.Contains(body, `"relays":null`) {
		t.Errorf("JSON should not contain \"relays\":null, got: %s", body)
	}
}

func TestParseRelayTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     nostr.Tags
		expected []RelayPrefsEntry
	}{
		{
			name: "read and write (no marker)",
			tags: nostr.Tags{
				{"r", "wss://relay.example.com"},
			},
			expected: []RelayPrefsEntry{
				{URL: "wss://relay.example.com", Read: true, Write: true},
			},
		},
		{
			name: "read only",
			tags: nostr.Tags{
				{"r", "wss://relay.example.com", "read"},
			},
			expected: []RelayPrefsEntry{
				{URL: "wss://relay.example.com", Read: true, Write: false},
			},
		},
		{
			name: "write only",
			tags: nostr.Tags{
				{"r", "wss://relay.example.com", "write"},
			},
			expected: []RelayPrefsEntry{
				{URL: "wss://relay.example.com", Read: false, Write: true},
			},
		},
		{
			name: "multiple relays mixed",
			tags: nostr.Tags{
				{"r", "wss://relay1.com"},
				{"r", "wss://relay2.com", "read"},
				{"r", "wss://relay3.com", "write"},
				{"d", "cloistr-relays"}, // Should be ignored
				{"p", "somepubkey"},     // Should be ignored
			},
			expected: []RelayPrefsEntry{
				{URL: "wss://relay1.com", Read: true, Write: true},
				{URL: "wss://relay2.com", Read: true, Write: false},
				{URL: "wss://relay3.com", Read: false, Write: true},
			},
		},
		{
			name: "empty URL is skipped",
			tags: nostr.Tags{
				{"r", ""},
				{"r", "wss://valid.com"},
			},
			expected: []RelayPrefsEntry{
				{URL: "wss://valid.com", Read: true, Write: true},
			},
		},
		{
			name:     "empty tags",
			tags:     nostr.Tags{},
			expected: nil,
		},
		{
			name: "malformed tag (too short)",
			tags: nostr.Tags{
				{"r"},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRelayTags(tt.tags)

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d entries, got %d", len(tt.expected), len(result))
			}

			for i, exp := range tt.expected {
				if result[i].URL != exp.URL {
					t.Errorf("entry %d: expected URL %s, got %s", i, exp.URL, result[i].URL)
				}
				if result[i].Read != exp.Read {
					t.Errorf("entry %d: expected Read %v, got %v", i, exp.Read, result[i].Read)
				}
				if result[i].Write != exp.Write {
					t.Errorf("entry %d: expected Write %v, got %v", i, exp.Write, result[i].Write)
				}
			}
		})
	}
}

func TestRelayPrefsCache(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to create cache client: %v", err)
	}

	ctx := context.Background()
	pubkey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	// Test Set
	entry := &cache.RelayPrefsEntry{
		Pubkey:   pubkey,
		Source:   "nip65",
		CachedAt: time.Now(),
		Relays: []cache.UserRelayData{
			{URL: "wss://test.com", Read: true, Write: true},
		},
	}

	if err := cacheClient.SetRelayPrefs(ctx, entry); err != nil {
		t.Fatalf("SetRelayPrefs failed: %v", err)
	}

	// Test Get
	retrieved, err := cacheClient.GetRelayPrefs(ctx, pubkey)
	if err != nil {
		t.Fatalf("GetRelayPrefs failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected non-nil entry from cache")
	}

	if retrieved.Pubkey != pubkey {
		t.Errorf("expected pubkey %s, got %s", pubkey, retrieved.Pubkey)
	}
	if retrieved.Source != "nip65" {
		t.Errorf("expected source nip65, got %s", retrieved.Source)
	}
	if len(retrieved.Relays) != 1 {
		t.Errorf("expected 1 relay, got %d", len(retrieved.Relays))
	}

	// Test cache miss
	missingEntry, err := cacheClient.GetRelayPrefs(ctx, "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetRelayPrefs for missing key should not error: %v", err)
	}
	if missingEntry != nil {
		t.Error("expected nil for missing cache entry")
	}
}
