package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestUserRelaysHandler_MethodNotAllowed(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	tests := []struct {
		name   string
		method string
	}{
		{"POST", http.MethodPost},
		{"PUT", http.MethodPut},
		{"DELETE", http.MethodDelete},
		{"PATCH", http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/users/abc123def456abc123def456abc123def456abc123def456abc123def456abcd/relays", nil)
			w := httptest.NewRecorder()

			server.UserRelaysHandler(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("UserRelaysHandler() %s status = %v, want %v", tt.method, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestUserRelaysHandler_InvalidPubkey(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	tests := []struct {
		name   string
		pubkey string
	}{
		{"empty pubkey", ""},
		{"too short", "abc123"},
		{"too long", "abc123def456abc123def456abc123def456abc123def456abc123def456abcdef12"},
		{"invalid hex", "xyz123def456abc123def456abc123def456abc123def456abc123def456abcd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+tt.pubkey+"/relays", nil)
			w := httptest.NewRecorder()

			server.UserRelaysHandler(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("UserRelaysHandler() invalid pubkey status = %v, want %v", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestUserRelaysHandler_CacheHit(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"

	// Pre-populate cache
	cacheEntry := &cache.UserNIP65Entry{
		Pubkey:    pubkey,
		FetchedAt: time.Now(),
		Relays: []cache.UserRelayData{
			{URL: "wss://relay.damus.io", Read: true, Write: true},
			{URL: "wss://nos.lol", Read: true, Write: false},
		},
	}
	err := server.cache.SetUserNIP65(ctx, cacheEntry)
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+pubkey+"/relays", nil)
	w := httptest.NewRecorder()

	server.UserRelaysHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UserRelaysHandler() status = %v, want %v", w.Code, http.StatusOK)
	}

	var resp UserRelayResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.CacheHit {
		t.Error("Expected cache_hit to be true")
	}

	if resp.Pubkey != pubkey {
		t.Errorf("Pubkey = %v, want %v", resp.Pubkey, pubkey)
	}

	if len(resp.Relays) != 2 {
		t.Errorf("Relays count = %v, want 2", len(resp.Relays))
	}

	// Check Content-Type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %v, want application/json", contentType)
	}
}

func TestUserRelaysHandler_EnrichWithHealth(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "def456abc123def456abc123def456abc123def456abc123def456abc123def4"

	// Add a monitored relay to the cache
	relayEntry := &cache.RelayEntry{
		URL:           "wss://relay.damus.io",
		Name:          "Damus Relay",
		Health:        "online",
		LatencyMs:     45,
		LastChecked:   time.Now(),
		SupportedNIPs: []int{1, 11, 42, 50},
	}
	err := server.cache.SetRelayEntry(ctx, relayEntry, time.Hour)
	if err != nil {
		t.Fatalf("Failed to set relay entry: %v", err)
	}

	// Pre-populate user NIP-65 cache
	cacheEntry := &cache.UserNIP65Entry{
		Pubkey:    pubkey,
		FetchedAt: time.Now(),
		Relays: []cache.UserRelayData{
			{URL: "wss://relay.damus.io", Read: true, Write: true},
			{URL: "wss://unmonitored.relay.com", Read: true, Write: false},
		},
	}
	err = server.cache.SetUserNIP65(ctx, cacheEntry)
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+pubkey+"/relays", nil)
	w := httptest.NewRecorder()

	server.UserRelaysHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UserRelaysHandler() status = %v, want %v", w.Code, http.StatusOK)
	}

	var resp UserRelayResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that the first relay (monitored) has health data
	var monitoredRelay, unmonitoredRelay *UserRelayEntry
	for i := range resp.Relays {
		if resp.Relays[i].URL == "wss://relay.damus.io" {
			monitoredRelay = &resp.Relays[i]
		} else if resp.Relays[i].URL == "wss://unmonitored.relay.com" {
			unmonitoredRelay = &resp.Relays[i]
		}
	}

	if monitoredRelay == nil {
		t.Fatal("Monitored relay not found in response")
	}
	if !monitoredRelay.Monitored {
		t.Error("Expected monitored relay to have Monitored=true")
	}
	if monitoredRelay.Health == nil {
		t.Error("Expected monitored relay to have health info")
	} else {
		if monitoredRelay.Health.Status != "online" {
			t.Errorf("Health.Status = %v, want online", monitoredRelay.Health.Status)
		}
		if monitoredRelay.Health.Name != "Damus Relay" {
			t.Errorf("Health.Name = %v, want Damus Relay", monitoredRelay.Health.Name)
		}
	}

	if unmonitoredRelay == nil {
		t.Fatal("Unmonitored relay not found in response")
	}
	if unmonitoredRelay.Monitored {
		t.Error("Expected unmonitored relay to have Monitored=false")
	}
	if unmonitoredRelay.Health != nil {
		t.Error("Expected unmonitored relay to have nil health")
	}
}

func TestValidatePubkey(t *testing.T) {
	tests := []struct {
		name    string
		pubkey  string
		wantErr bool
	}{
		{
			name:    "valid pubkey",
			pubkey:  "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantErr: false,
		},
		{
			name:    "empty pubkey",
			pubkey:  "",
			wantErr: true,
		},
		{
			name:    "too short",
			pubkey:  "abc123",
			wantErr: true,
		},
		{
			name:    "too long",
			pubkey:  "abc123def456abc123def456abc123def456abc123def456abc123def456abcdef12",
			wantErr: true,
		},
		{
			name:    "invalid hex characters",
			pubkey:  "xyz123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantErr: true,
		},
		{
			name:    "valid all zeros",
			pubkey:  "0000000000000000000000000000000000000000000000000000000000000000",
			wantErr: false,
		},
		{
			name:    "valid all f's",
			pubkey:  "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePubkey(tt.pubkey)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePubkey(%q) error = %v, wantErr %v", tt.pubkey, err, tt.wantErr)
			}
		})
	}
}

func TestParseNIP65Tags(t *testing.T) {
	tests := []struct {
		name     string
		tags     nostr.Tags
		expected []UserRelayEntry
	}{
		{
			name: "read and write relay",
			tags: nostr.Tags{
				{"r", "wss://relay.example.com"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay.example.com", Read: true, Write: true},
			},
		},
		{
			name: "read-only relay",
			tags: nostr.Tags{
				{"r", "wss://relay.example.com", "read"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay.example.com", Read: true, Write: false},
			},
		},
		{
			name: "write-only relay",
			tags: nostr.Tags{
				{"r", "wss://relay.example.com", "write"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay.example.com", Read: false, Write: true},
			},
		},
		{
			name: "multiple relays with mixed markers",
			tags: nostr.Tags{
				{"r", "wss://relay1.example.com"},
				{"r", "wss://relay2.example.com", "read"},
				{"r", "wss://relay3.example.com", "write"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay1.example.com", Read: true, Write: true},
				{URL: "wss://relay2.example.com", Read: true, Write: false},
				{URL: "wss://relay3.example.com", Read: false, Write: true},
			},
		},
		{
			name:     "empty tags",
			tags:     nostr.Tags{},
			expected: nil,
		},
		{
			name: "ignore non-r tags",
			tags: nostr.Tags{
				{"p", "abc123"},
				{"e", "def456"},
				{"r", "wss://relay.example.com"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay.example.com", Read: true, Write: true},
			},
		},
		{
			name: "skip empty URL",
			tags: nostr.Tags{
				{"r", ""},
				{"r", "wss://relay.example.com"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay.example.com", Read: true, Write: true},
			},
		},
		{
			name: "skip malformed tag (only one element)",
			tags: nostr.Tags{
				{"r"},
				{"r", "wss://relay.example.com"},
			},
			expected: []UserRelayEntry{
				{URL: "wss://relay.example.com", Read: true, Write: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNIP65Tags(tt.tags)

			if len(result) != len(tt.expected) {
				t.Errorf("parseNIP65Tags() returned %d entries, want %d", len(result), len(tt.expected))
				return
			}

			for i, entry := range result {
				if entry.URL != tt.expected[i].URL {
					t.Errorf("Entry[%d].URL = %v, want %v", i, entry.URL, tt.expected[i].URL)
				}
				if entry.Read != tt.expected[i].Read {
					t.Errorf("Entry[%d].Read = %v, want %v", i, entry.Read, tt.expected[i].Read)
				}
				if entry.Write != tt.expected[i].Write {
					t.Errorf("Entry[%d].Write = %v, want %v", i, entry.Write, tt.expected[i].Write)
				}
			}
		})
	}
}

func TestEnrichWithHealth_EmptyEntries(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	entries := []UserRelayEntry{}

	result := server.enrichWithHealth(ctx, entries)

	if len(result) != 0 {
		t.Errorf("enrichWithHealth() returned %d entries, want 0", len(result))
	}
}

func TestEnrichWithHealth_MixedRelays(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Add one monitored relay
	relayEntry := &cache.RelayEntry{
		URL:           "wss://monitored.relay.com",
		Name:          "Monitored Relay",
		Health:        "online",
		LatencyMs:     100,
		LastChecked:   time.Now(),
		SupportedNIPs: []int{1, 11},
	}
	if err := server.cache.SetRelayEntry(ctx, relayEntry, time.Hour); err != nil {
		t.Fatalf("Failed to set relay entry: %v", err)
	}

	entries := []UserRelayEntry{
		{URL: "wss://monitored.relay.com", Read: true, Write: true},
		{URL: "wss://unmonitored.relay.com", Read: true, Write: false},
	}

	result := server.enrichWithHealth(ctx, entries)

	if len(result) != 2 {
		t.Fatalf("enrichWithHealth() returned %d entries, want 2", len(result))
	}

	// Check monitored relay
	if !result[0].Monitored {
		t.Error("Expected first relay to be monitored")
	}
	if result[0].Health == nil {
		t.Error("Expected first relay to have health info")
	}

	// Check unmonitored relay
	if result[1].Monitored {
		t.Error("Expected second relay to not be monitored")
	}
	if result[1].Health != nil {
		t.Error("Expected second relay to have nil health")
	}
}

func TestUserRelaysHandler_ResponseFields(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "bbb123def456abc123def456abc123def456abc123def456abc123def456abcd"

	// Pre-populate cache
	fetchedAt := time.Now()
	cacheEntry := &cache.UserNIP65Entry{
		Pubkey:    pubkey,
		FetchedAt: fetchedAt,
		Relays: []cache.UserRelayData{
			{URL: "wss://relay1.com", Read: true, Write: true},
		},
	}
	if err := server.cache.SetUserNIP65(ctx, cacheEntry); err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+pubkey+"/relays", nil)
	w := httptest.NewRecorder()

	server.UserRelaysHandler(w, req)

	var resp UserRelayResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify all expected fields are present
	if resp.Pubkey != pubkey {
		t.Errorf("Pubkey = %v, want %v", resp.Pubkey, pubkey)
	}
	if resp.Total != 1 {
		t.Errorf("Total = %v, want 1", resp.Total)
	}
	if !resp.CacheHit {
		t.Error("Expected CacheHit to be true")
	}
	if resp.FetchedAt.IsZero() {
		t.Error("FetchedAt should not be zero")
	}

	// Check relay entry fields
	if len(resp.Relays) != 1 {
		t.Fatalf("Expected 1 relay, got %d", len(resp.Relays))
	}
	relay := resp.Relays[0]
	if relay.URL != "wss://relay1.com" {
		t.Errorf("Relay URL = %v, want wss://relay1.com", relay.URL)
	}
	if !relay.Read {
		t.Error("Expected Read to be true")
	}
	if !relay.Write {
		t.Error("Expected Write to be true")
	}
}

func TestUserRelaysHandler_PathParsing(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Test that path parsing extracts pubkey correctly with various path formats
	tests := []struct {
		name        string
		path        string
		wantInvalid bool
	}{
		{
			name:        "standard path",
			path:        "/api/v1/users/abc123def456abc123def456abc123def456abc123def456abc123def456abcd/relays",
			wantInvalid: false,
		},
		{
			name:        "no trailing slash",
			path:        "/api/v1/users/abc123def456abc123def456abc123def456abc123def456abc123def456abcd/relays",
			wantInvalid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pre-populate cache for valid pubkeys
			if !tt.wantInvalid {
				pubkey := strings.TrimPrefix(tt.path, "/api/v1/users/")
				pubkey = strings.TrimSuffix(pubkey, "/relays")

				cacheEntry := &cache.UserNIP65Entry{
					Pubkey:    pubkey,
					FetchedAt: time.Now(),
					Relays:    []cache.UserRelayData{{URL: "wss://test.relay", Read: true, Write: true}},
				}
				if err := server.cache.SetUserNIP65(ctx, cacheEntry); err != nil {
					t.Fatalf("Failed to set cache: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.UserRelaysHandler(w, req)

			if tt.wantInvalid {
				if w.Code != http.StatusBadRequest {
					t.Errorf("Expected StatusBadRequest for invalid path, got %v", w.Code)
				}
			} else {
				if w.Code != http.StatusOK {
					t.Errorf("Expected StatusOK, got %v (body: %s)", w.Code, w.Body.String())
				}
			}
		})
	}
}
