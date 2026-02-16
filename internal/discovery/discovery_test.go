package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// setupTestCoordinator creates a test coordinator with miniredis cache.
// Returns the coordinator, miniredis instance, cache client, and output channel.
func setupTestCoordinator(t *testing.T, cfg *config.Config) (*Coordinator, *miniredis.Miniredis, *cache.Client, chan string) {
	t.Helper()

	mr := miniredis.RunT(t)

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("Failed to create cache client: %v", err)
	}

	if cfg == nil {
		cfg = &config.Config{
			NIP65CrawlEnabled:    true,
			NIP66Enabled:         true,
			PeerDiscoveryEnabled: true,
			HostedRelayListURL:   "https://example.com/relays.json",
		}
	}

	output := make(chan string, 10)
	coordinator := NewCoordinator(cfg, cacheClient, output)

	return coordinator, mr, cacheClient, output
}

func TestNewCoordinator(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *config.Config
		wantHosted       bool
		wantNIP65        bool
		wantNIP66        bool
		wantPeers        bool
		wantChannelCap   int
		wantDiscoveryCap int
	}{
		{
			name: "all sources enabled",
			cfg: &config.Config{
				HostedRelayListURL:   "https://example.com/relays.json",
				NIP65CrawlEnabled:    true,
				NIP66Enabled:         true,
				PeerDiscoveryEnabled: true,
			},
			wantHosted:       true,
			wantNIP65:        true,
			wantNIP66:        true,
			wantPeers:        true,
			wantChannelCap:   10,
			wantDiscoveryCap: 1000,
		},
		{
			name: "only NIP-65 enabled",
			cfg: &config.Config{
				HostedRelayListURL:   "",
				NIP65CrawlEnabled:    true,
				NIP66Enabled:         false,
				PeerDiscoveryEnabled: false,
			},
			wantHosted:       false,
			wantNIP65:        true,
			wantNIP66:        false,
			wantPeers:        false,
			wantChannelCap:   10,
			wantDiscoveryCap: 1000,
		},
		{
			name: "only NIP-66 enabled",
			cfg: &config.Config{
				HostedRelayListURL:   "",
				NIP65CrawlEnabled:    false,
				NIP66Enabled:         true,
				PeerDiscoveryEnabled: false,
			},
			wantHosted:       false,
			wantNIP65:        false,
			wantNIP66:        true,
			wantPeers:        false,
			wantChannelCap:   10,
			wantDiscoveryCap: 1000,
		},
		{
			name: "all sources disabled",
			cfg: &config.Config{
				HostedRelayListURL:   "",
				NIP65CrawlEnabled:    false,
				NIP66Enabled:         false,
				PeerDiscoveryEnabled: false,
			},
			wantHosted:       false,
			wantNIP65:        false,
			wantNIP66:        false,
			wantPeers:        false,
			wantChannelCap:   10,
			wantDiscoveryCap: 1000,
		},
		{
			name: "hosted and peers enabled",
			cfg: &config.Config{
				HostedRelayListURL:   "https://example.com/relays.json",
				NIP65CrawlEnabled:    false,
				NIP66Enabled:         false,
				PeerDiscoveryEnabled: true,
			},
			wantHosted:       true,
			wantNIP65:        false,
			wantNIP66:        false,
			wantPeers:        true,
			wantChannelCap:   10,
			wantDiscoveryCap: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			defer mr.Close()

			cacheClient, err := cache.New("redis://" + mr.Addr())
			if err != nil {
				t.Fatalf("Failed to create cache client: %v", err)
			}
			defer cacheClient.Close()

			output := make(chan string, tt.wantChannelCap)
			coordinator := NewCoordinator(tt.cfg, cacheClient, output)

			if coordinator == nil {
				t.Fatal("NewCoordinator() returned nil")
			}

			if coordinator.cfg != tt.cfg {
				t.Error("coordinator config not set correctly")
			}

			if coordinator.cache != cacheClient {
				t.Error("coordinator cache not set correctly")
			}

			if coordinator.discoveries == nil {
				t.Fatal("discoveries channel is nil")
			}

			if cap(coordinator.discoveries) != tt.wantDiscoveryCap {
				t.Errorf("discoveries channel capacity = %d, want %d",
					cap(coordinator.discoveries), tt.wantDiscoveryCap)
			}

			// Check source initialization
			if (coordinator.hostedFetcher != nil) != tt.wantHosted {
				t.Errorf("hostedFetcher initialized = %v, want %v",
					coordinator.hostedFetcher != nil, tt.wantHosted)
			}

			if (coordinator.nip65Crawler != nil) != tt.wantNIP65 {
				t.Errorf("nip65Crawler initialized = %v, want %v",
					coordinator.nip65Crawler != nil, tt.wantNIP65)
			}

			if (coordinator.nip66Consumer != nil) != tt.wantNIP66 {
				t.Errorf("nip66Consumer initialized = %v, want %v",
					coordinator.nip66Consumer != nil, tt.wantNIP66)
			}

			if (coordinator.peerDiscovery != nil) != tt.wantPeers {
				t.Errorf("peerDiscovery initialized = %v, want %v",
					coordinator.peerDiscovery != nil, tt.wantPeers)
			}

			// Verify initial running state
			coordinator.mu.RLock()
			running := coordinator.running
			coordinator.mu.RUnlock()

			if running {
				t.Error("coordinator should not be running initially")
			}
		})
	}
}

func TestCoordinator_SubmitRelay(t *testing.T) {
	coordinator, mr, cacheClient, _ := setupTestCoordinator(t, nil)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := context.Background()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "submit valid relay",
			url:     "wss://relay.example.com",
			wantErr: false,
		},
		{
			name:    "submit relay with trailing slash",
			url:     "wss://relay.example.com/",
			wantErr: false,
		},
		{
			name:    "submit relay with whitespace",
			url:     "  wss://relay.example.com  ",
			wantErr: false,
		},
		{
			name:    "submit empty URL",
			url:     "",
			wantErr: false,
		},
		{
			name:    "submit localhost relay",
			url:     "ws://localhost:7777",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := coordinator.SubmitRelay(ctx, tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("SubmitRelay() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify the relay was sent to the discoveries channel
			select {
			case discovered := <-coordinator.discoveries:
				if discovered.URL != tt.url {
					t.Errorf("discovered URL = %s, want %s", discovered.URL, tt.url)
				}
				if discovered.Source != "manual" {
					t.Errorf("discovered Source = %s, want manual", discovered.Source)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("SubmitRelay() did not send to discoveries channel")
			}
		})
	}
}

func TestNormalizeRelayURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already normalized wss URL",
			input:    "wss://relay.example.com",
			expected: "wss://relay.example.com",
		},
		{
			name:     "wss URL with trailing slash",
			input:    "wss://relay.example.com/",
			expected: "wss://relay.example.com",
		},
		{
			name:     "ws URL",
			input:    "ws://localhost:7777",
			expected: "ws://localhost:7777",
		},
		{
			name:     "ws URL with trailing slash",
			input:    "ws://localhost:7777/",
			expected: "ws://localhost:7777",
		},
		{
			name:     "URL without protocol prefix",
			input:    "relay.example.com",
			expected: "wss://relay.example.com",
		},
		{
			name:     "URL without protocol with trailing slash",
			input:    "relay.example.com/",
			expected: "wss://relay.example.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "very short string",
			input:    "abc",
			expected: "",
		},
		{
			name:     "URL with path",
			input:    "wss://relay.example.com/nostr",
			expected: "wss://relay.example.com/nostr",
		},
		{
			name:     "URL with path and trailing slash",
			input:    "wss://relay.example.com/nostr/",
			expected: "wss://relay.example.com/nostr",
		},
		{
			name:     "URL with multiple trailing slashes",
			input:    "wss://relay.example.com///",
			expected: "wss://relay.example.com//",
		},
		{
			name:     "URL with port",
			input:    "wss://relay.example.com:443",
			expected: "wss://relay.example.com:443",
		},
		{
			name:     "URL with port and trailing slash",
			input:    "wss://relay.example.com:443/",
			expected: "wss://relay.example.com:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeRelayURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeRelayURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCoordinator_HandleDiscoveredRelay(t *testing.T) {
	ctx := context.Background()

	t.Run("processes new relay successfully", func(t *testing.T) {
		coordinator, mr, cacheClient, output := setupTestCoordinator(t, nil)
		defer mr.Close()
		defer cacheClient.Close()

		discovered := DiscoveredRelay{
			URL:    "wss://new-relay.example.com",
			Source: "nip65",
		}

		// Process the discovery
		coordinator.handleDiscoveredRelay(ctx, discovered)

		// Verify relay was sent to output channel
		select {
		case url := <-output:
			if url != "wss://new-relay.example.com" {
				t.Errorf("output URL = %s, want wss://new-relay.example.com", url)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("handleDiscoveredRelay() did not send to output channel")
		}

		// Verify stats were updated
		stat, err := cacheClient.GetStat(ctx, "discovery:nip65")
		if err != nil {
			t.Fatalf("GetStat() error: %v", err)
		}
		if stat != 1 {
			t.Errorf("discovery:nip65 stat = %d, want 1", stat)
		}

		totalStat, err := cacheClient.GetStat(ctx, "discovery:total")
		if err != nil {
			t.Fatalf("GetStat() error: %v", err)
		}
		if totalStat != 1 {
			t.Errorf("discovery:total stat = %d, want 1", totalStat)
		}
	})

	t.Run("normalizes URL before processing", func(t *testing.T) {
		coordinator, mr, cacheClient, output := setupTestCoordinator(t, nil)
		defer mr.Close()
		defer cacheClient.Close()

		discovered := DiscoveredRelay{
			URL:    "wss://relay-with-slash.example.com/",
			Source: "nip66",
		}

		coordinator.handleDiscoveredRelay(ctx, discovered)

		select {
		case url := <-output:
			if url != "wss://relay-with-slash.example.com" {
				t.Errorf("output URL = %s, want wss://relay-with-slash.example.com", url)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("handleDiscoveredRelay() did not send normalized URL")
		}
	})

	t.Run("ignores empty URL after normalization", func(t *testing.T) {
		coordinator, mr, cacheClient, output := setupTestCoordinator(t, nil)
		defer mr.Close()
		defer cacheClient.Close()

		discovered := DiscoveredRelay{
			URL:    "",
			Source: "manual",
		}

		coordinator.handleDiscoveredRelay(ctx, discovered)

		// Should not send to output channel
		select {
		case <-output:
			t.Error("handleDiscoveredRelay() should not send empty URL to output")
		case <-time.After(50 * time.Millisecond):
			// Expected - no output
		}
	})

	t.Run("skips blacklisted relay", func(t *testing.T) {
		coordinator, mr, cacheClient, output := setupTestCoordinator(t, nil)
		defer mr.Close()
		defer cacheClient.Close()

		// Add relay to blacklist
		blacklistedURL := "wss://blacklisted.example.com"
		err := cacheClient.AddToBlacklist(ctx, blacklistedURL)
		if err != nil {
			t.Fatalf("AddToBlacklist() error: %v", err)
		}

		discovered := DiscoveredRelay{
			URL:    blacklistedURL,
			Source: "hosted",
		}

		coordinator.handleDiscoveredRelay(ctx, discovered)

		// Should not send to output channel
		select {
		case <-output:
			t.Error("handleDiscoveredRelay() should not send blacklisted relay to output")
		case <-time.After(50 * time.Millisecond):
			// Expected - no output
		}

		// Stats should not be updated for blacklisted relays
		stat, _ := cacheClient.GetStat(ctx, "discovery:hosted")
		if stat != 0 {
			t.Errorf("discovery:hosted stat = %d, want 0 for blacklisted relay", stat)
		}
	})

	t.Run("deduplicates already seen relay", func(t *testing.T) {
		coordinator, mr, cacheClient, output := setupTestCoordinator(t, nil)
		defer mr.Close()
		defer cacheClient.Close()

		relayURL := "wss://duplicate.example.com"

		// Mark relay as seen
		_, err := cacheClient.MarkRelaySeen(ctx, relayURL)
		if err != nil {
			t.Fatalf("MarkRelaySeen() error: %v", err)
		}

		discovered := DiscoveredRelay{
			URL:    relayURL,
			Source: "peers",
		}

		coordinator.handleDiscoveredRelay(ctx, discovered)

		// Should not send to output channel (already seen)
		select {
		case <-output:
			t.Error("handleDiscoveredRelay() should not send duplicate relay to output")
		case <-time.After(50 * time.Millisecond):
			// Expected - no output
		}

		// Stats should not be updated for duplicates
		stat, _ := cacheClient.GetStat(ctx, "discovery:peers")
		if stat != 0 {
			t.Errorf("discovery:peers stat = %d, want 0 for duplicate relay", stat)
		}
	})

	t.Run("tracks stats for different sources", func(t *testing.T) {
		coordinator, mr, cacheClient, output := setupTestCoordinator(t, nil)
		defer mr.Close()
		defer cacheClient.Close()

		sources := []string{"hosted", "nip65", "nip66", "peers", "manual"}

		for i, source := range sources {
			discovered := DiscoveredRelay{
				URL:    "wss://relay-" + source + ".example.com",
				Source: source,
			}

			coordinator.handleDiscoveredRelay(ctx, discovered)

			// Drain output channel
			<-output

			// Verify source-specific stat
			stat, err := cacheClient.GetStat(ctx, "discovery:"+source)
			if err != nil {
				t.Fatalf("GetStat() error for %s: %v", source, err)
			}
			if stat != 1 {
				t.Errorf("discovery:%s stat = %d, want 1", source, stat)
			}

			// Verify total stat
			totalStat, err := cacheClient.GetStat(ctx, "discovery:total")
			if err != nil {
				t.Fatalf("GetStat() error for total: %v", err)
			}
			if totalStat != int64(i+1) {
				t.Errorf("discovery:total stat = %d, want %d", totalStat, i+1)
			}
		}
	})

	t.Run("handles channel full by dropping relay", func(t *testing.T) {
		// Create coordinator with very small output channel
		cfg := &config.Config{
			NIP65CrawlEnabled:    true,
			NIP66Enabled:         true,
			PeerDiscoveryEnabled: true,
			HostedRelayListURL:   "",
		}
		mr := miniredis.RunT(t)
		defer mr.Close()

		cacheClient, err := cache.New("redis://" + mr.Addr())
		if err != nil {
			t.Fatalf("Failed to create cache client: %v", err)
		}
		defer cacheClient.Close()

		output := make(chan string, 1)
		coordinator := NewCoordinator(cfg, cacheClient, output)

		// Fill the channel
		output <- "wss://fill1.example.com"

		// Try to send another relay
		discovered := DiscoveredRelay{
			URL:    "wss://dropped.example.com",
			Source: "nip65",
		}

		coordinator.handleDiscoveredRelay(ctx, discovered)

		// Channel should still have the original relay
		select {
		case url := <-output:
			if url != "wss://fill1.example.com" {
				t.Errorf("output channel has wrong relay: %s", url)
			}
		default:
			t.Error("output channel should have original relay")
		}

		// Second relay should have been dropped (no panic, no block)
	})
}

func TestCoordinator_GetLastFetchTimes(t *testing.T) {
	t.Run("all sources enabled", func(t *testing.T) {
		cfg := &config.Config{
			HostedRelayListURL:   "https://example.com/relays.json",
			NIP65CrawlEnabled:    true,
			NIP66Enabled:         true,
			PeerDiscoveryEnabled: true,
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		times := coordinator.GetLastFetchTimes()

		// Initially, all times should be zero
		if !times.Hosted.IsZero() {
			t.Error("Hosted time should be zero initially")
		}
		if !times.NIP65.IsZero() {
			t.Error("NIP65 time should be zero initially")
		}
		if !times.NIP66.IsZero() {
			t.Error("NIP66 time should be zero initially")
		}
		if !times.Peers.IsZero() {
			t.Error("Peers time should be zero initially")
		}
	})

	t.Run("only NIP-65 enabled", func(t *testing.T) {
		cfg := &config.Config{
			HostedRelayListURL:   "",
			NIP65CrawlEnabled:    true,
			NIP66Enabled:         false,
			PeerDiscoveryEnabled: false,
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		times := coordinator.GetLastFetchTimes()

		// Only NIP65 should have a non-nil source (even though time is zero)
		if !times.Hosted.IsZero() {
			t.Error("Hosted time should be zero when disabled")
		}
	})

	t.Run("all sources disabled", func(t *testing.T) {
		cfg := &config.Config{
			HostedRelayListURL:   "",
			NIP65CrawlEnabled:    false,
			NIP66Enabled:         false,
			PeerDiscoveryEnabled: false,
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		times := coordinator.GetLastFetchTimes()

		// All times should be zero
		if !times.Hosted.IsZero() {
			t.Error("Hosted time should be zero when disabled")
		}
		if !times.NIP65.IsZero() {
			t.Error("NIP65 time should be zero when disabled")
		}
		if !times.NIP66.IsZero() {
			t.Error("NIP66 time should be zero when disabled")
		}
		if !times.Peers.IsZero() {
			t.Error("Peers time should be zero when disabled")
		}
	})
}

func TestCoordinator_IsNIP65Enabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{
			name:    "NIP-65 enabled",
			enabled: true,
			want:    true,
		},
		{
			name:    "NIP-65 disabled",
			enabled: false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				NIP65CrawlEnabled:    tt.enabled,
				NIP66Enabled:         false,
				PeerDiscoveryEnabled: false,
				HostedRelayListURL:   "",
			}
			coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
			defer mr.Close()
			defer cacheClient.Close()

			result := coordinator.IsNIP65Enabled()
			if result != tt.want {
				t.Errorf("IsNIP65Enabled() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestCoordinator_IsNIP66Enabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{
			name:    "NIP-66 enabled",
			enabled: true,
			want:    true,
		},
		{
			name:    "NIP-66 disabled",
			enabled: false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				NIP65CrawlEnabled:    false,
				NIP66Enabled:         tt.enabled,
				PeerDiscoveryEnabled: false,
				HostedRelayListURL:   "",
			}
			coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
			defer mr.Close()
			defer cacheClient.Close()

			result := coordinator.IsNIP66Enabled()
			if result != tt.want {
				t.Errorf("IsNIP66Enabled() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestCoordinator_NIP65LastCrawl(t *testing.T) {
	t.Run("returns zero time when disabled", func(t *testing.T) {
		cfg := &config.Config{
			NIP65CrawlEnabled:    false,
			NIP66Enabled:         false,
			PeerDiscoveryEnabled: false,
			HostedRelayListURL:   "",
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		lastCrawl := coordinator.NIP65LastCrawl()
		if !lastCrawl.IsZero() {
			t.Errorf("NIP65LastCrawl() = %v, want zero time when disabled", lastCrawl)
		}
	})

	t.Run("returns time when enabled", func(t *testing.T) {
		cfg := &config.Config{
			NIP65CrawlEnabled:    true,
			NIP66Enabled:         false,
			PeerDiscoveryEnabled: false,
			HostedRelayListURL:   "",
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		// Initially zero
		lastCrawl := coordinator.NIP65LastCrawl()
		if !lastCrawl.IsZero() {
			t.Errorf("NIP65LastCrawl() = %v, want zero time initially", lastCrawl)
		}
	})
}

func TestCoordinator_NIP66LastConsume(t *testing.T) {
	t.Run("returns zero time when disabled", func(t *testing.T) {
		cfg := &config.Config{
			NIP65CrawlEnabled:    false,
			NIP66Enabled:         false,
			PeerDiscoveryEnabled: false,
			HostedRelayListURL:   "",
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		lastConsume := coordinator.NIP66LastConsume()
		if !lastConsume.IsZero() {
			t.Errorf("NIP66LastConsume() = %v, want zero time when disabled", lastConsume)
		}
	})

	t.Run("returns time when enabled", func(t *testing.T) {
		cfg := &config.Config{
			NIP65CrawlEnabled:    false,
			NIP66Enabled:         true,
			PeerDiscoveryEnabled: false,
			HostedRelayListURL:   "",
		}
		coordinator, mr, cacheClient, _ := setupTestCoordinator(t, cfg)
		defer mr.Close()
		defer cacheClient.Close()

		// Initially zero
		lastConsume := coordinator.NIP66LastConsume()
		if !lastConsume.IsZero() {
			t.Errorf("NIP66LastConsume() = %v, want zero time initially", lastConsume)
		}
	})
}

func TestCoordinator_GetStats(t *testing.T) {
	coordinator, mr, cacheClient, _ := setupTestCoordinator(t, nil)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := context.Background()

	// Set some test stats
	err := cacheClient.SetStat(ctx, "discovery:total", 100)
	if err != nil {
		t.Fatalf("SetStat() error: %v", err)
	}

	err = cacheClient.SetStat(ctx, "discovery:nip65", 50)
	if err != nil {
		t.Fatalf("SetStat() error: %v", err)
	}

	err = cacheClient.SetStat(ctx, "discovery:nip66", 30)
	if err != nil {
		t.Fatalf("SetStat() error: %v", err)
	}

	stats, err := coordinator.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error: %v", err)
	}

	if stats["discovery:total"] != 100 {
		t.Errorf("discovery:total = %d, want 100", stats["discovery:total"])
	}

	if stats["discovery:nip65"] != 50 {
		t.Errorf("discovery:nip65 = %d, want 50", stats["discovery:nip65"])
	}

	if stats["discovery:nip66"] != 30 {
		t.Errorf("discovery:nip66 = %d, want 30", stats["discovery:nip66"])
	}
}

func TestDiscoveredRelay_SourceValues(t *testing.T) {
	// Test that DiscoveredRelay struct works with all expected source values
	sources := []string{"hosted", "nip65", "nip66", "peers", "manual"}

	for _, source := range sources {
		discovered := DiscoveredRelay{
			URL:    "wss://test.example.com",
			Source: source,
		}

		if discovered.Source != source {
			t.Errorf("DiscoveredRelay.Source = %s, want %s", discovered.Source, source)
		}
	}
}

func TestCoordinator_ConcurrentSubmit(t *testing.T) {
	coordinator, mr, cacheClient, _ := setupTestCoordinator(t, nil)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := context.Background()

	// Submit relays concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 10; j++ {
				url := "wss://concurrent-" + string(rune(n)) + "-" + string(rune(j)) + ".example.com"
				coordinator.SubmitRelay(ctx, url)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and should have populated the discoveries channel
}
