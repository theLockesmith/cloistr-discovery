package relay

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

// setupTestMonitor creates a test monitor with miniredis cache.
func setupTestMonitor(t *testing.T, cfg *config.Config) (*Monitor, *miniredis.Miniredis, *cache.Client) {
	t.Helper()

	mr := miniredis.RunT(t)

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("Failed to create cache client: %v", err)
	}

	if cfg == nil {
		cfg = &config.Config{
			SeedRelays:         []string{"wss://relay.example.com"},
			RelayCheckInterval: 300,
			NIP11Timeout:       10,
		}
	}

	monitor := NewMonitor(cfg, cacheClient)
	return monitor, mr, cacheClient
}

func TestNewMonitor(t *testing.T) {
	t.Run("creates monitor with correct defaults", func(t *testing.T) {
		cfg := &config.Config{
			SeedRelays:         []string{"wss://relay1.example.com", "wss://relay2.example.com"},
			RelayCheckInterval: 300,
			NIP11Timeout:       10,
		}

		monitor, _, _ := setupTestMonitor(t, cfg)

		if monitor == nil {
			t.Fatal("NewMonitor() returned nil")
		}

		if monitor.cfg != cfg {
			t.Error("monitor config not set correctly")
		}

		if monitor.cache == nil {
			t.Error("monitor cache is nil")
		}

		if monitor.client == nil {
			t.Error("monitor HTTP client is nil")
		}

		if monitor.client.Timeout != time.Duration(cfg.NIP11Timeout)*time.Second {
			t.Errorf("HTTP client timeout = %v, want %v",
				monitor.client.Timeout, time.Duration(cfg.NIP11Timeout)*time.Second)
		}

		if monitor.knownRelays == nil {
			t.Error("knownRelays map is nil")
		}

		if len(monitor.knownRelays) != 0 {
			t.Errorf("knownRelays should be empty initially, got %d", len(monitor.knownRelays))
		}

		if monitor.discoveryInput == nil {
			t.Error("discoveryInput channel is nil")
		}

		// Test channel capacity
		if cap(monitor.discoveryInput) != 1000 {
			t.Errorf("discoveryInput channel capacity = %d, want 1000", cap(monitor.discoveryInput))
		}
	})

	t.Run("creates monitor with different timeout", func(t *testing.T) {
		cfg := &config.Config{
			SeedRelays:         []string{},
			RelayCheckInterval: 60,
			NIP11Timeout:       5,
		}

		monitor, _, _ := setupTestMonitor(t, cfg)

		if monitor.client.Timeout != 5*time.Second {
			t.Errorf("HTTP client timeout = %v, want 5s", monitor.client.Timeout)
		}
	})
}

func TestMonitor_AddRelay(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "add relay with wss protocol",
			url:      "wss://relay.example.com",
			expected: "wss://relay.example.com",
		},
		{
			name:     "add relay with trailing slash",
			url:      "wss://relay.example.com/",
			expected: "wss://relay.example.com",
		},
		{
			name:     "add relay with whitespace",
			url:      "  wss://relay.example.com  ",
			expected: "wss://relay.example.com",
		},
		{
			name:     "add relay with ws protocol",
			url:      "ws://localhost:7777",
			expected: "ws://localhost:7777",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor.AddRelay(tt.url)

			relays := monitor.GetRelays()
			found := false
			for _, r := range relays {
				if r == tt.expected {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("AddRelay() relay %s not found in known relays", tt.expected)
			}
		})
	}
}

func TestMonitor_RemoveRelay(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	// Add some relays
	monitor.AddRelay("wss://relay1.example.com")
	monitor.AddRelay("wss://relay2.example.com")
	monitor.AddRelay("wss://relay3.example.com")

	if monitor.RelayCount() != 3 {
		t.Fatalf("Expected 3 relays, got %d", monitor.RelayCount())
	}

	// Remove one
	monitor.RemoveRelay("wss://relay2.example.com")

	if monitor.RelayCount() != 2 {
		t.Errorf("After removal, expected 2 relays, got %d", monitor.RelayCount())
	}

	relays := monitor.GetRelays()
	for _, r := range relays {
		if r == "wss://relay2.example.com" {
			t.Error("RemoveRelay() relay still present after removal")
		}
	}
}

func TestMonitor_RemoveRelay_Normalization(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	// Add relay
	monitor.AddRelay("wss://relay.example.com")

	// Remove with trailing slash (should normalize and remove)
	monitor.RemoveRelay("wss://relay.example.com/")

	if monitor.RelayCount() != 0 {
		t.Errorf("After removal with normalized URL, expected 0 relays, got %d", monitor.RelayCount())
	}
}

func TestMonitor_GetRelays(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	t.Run("get empty relay list", func(t *testing.T) {
		relays := monitor.GetRelays()
		if relays == nil {
			t.Error("GetRelays() returned nil, expected empty slice")
		}
		if len(relays) != 0 {
			t.Errorf("GetRelays() returned %d relays, want 0", len(relays))
		}
	})

	t.Run("get relay list with relays", func(t *testing.T) {
		monitor.AddRelay("wss://relay1.example.com")
		monitor.AddRelay("wss://relay2.example.com")
		monitor.AddRelay("wss://relay3.example.com")

		relays := monitor.GetRelays()
		if len(relays) != 3 {
			t.Errorf("GetRelays() returned %d relays, want 3", len(relays))
		}

		// Verify all added relays are present
		expected := map[string]bool{
			"wss://relay1.example.com": false,
			"wss://relay2.example.com": false,
			"wss://relay3.example.com": false,
		}

		for _, r := range relays {
			if _, ok := expected[r]; ok {
				expected[r] = true
			}
		}

		for url, found := range expected {
			if !found {
				t.Errorf("GetRelays() missing expected relay: %s", url)
			}
		}
	})
}

func TestMonitor_RelayCount(t *testing.T) {
	tests := []struct {
		name     string
		addURLs  []string
		expected int
	}{
		{
			name:     "no relays",
			addURLs:  []string{},
			expected: 0,
		},
		{
			name:     "single relay",
			addURLs:  []string{"wss://relay1.example.com"},
			expected: 1,
		},
		{
			name:     "multiple relays",
			addURLs:  []string{"wss://relay1.example.com", "wss://relay2.example.com", "wss://relay3.example.com"},
			expected: 3,
		},
		{
			name:     "duplicate relays (should not increase count)",
			addURLs:  []string{"wss://same.example.com", "wss://same.example.com"},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh monitor for each test
			m, _, _ := setupTestMonitor(t, nil)

			for _, url := range tt.addURLs {
				m.AddRelay(url)
			}

			count := m.RelayCount()
			if count != tt.expected {
				t.Errorf("RelayCount() = %d, want %d", count, tt.expected)
			}
		})
	}
}

func TestMonitor_LastCheck(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	t.Run("initial last check is zero time", func(t *testing.T) {
		lastCheck := monitor.LastCheck()
		if !lastCheck.IsZero() {
			t.Errorf("LastCheck() = %v, want zero time", lastCheck)
		}
	})

	t.Run("last check updates after manual set", func(t *testing.T) {
		// Simulate updating lastCheck (in real usage, checkAllRelays does this)
		now := time.Now()
		monitor.mu.Lock()
		monitor.lastCheck = now
		monitor.mu.Unlock()

		lastCheck := monitor.LastCheck()
		if lastCheck.IsZero() {
			t.Error("LastCheck() returned zero time after update")
		}

		// Allow for small time difference due to test execution
		if lastCheck.Sub(now).Abs() > time.Second {
			t.Errorf("LastCheck() = %v, want approximately %v", lastCheck, now)
		}
	})
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "url without trailing slash",
			input:    "wss://relay.example.com",
			expected: "wss://relay.example.com",
		},
		{
			name:     "url with trailing slash",
			input:    "wss://relay.example.com/",
			expected: "wss://relay.example.com",
		},
		{
			name:     "url with leading whitespace",
			input:    "  wss://relay.example.com",
			expected: "wss://relay.example.com",
		},
		{
			name:     "url with trailing whitespace",
			input:    "wss://relay.example.com  ",
			expected: "wss://relay.example.com",
		},
		{
			name:     "url with both whitespace and trailing slash",
			input:    "  wss://relay.example.com/  ",
			expected: "wss://relay.example.com",
		},
		{
			name:     "url with multiple trailing slashes",
			input:    "wss://relay.example.com///",
			expected: "wss://relay.example.com//",
		},
		{
			name:     "url with path",
			input:    "wss://relay.example.com/path",
			expected: "wss://relay.example.com/path",
		},
		{
			name:     "url with path and trailing slash",
			input:    "wss://relay.example.com/path/",
			expected: "wss://relay.example.com/path",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "ws protocol",
			input:    "ws://localhost:7777/",
			expected: "ws://localhost:7777",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWsToHTTP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "wss to https",
			input:    "wss://relay.example.com",
			expected: "https://relay.example.com",
		},
		{
			name:     "wss to https with path",
			input:    "wss://relay.example.com/path",
			expected: "https://relay.example.com/path",
		},
		{
			name:     "ws to http",
			input:    "ws://localhost:7777",
			expected: "http://localhost:7777",
		},
		{
			name:     "ws to http with path",
			input:    "ws://localhost:7777/nostr",
			expected: "http://localhost:7777/nostr",
		},
		{
			name:     "already https",
			input:    "https://relay.example.com",
			expected: "https://relay.example.com",
		},
		{
			name:     "already http",
			input:    "http://relay.example.com",
			expected: "http://relay.example.com",
		},
		{
			name:     "wss with port",
			input:    "wss://relay.example.com:443",
			expected: "https://relay.example.com:443",
		},
		{
			name:     "ws with port",
			input:    "ws://relay.example.com:80",
			expected: "http://relay.example.com:80",
		},
		{
			name:     "no protocol",
			input:    "relay.example.com",
			expected: "relay.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wsToHTTP(tt.input)
			if result != tt.expected {
				t.Errorf("wsToHTTP(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMonitor_DiscoveryChannel(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	ch := monitor.DiscoveryChannel()
	if ch == nil {
		t.Fatal("DiscoveryChannel() returned nil")
	}

	// Test that we can send to the channel
	select {
	case ch <- "wss://test.relay.com":
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("DiscoveryChannel() channel blocked unexpectedly")
	}
}

func TestMonitor_HandleDiscoveredRelay(t *testing.T) {
	ctx := context.Background()

	t.Run("adds new relay", func(t *testing.T) {
		monitor, _, _ := setupTestMonitor(t, nil)

		initialCount := monitor.RelayCount()

		monitor.handleDiscoveredRelay(ctx, "wss://new-relay.example.com")

		// Give it a moment to process (it's called synchronously in the test)
		time.Sleep(10 * time.Millisecond)

		if monitor.RelayCount() != initialCount+1 {
			t.Errorf("handleDiscoveredRelay() count = %d, want %d", monitor.RelayCount(), initialCount+1)
		}

		relays := monitor.GetRelays()
		found := false
		for _, r := range relays {
			if r == "wss://new-relay.example.com" {
				found = true
				break
			}
		}
		if !found {
			t.Error("handleDiscoveredRelay() relay not added to known relays")
		}
	})

	t.Run("normalizes URL before adding", func(t *testing.T) {
		monitor, _, _ := setupTestMonitor(t, nil)

		monitor.handleDiscoveredRelay(ctx, "  wss://relay-with-space.example.com/  ")

		relays := monitor.GetRelays()
		found := false
		for _, r := range relays {
			if r == "wss://relay-with-space.example.com" {
				found = true
				break
			}
		}
		if !found {
			t.Error("handleDiscoveredRelay() did not normalize URL correctly")
		}
	})

	t.Run("ignores empty URL after normalization", func(t *testing.T) {
		monitor, _, _ := setupTestMonitor(t, nil)

		initialCount := monitor.RelayCount()
		monitor.handleDiscoveredRelay(ctx, "   ")

		if monitor.RelayCount() != initialCount {
			t.Error("handleDiscoveredRelay() should ignore empty normalized URL")
		}
	})

	t.Run("does not add duplicate relay", func(t *testing.T) {
		monitor, _, _ := setupTestMonitor(t, nil)

		monitor.AddRelay("wss://existing.example.com")
		initialCount := monitor.RelayCount()

		monitor.handleDiscoveredRelay(ctx, "wss://existing.example.com")

		if monitor.RelayCount() != initialCount {
			t.Error("handleDiscoveredRelay() should not add duplicate relay")
		}
	})

	t.Run("skips blacklisted relay", func(t *testing.T) {
		monitor, _, cacheClient := setupTestMonitor(t, nil)

		// Add to blacklist
		blacklistedURL := "wss://blacklisted.example.com"
		err := cacheClient.AddToBlacklist(ctx, blacklistedURL)
		if err != nil {
			t.Fatalf("Failed to add to blacklist: %v", err)
		}

		initialCount := monitor.RelayCount()
		monitor.handleDiscoveredRelay(ctx, blacklistedURL)

		time.Sleep(10 * time.Millisecond)

		if monitor.RelayCount() != initialCount {
			t.Error("handleDiscoveredRelay() should not add blacklisted relay")
		}
	})
}

func TestMonitor_CheckRelay(t *testing.T) {
	ctx := context.Background()

	t.Run("successful NIP-11 fetch", func(t *testing.T) {
		// Create test HTTP server
		nip11Info := NIP11Info{
			Name:          "Test Relay",
			Description:   "A test relay for unit tests",
			Pubkey:        "testpubkey123",
			Contact:       "test@example.com",
			SupportedNIPs: []int{1, 11, 42},
			Software:      "test-relay",
			Version:       "1.0.0",
		}
		nip11Info.Limitation.AuthRequired = false
		nip11Info.Limitation.PaymentRequired = false

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify Accept header
			if r.Header.Get("Accept") != "application/nostr+json" {
				t.Errorf("Accept header = %s, want application/nostr+json", r.Header.Get("Accept"))
			}

			w.Header().Set("Content-Type", "application/nostr+json")
			json.NewEncoder(w).Encode(nip11Info)
		}))
		defer ts.Close()

		monitor, _, _ := setupTestMonitor(t, nil)

		// Convert test server URL to wss:// format (monitor will convert back to http)
		relayURL := "wss://test.relay"

		// Override HTTP client to use test server
		monitor.client.Transport = &transportOverride{testServerURL: ts.URL}

		entry, err := monitor.checkRelay(ctx, relayURL)
		if err != nil {
			t.Fatalf("checkRelay() error: %v", err)
		}

		if entry == nil {
			t.Fatal("checkRelay() returned nil entry")
		}

		if entry.URL != relayURL {
			t.Errorf("entry.URL = %s, want %s", entry.URL, relayURL)
		}
		if entry.Name != nip11Info.Name {
			t.Errorf("entry.Name = %s, want %s", entry.Name, nip11Info.Name)
		}
		if entry.Description != nip11Info.Description {
			t.Errorf("entry.Description = %s, want %s", entry.Description, nip11Info.Description)
		}
		if len(entry.SupportedNIPs) != len(nip11Info.SupportedNIPs) {
			t.Errorf("entry.SupportedNIPs length = %d, want %d", len(entry.SupportedNIPs), len(nip11Info.SupportedNIPs))
		}
		if entry.Health != "online" {
			t.Errorf("entry.Health = %s, want online", entry.Health)
		}
		// Latency can be 0 for very fast local test servers
		if entry.LatencyMs < 0 {
			t.Error("entry.LatencyMs should be >= 0")
		}
	})

	t.Run("degraded health for slow response", func(t *testing.T) {
		nip11Info := NIP11Info{
			Name: "Slow Relay",
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate slow response
			time.Sleep(6 * time.Second)
			w.Header().Set("Content-Type", "application/nostr+json")
			json.NewEncoder(w).Encode(nip11Info)
		}))
		defer ts.Close()

		cfg := &config.Config{
			SeedRelays:         []string{},
			RelayCheckInterval: 300,
			NIP11Timeout:       15, // High enough to not timeout
		}
		monitor, _, _ := setupTestMonitor(t, cfg)
		monitor.client.Transport = &transportOverride{testServerURL: ts.URL}

		entry, err := monitor.checkRelay(ctx, "wss://slow.relay")
		if err != nil {
			t.Fatalf("checkRelay() error: %v", err)
		}

		if entry.Health != "degraded" {
			t.Errorf("entry.Health = %s, want degraded", entry.Health)
		}

		if entry.LatencyMs <= 5000 {
			t.Errorf("entry.LatencyMs = %d, want > 5000", entry.LatencyMs)
		}
	})

	t.Run("handles non-200 status code", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		monitor, _, _ := setupTestMonitor(t, nil)
		monitor.client.Transport = &transportOverride{testServerURL: ts.URL}

		_, err := monitor.checkRelay(ctx, "wss://notfound.relay")
		if err == nil {
			t.Error("checkRelay() expected error for 404 response, got nil")
		}
	})

	t.Run("handles invalid JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/nostr+json")
			w.Write([]byte("invalid json {{{"))
		}))
		defer ts.Close()

		monitor, _, _ := setupTestMonitor(t, nil)
		monitor.client.Transport = &transportOverride{testServerURL: ts.URL}

		_, err := monitor.checkRelay(ctx, "wss://badjson.relay")
		if err == nil {
			t.Error("checkRelay() expected error for invalid JSON, got nil")
		}
	})
}

func TestMonitor_ConcurrentAccess(t *testing.T) {
	monitor, _, _ := setupTestMonitor(t, nil)

	// Test concurrent AddRelay calls
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				monitor.AddRelay("wss://relay" + string(rune(n)) + ".example.com")
				monitor.GetRelays()
				monitor.RelayCount()
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and should have some relays added
	count := monitor.RelayCount()
	if count == 0 {
		t.Error("Expected some relays to be added concurrently")
	}
}

// transportOverride is a test helper to redirect HTTP requests to a test server
type transportOverride struct {
	testServerURL string
}

func (t *transportOverride) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect all requests to test server
	req.URL.Scheme = "http"
	req.URL.Host = req.URL.Host // Keep original for testing URL conversion

	// Create new request to test server
	testReq, err := http.NewRequest(req.Method, t.testServerURL, req.Body)
	if err != nil {
		return nil, err
	}
	testReq.Header = req.Header

	return http.DefaultTransport.RoundTrip(testReq)
}
