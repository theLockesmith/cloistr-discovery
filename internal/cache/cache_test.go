package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// setupTestCache creates a test cache client with miniredis.
func setupTestCache(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	client := &Client{rdb: rdb}

	return client, mr
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid redis URL",
			url:     "redis://localhost:6379",
			wantErr: false,
		},
		{
			name:    "valid redis URL with database",
			url:     "redis://localhost:6379/0",
			wantErr: false,
		},
		{
			name:    "valid redis URL with password",
			url:     "redis://:password@localhost:6379",
			wantErr: false,
		},
		{
			name:    "invalid URL format",
			url:     "not-a-valid-url",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("New() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("New() unexpected error: %v", err)
				return
			}

			if client == nil {
				t.Error("New() returned nil client")
				return
			}

			if client.rdb == nil {
				t.Error("New() client has nil rdb")
			}

			client.Close()
		})
	}
}

func TestClient_Close(t *testing.T) {
	client, _ := setupTestCache(t)

	err := client.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

func TestClient_Ping(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("successful ping", func(t *testing.T) {
		err := client.Ping(ctx)
		if err != nil {
			t.Errorf("Ping() unexpected error: %v", err)
		}
	})

	t.Run("ping after server close", func(t *testing.T) {
		mr.Close()
		err := client.Ping(ctx)
		if err == nil {
			t.Error("Ping() expected error after server close, got nil")
		}
	})
}

func TestClient_SetRelayEntry_GetRelayEntry(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("store and retrieve relay entry", func(t *testing.T) {
		entry := &RelayEntry{
			URL:           "wss://relay.example.com",
			Name:          "Example Relay",
			Description:   "A test relay",
			Pubkey:        "pubkey123",
			SupportedNIPs: []int{1, 11, 42},
			Software:      "nostr-rs-relay",
			Version:       "0.8.0",
			Health:        "online",
			LatencyMs:     50,
			LastChecked:   time.Now(),
			CountryCode:   "US",
			PaymentRequired: false,
			AuthRequired:  false,
		}

		err := client.SetRelayEntry(ctx, entry, time.Hour)
		if err != nil {
			t.Fatalf("SetRelayEntry() error: %v", err)
		}

		retrieved, err := client.GetRelayEntry(ctx, entry.URL)
		if err != nil {
			t.Fatalf("GetRelayEntry() error: %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetRelayEntry() returned nil")
		}

		if retrieved.URL != entry.URL {
			t.Errorf("URL = %v, want %v", retrieved.URL, entry.URL)
		}
		if retrieved.Name != entry.Name {
			t.Errorf("Name = %v, want %v", retrieved.Name, entry.Name)
		}
		if retrieved.Health != entry.Health {
			t.Errorf("Health = %v, want %v", retrieved.Health, entry.Health)
		}
		if len(retrieved.SupportedNIPs) != len(entry.SupportedNIPs) {
			t.Errorf("SupportedNIPs length = %v, want %v", len(retrieved.SupportedNIPs), len(entry.SupportedNIPs))
		}
	})

	t.Run("get non-existent relay entry", func(t *testing.T) {
		retrieved, err := client.GetRelayEntry(ctx, "wss://nonexistent.relay")
		if err != nil {
			t.Errorf("GetRelayEntry() unexpected error: %v", err)
		}
		if retrieved != nil {
			t.Errorf("GetRelayEntry() expected nil, got %v", retrieved)
		}
	})

	t.Run("store entry without country code", func(t *testing.T) {
		entry := &RelayEntry{
			URL:           "wss://relay2.example.com",
			Name:          "Relay 2",
			Health:        "degraded",
			SupportedNIPs: []int{1},
		}

		err := client.SetRelayEntry(ctx, entry, time.Hour)
		if err != nil {
			t.Fatalf("SetRelayEntry() error: %v", err)
		}

		retrieved, err := client.GetRelayEntry(ctx, entry.URL)
		if err != nil {
			t.Fatalf("GetRelayEntry() error: %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetRelayEntry() returned nil")
		}

		if retrieved.CountryCode != "" {
			t.Errorf("CountryCode = %v, want empty string", retrieved.CountryCode)
		}
	})

	t.Run("store entry with empty NIPs", func(t *testing.T) {
		entry := &RelayEntry{
			URL:           "wss://relay3.example.com",
			Name:          "Relay 3",
			Health:        "offline",
			SupportedNIPs: []int{},
		}

		err := client.SetRelayEntry(ctx, entry, time.Hour)
		if err != nil {
			t.Fatalf("SetRelayEntry() error: %v", err)
		}

		retrieved, err := client.GetRelayEntry(ctx, entry.URL)
		if err != nil {
			t.Fatalf("GetRelayEntry() error: %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetRelayEntry() returned nil")
		}
	})
}

func TestClient_GetRelaysByNIP(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	// Set up test data
	relay1 := &RelayEntry{
		URL:           "wss://relay1.example.com",
		Name:          "Relay 1",
		Health:        "online",
		SupportedNIPs: []int{1, 11, 42},
	}
	relay2 := &RelayEntry{
		URL:           "wss://relay2.example.com",
		Name:          "Relay 2",
		Health:        "online",
		SupportedNIPs: []int{1, 11},
	}
	relay3 := &RelayEntry{
		URL:           "wss://relay3.example.com",
		Name:          "Relay 3",
		Health:        "online",
		SupportedNIPs: []int{42},
	}

	client.SetRelayEntry(ctx, relay1, time.Hour)
	client.SetRelayEntry(ctx, relay2, time.Hour)
	client.SetRelayEntry(ctx, relay3, time.Hour)

	tests := []struct {
		name      string
		nip       int
		wantCount int
		wantURLs  []string
	}{
		{
			name:      "NIP supported by multiple relays",
			nip:       1,
			wantCount: 2,
			wantURLs:  []string{"wss://relay1.example.com", "wss://relay2.example.com"},
		},
		{
			name:      "NIP supported by one relay",
			nip:       42,
			wantCount: 2,
			wantURLs:  []string{"wss://relay1.example.com", "wss://relay3.example.com"},
		},
		{
			name:      "NIP not supported",
			nip:       99,
			wantCount: 0,
			wantURLs:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relays, err := client.GetRelaysByNIP(ctx, tt.nip)
			if err != nil {
				t.Fatalf("GetRelaysByNIP() error: %v", err)
			}

			if len(relays) != tt.wantCount {
				t.Errorf("GetRelaysByNIP() got %d relays, want %d", len(relays), tt.wantCount)
			}

			// Check that all returned URLs are in expected list
			for _, url := range relays {
				found := false
				for _, wantURL := range tt.wantURLs {
					if url == wantURL {
						found = true
						break
					}
				}
				if !found && tt.wantCount > 0 {
					t.Errorf("GetRelaysByNIP() returned unexpected URL: %s", url)
				}
			}
		})
	}
}

func TestClient_GetRelaysByLocation(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	// Set up test data
	relay1 := &RelayEntry{
		URL:         "wss://us-relay1.example.com",
		Name:        "US Relay 1",
		Health:      "online",
		CountryCode: "US",
	}
	relay2 := &RelayEntry{
		URL:         "wss://us-relay2.example.com",
		Name:        "US Relay 2",
		Health:      "online",
		CountryCode: "US",
	}
	relay3 := &RelayEntry{
		URL:         "wss://uk-relay.example.com",
		Name:        "UK Relay",
		Health:      "online",
		CountryCode: "UK",
	}
	relay4 := &RelayEntry{
		URL:    "wss://no-location.example.com",
		Name:   "No Location",
		Health: "online",
		// No CountryCode
	}

	client.SetRelayEntry(ctx, relay1, time.Hour)
	client.SetRelayEntry(ctx, relay2, time.Hour)
	client.SetRelayEntry(ctx, relay3, time.Hour)
	client.SetRelayEntry(ctx, relay4, time.Hour)

	tests := []struct {
		name        string
		countryCode string
		wantCount   int
		wantURLs    []string
	}{
		{
			name:        "country with multiple relays",
			countryCode: "US",
			wantCount:   2,
			wantURLs:    []string{"wss://us-relay1.example.com", "wss://us-relay2.example.com"},
		},
		{
			name:        "country with single relay",
			countryCode: "UK",
			wantCount:   1,
			wantURLs:    []string{"wss://uk-relay.example.com"},
		},
		{
			name:        "country with no relays",
			countryCode: "JP",
			wantCount:   0,
			wantURLs:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relays, err := client.GetRelaysByLocation(ctx, tt.countryCode)
			if err != nil {
				t.Fatalf("GetRelaysByLocation() error: %v", err)
			}

			if len(relays) != tt.wantCount {
				t.Errorf("GetRelaysByLocation() got %d relays, want %d", len(relays), tt.wantCount)
			}

			// Check that all returned URLs are in expected list
			for _, url := range relays {
				found := false
				for _, wantURL := range tt.wantURLs {
					if url == wantURL {
						found = true
						break
					}
				}
				if !found && tt.wantCount > 0 {
					t.Errorf("GetRelaysByLocation() returned unexpected URL: %s", url)
				}
			}
		})
	}
}

func TestClient_SetPubkeyRelay_GetPubkeyRelays(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("set and get pubkey relays", func(t *testing.T) {
		pubkey := "npub123abc"
		relay1 := "wss://relay1.example.com"
		relay2 := "wss://relay2.example.com"

		err := client.SetPubkeyRelay(ctx, pubkey, relay1, time.Hour)
		if err != nil {
			t.Fatalf("SetPubkeyRelay() error: %v", err)
		}

		err = client.SetPubkeyRelay(ctx, pubkey, relay2, time.Hour)
		if err != nil {
			t.Fatalf("SetPubkeyRelay() error: %v", err)
		}

		relays, err := client.GetPubkeyRelays(ctx, pubkey)
		if err != nil {
			t.Fatalf("GetPubkeyRelays() error: %v", err)
		}

		if len(relays) != 2 {
			t.Errorf("GetPubkeyRelays() got %d relays, want 2", len(relays))
		}

		// Check that both relays are present
		hasRelay1 := false
		hasRelay2 := false
		for _, r := range relays {
			if r == relay1 {
				hasRelay1 = true
			}
			if r == relay2 {
				hasRelay2 = true
			}
		}

		if !hasRelay1 {
			t.Error("GetPubkeyRelays() missing relay1")
		}
		if !hasRelay2 {
			t.Error("GetPubkeyRelays() missing relay2")
		}
	})

	t.Run("get relays for pubkey with no data", func(t *testing.T) {
		relays, err := client.GetPubkeyRelays(ctx, "nonexistent-pubkey")
		if err != nil {
			t.Errorf("GetPubkeyRelays() unexpected error: %v", err)
		}

		if len(relays) != 0 {
			t.Errorf("GetPubkeyRelays() got %d relays, want 0", len(relays))
		}
	})

	t.Run("add same relay twice (idempotent)", func(t *testing.T) {
		pubkey := "npub456def"
		relay := "wss://relay.example.com"

		err := client.SetPubkeyRelay(ctx, pubkey, relay, time.Hour)
		if err != nil {
			t.Fatalf("SetPubkeyRelay() error: %v", err)
		}

		err = client.SetPubkeyRelay(ctx, pubkey, relay, time.Hour)
		if err != nil {
			t.Fatalf("SetPubkeyRelay() error on second call: %v", err)
		}

		relays, err := client.GetPubkeyRelays(ctx, pubkey)
		if err != nil {
			t.Fatalf("GetPubkeyRelays() error: %v", err)
		}

		if len(relays) != 1 {
			t.Errorf("GetPubkeyRelays() got %d relays, want 1 (set should be idempotent)", len(relays))
		}
	})
}

func TestClient_SetActivity_GetActivity_ClearActivity(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("set and get activity", func(t *testing.T) {
		now := time.Now()
		activity := &Activity{
			Pubkey:    "npub789",
			Type:      "online",
			Details:   "Active on desktop",
			URL:       "",
			CreatedAt: now,
			ExpiresAt: now.Add(15 * time.Minute),
		}

		err := client.SetActivity(ctx, activity, 15*time.Minute)
		if err != nil {
			t.Fatalf("SetActivity() error: %v", err)
		}

		retrieved, err := client.GetActivity(ctx, activity.Pubkey)
		if err != nil {
			t.Fatalf("GetActivity() error: %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetActivity() returned nil")
		}

		if retrieved.Pubkey != activity.Pubkey {
			t.Errorf("Pubkey = %v, want %v", retrieved.Pubkey, activity.Pubkey)
		}
		if retrieved.Type != activity.Type {
			t.Errorf("Type = %v, want %v", retrieved.Type, activity.Type)
		}
		if retrieved.Details != activity.Details {
			t.Errorf("Details = %v, want %v", retrieved.Details, activity.Details)
		}
	})

	t.Run("get non-existent activity", func(t *testing.T) {
		retrieved, err := client.GetActivity(ctx, "nonexistent-pubkey")
		if err != nil {
			t.Errorf("GetActivity() unexpected error: %v", err)
		}
		if retrieved != nil {
			t.Errorf("GetActivity() expected nil, got %v", retrieved)
		}
	})

	t.Run("clear activity", func(t *testing.T) {
		now := time.Now()
		activity := &Activity{
			Pubkey:    "npub999",
			Type:      "writing",
			CreatedAt: now,
			ExpiresAt: now.Add(15 * time.Minute),
		}

		err := client.SetActivity(ctx, activity, 15*time.Minute)
		if err != nil {
			t.Fatalf("SetActivity() error: %v", err)
		}

		// Verify it exists
		retrieved, err := client.GetActivity(ctx, activity.Pubkey)
		if err != nil {
			t.Fatalf("GetActivity() error: %v", err)
		}
		if retrieved == nil {
			t.Fatal("GetActivity() returned nil, expected activity")
		}

		// Clear it
		err = client.ClearActivity(ctx, activity.Pubkey)
		if err != nil {
			t.Fatalf("ClearActivity() error: %v", err)
		}

		// Verify it's gone
		retrieved, err = client.GetActivity(ctx, activity.Pubkey)
		if err != nil {
			t.Fatalf("GetActivity() error after clear: %v", err)
		}
		if retrieved != nil {
			t.Errorf("GetActivity() expected nil after clear, got %v", retrieved)
		}
	})

	t.Run("streaming activity type", func(t *testing.T) {
		now := time.Now()
		activity := &Activity{
			Pubkey:    "npub_streamer",
			Type:      "streaming",
			Details:   "Live coding session",
			URL:       "https://stream.example.com/live123",
			CreatedAt: now,
			ExpiresAt: now.Add(2 * time.Hour),
		}

		err := client.SetActivity(ctx, activity, 2*time.Hour)
		if err != nil {
			t.Fatalf("SetActivity() error: %v", err)
		}

		retrieved, err := client.GetActivity(ctx, activity.Pubkey)
		if err != nil {
			t.Fatalf("GetActivity() error: %v", err)
		}

		if retrieved == nil {
			t.Fatal("GetActivity() returned nil")
		}

		if retrieved.Type != "streaming" {
			t.Errorf("Type = %v, want streaming", retrieved.Type)
		}
		if retrieved.URL != activity.URL {
			t.Errorf("URL = %v, want %v", retrieved.URL, activity.URL)
		}
	})
}

func TestClient_GetActiveStreams(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("get active streams", func(t *testing.T) {
		now := time.Now()

		stream1 := &Activity{
			Pubkey:    "streamer1",
			Type:      "streaming",
			URL:       "https://stream1.example.com",
			CreatedAt: now,
			ExpiresAt: now.Add(2 * time.Hour),
		}
		stream2 := &Activity{
			Pubkey:    "streamer2",
			Type:      "streaming",
			URL:       "https://stream2.example.com",
			CreatedAt: now,
			ExpiresAt: now.Add(2 * time.Hour),
		}
		nonStream := &Activity{
			Pubkey:    "writer1",
			Type:      "writing",
			CreatedAt: now,
			ExpiresAt: now.Add(15 * time.Minute),
		}

		client.SetActivity(ctx, stream1, 2*time.Hour)
		client.SetActivity(ctx, stream2, 2*time.Hour)
		client.SetActivity(ctx, nonStream, 15*time.Minute)

		streams, err := client.GetActiveStreams(ctx)
		if err != nil {
			t.Fatalf("GetActiveStreams() error: %v", err)
		}

		if len(streams) != 2 {
			t.Errorf("GetActiveStreams() got %d streams, want 2", len(streams))
		}

		// Check that only streaming activities are returned
		hasStreamer1 := false
		hasStreamer2 := false
		hasWriter := false

		for _, pubkey := range streams {
			if pubkey == "streamer1" {
				hasStreamer1 = true
			}
			if pubkey == "streamer2" {
				hasStreamer2 = true
			}
			if pubkey == "writer1" {
				hasWriter = true
			}
		}

		if !hasStreamer1 {
			t.Error("GetActiveStreams() missing streamer1")
		}
		if !hasStreamer2 {
			t.Error("GetActiveStreams() missing streamer2")
		}
		if hasWriter {
			t.Error("GetActiveStreams() should not include non-streaming activity")
		}
	})

	t.Run("get active streams when none exist", func(t *testing.T) {
		client2, _ := setupTestCache(t)
		defer client2.Close()

		streams, err := client2.GetActiveStreams(ctx)
		if err != nil {
			t.Errorf("GetActiveStreams() unexpected error: %v", err)
		}

		if len(streams) != 0 {
			t.Errorf("GetActiveStreams() got %d streams, want 0", len(streams))
		}
	})

	t.Run("clear streaming activity removes from active streams", func(t *testing.T) {
		now := time.Now()
		activity := &Activity{
			Pubkey:    "streamer_to_clear",
			Type:      "streaming",
			URL:       "https://stream.example.com",
			CreatedAt: now,
			ExpiresAt: now.Add(2 * time.Hour),
		}

		err := client.SetActivity(ctx, activity, 2*time.Hour)
		if err != nil {
			t.Fatalf("SetActivity() error: %v", err)
		}

		// Verify stream is active
		streams, err := client.GetActiveStreams(ctx)
		if err != nil {
			t.Fatalf("GetActiveStreams() error: %v", err)
		}

		hasStreamer := false
		for _, pk := range streams {
			if pk == activity.Pubkey {
				hasStreamer = true
				break
			}
		}
		if !hasStreamer {
			t.Error("GetActiveStreams() should include the streaming activity")
		}

		// Clear the activity
		err = client.ClearActivity(ctx, activity.Pubkey)
		if err != nil {
			t.Fatalf("ClearActivity() error: %v", err)
		}

		// Verify stream is no longer in active list
		streams, err = client.GetActiveStreams(ctx)
		if err != nil {
			t.Fatalf("GetActiveStreams() error after clear: %v", err)
		}

		for _, pk := range streams {
			if pk == activity.Pubkey {
				t.Error("GetActiveStreams() should not include cleared streaming activity")
			}
		}
	})
}

func TestRelayEntryHealthIndex(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("health index is created separately", func(t *testing.T) {
		entry := &RelayEntry{
			URL:    "wss://health-test.example.com",
			Name:   "Health Test",
			Health: "online",
		}

		err := client.SetRelayEntry(ctx, entry, time.Hour)
		if err != nil {
			t.Fatalf("SetRelayEntry() error: %v", err)
		}

		// Verify the health key exists separately
		healthKey := "relay:health:" + entry.URL
		health, err := client.rdb.Get(ctx, healthKey).Result()
		if err != nil {
			t.Fatalf("failed to get health key: %v", err)
		}

		if health != "online" {
			t.Errorf("health = %v, want online", health)
		}
	})
}

func TestInventoryMarkers(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("inventory markers are created", func(t *testing.T) {
		pubkey := "npub_inventory_test"
		relay := "wss://relay.example.com"

		err := client.SetPubkeyRelay(ctx, pubkey, relay, time.Hour)
		if err != nil {
			t.Fatalf("SetPubkeyRelay() error: %v", err)
		}

		// Verify the inventory marker exists
		invKey := "inventory:" + relay + ":" + pubkey
		val, err := client.rdb.Get(ctx, invKey).Result()
		if err != nil {
			t.Fatalf("failed to get inventory key: %v", err)
		}

		if val != "1" {
			t.Errorf("inventory marker = %v, want 1", val)
		}
	})
}

func TestWhitelist(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("add and get whitelist", func(t *testing.T) {
		err := client.AddToWhitelist(ctx, "wss://relay1.example.com")
		if err != nil {
			t.Fatalf("AddToWhitelist() error: %v", err)
		}

		err = client.AddToWhitelist(ctx, "wss://relay2.example.com")
		if err != nil {
			t.Fatalf("AddToWhitelist() error: %v", err)
		}

		whitelist, err := client.GetWhitelist(ctx)
		if err != nil {
			t.Fatalf("GetWhitelist() error: %v", err)
		}

		if len(whitelist) != 2 {
			t.Errorf("GetWhitelist() len = %v, want 2", len(whitelist))
		}
	})

	t.Run("is whitelisted", func(t *testing.T) {
		isWhitelisted, err := client.IsWhitelisted(ctx, "wss://relay1.example.com")
		if err != nil {
			t.Fatalf("IsWhitelisted() error: %v", err)
		}
		if !isWhitelisted {
			t.Error("IsWhitelisted() = false, want true")
		}

		isWhitelisted, err = client.IsWhitelisted(ctx, "wss://unknown.example.com")
		if err != nil {
			t.Fatalf("IsWhitelisted() error: %v", err)
		}
		if isWhitelisted {
			t.Error("IsWhitelisted() = true, want false")
		}
	})

	t.Run("remove from whitelist", func(t *testing.T) {
		err := client.RemoveFromWhitelist(ctx, "wss://relay1.example.com")
		if err != nil {
			t.Fatalf("RemoveFromWhitelist() error: %v", err)
		}

		isWhitelisted, _ := client.IsWhitelisted(ctx, "wss://relay1.example.com")
		if isWhitelisted {
			t.Error("IsWhitelisted() after remove = true, want false")
		}
	})
}

func TestBlacklist(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("add and get blacklist", func(t *testing.T) {
		err := client.AddToBlacklist(ctx, "wss://spam.example.com")
		if err != nil {
			t.Fatalf("AddToBlacklist() error: %v", err)
		}

		blacklist, err := client.GetBlacklist(ctx)
		if err != nil {
			t.Fatalf("GetBlacklist() error: %v", err)
		}

		if len(blacklist) != 1 {
			t.Errorf("GetBlacklist() len = %v, want 1", len(blacklist))
		}
	})

	t.Run("is blacklisted", func(t *testing.T) {
		isBlacklisted, err := client.IsBlacklisted(ctx, "wss://spam.example.com")
		if err != nil {
			t.Fatalf("IsBlacklisted() error: %v", err)
		}
		if !isBlacklisted {
			t.Error("IsBlacklisted() = false, want true")
		}
	})

	t.Run("remove from blacklist", func(t *testing.T) {
		err := client.RemoveFromBlacklist(ctx, "wss://spam.example.com")
		if err != nil {
			t.Fatalf("RemoveFromBlacklist() error: %v", err)
		}

		isBlacklisted, _ := client.IsBlacklisted(ctx, "wss://spam.example.com")
		if isBlacklisted {
			t.Error("IsBlacklisted() after remove = true, want false")
		}
	})
}

func TestTrustedPeers(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("add and get trusted peers", func(t *testing.T) {
		pubkey1 := "abc123def456"
		pubkey2 := "789xyz000111"

		err := client.AddTrustedPeer(ctx, pubkey1)
		if err != nil {
			t.Fatalf("AddTrustedPeer() error: %v", err)
		}

		err = client.AddTrustedPeer(ctx, pubkey2)
		if err != nil {
			t.Fatalf("AddTrustedPeer() error: %v", err)
		}

		peers, err := client.GetTrustedPeers(ctx)
		if err != nil {
			t.Fatalf("GetTrustedPeers() error: %v", err)
		}

		if len(peers) != 2 {
			t.Errorf("GetTrustedPeers() len = %v, want 2", len(peers))
		}
	})

	t.Run("remove trusted peer", func(t *testing.T) {
		err := client.RemoveTrustedPeer(ctx, "abc123def456")
		if err != nil {
			t.Fatalf("RemoveTrustedPeer() error: %v", err)
		}

		peers, _ := client.GetTrustedPeers(ctx)
		if len(peers) != 1 {
			t.Errorf("GetTrustedPeers() after remove len = %v, want 1", len(peers))
		}
	})
}

func TestDiscoveryDeduplication(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("mark relay seen returns true for new relay", func(t *testing.T) {
		isNew, err := client.MarkRelaySeen(ctx, "wss://new-relay.example.com")
		if err != nil {
			t.Fatalf("MarkRelaySeen() error: %v", err)
		}
		if !isNew {
			t.Error("MarkRelaySeen() = false, want true for new relay")
		}
	})

	t.Run("mark relay seen returns false for existing relay", func(t *testing.T) {
		isNew, err := client.MarkRelaySeen(ctx, "wss://new-relay.example.com")
		if err != nil {
			t.Fatalf("MarkRelaySeen() error: %v", err)
		}
		if isNew {
			t.Error("MarkRelaySeen() = true, want false for existing relay")
		}
	})

	t.Run("get seen relays", func(t *testing.T) {
		client.MarkRelaySeen(ctx, "wss://another-relay.example.com")

		seen, err := client.GetSeenRelays(ctx)
		if err != nil {
			t.Fatalf("GetSeenRelays() error: %v", err)
		}
		if len(seen) != 2 {
			t.Errorf("GetSeenRelays() len = %v, want 2", len(seen))
		}
	})

	t.Run("clear seen relays", func(t *testing.T) {
		err := client.ClearSeenRelays(ctx)
		if err != nil {
			t.Fatalf("ClearSeenRelays() error: %v", err)
		}

		seen, _ := client.GetSeenRelays(ctx)
		if len(seen) != 0 {
			t.Errorf("GetSeenRelays() after clear len = %v, want 0", len(seen))
		}
	})
}

// TTL Expiration Tests

func TestRelayEntryTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	entry := &RelayEntry{
		URL:    "wss://ttl-test.example.com",
		Name:   "TTL Test Relay",
		Health: "online",
	}

	ttl := 10 * time.Second

	err := client.SetRelayEntry(ctx, entry, ttl)
	if err != nil {
		t.Fatalf("SetRelayEntry() error: %v", err)
	}

	// Verify entry exists
	retrieved, err := client.GetRelayEntry(ctx, entry.URL)
	if err != nil {
		t.Fatalf("GetRelayEntry() error: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetRelayEntry() returned nil before TTL expiry")
	}

	// Fast forward past TTL
	mr.FastForward(11 * time.Second)

	// Verify entry is expired
	retrieved, err = client.GetRelayEntry(ctx, entry.URL)
	if err != nil {
		t.Fatalf("GetRelayEntry() error after TTL: %v", err)
	}
	if retrieved != nil {
		t.Error("GetRelayEntry() should return nil after TTL expiry")
	}
}

func TestHealthKeyTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	entry := &RelayEntry{
		URL:    "wss://health-ttl-test.example.com",
		Name:   "Health TTL Test",
		Health: "online",
	}

	// Set with a long TTL for the main entry
	err := client.SetRelayEntry(ctx, entry, time.Hour)
	if err != nil {
		t.Fatalf("SetRelayEntry() error: %v", err)
	}

	// Verify health key exists
	healthKey := "relay:health:" + entry.URL
	health, err := client.rdb.Get(ctx, healthKey).Result()
	if err != nil {
		t.Fatalf("Get health key error: %v", err)
	}
	if health != "online" {
		t.Errorf("health = %v, want online", health)
	}

	// Health key has 5 minute TTL, fast forward past that
	mr.FastForward(6 * time.Minute)

	// Health key should be expired
	_, err = client.rdb.Get(ctx, healthKey).Result()
	if err == nil {
		t.Error("health key should be expired after 5 minutes")
	}

	// Main entry should still exist (1 hour TTL)
	retrieved, err := client.GetRelayEntry(ctx, entry.URL)
	if err != nil {
		t.Fatalf("GetRelayEntry() error: %v", err)
	}
	if retrieved == nil {
		t.Error("main relay entry should still exist")
	}
}

func TestNIPIndexTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	entry := &RelayEntry{
		URL:           "wss://nip-ttl-test.example.com",
		Name:          "NIP TTL Test",
		Health:        "online",
		SupportedNIPs: []int{1, 11, 42},
	}

	ttl := 30 * time.Second

	err := client.SetRelayEntry(ctx, entry, ttl)
	if err != nil {
		t.Fatalf("SetRelayEntry() error: %v", err)
	}

	// Verify NIP index contains the relay
	relays, err := client.GetRelaysByNIP(ctx, 42)
	if err != nil {
		t.Fatalf("GetRelaysByNIP() error: %v", err)
	}
	if len(relays) != 1 {
		t.Errorf("GetRelaysByNIP() returned %d relays, want 1", len(relays))
	}

	// Fast forward past TTL
	mr.FastForward(31 * time.Second)

	// NIP index should be empty after expiration
	relays, err = client.GetRelaysByNIP(ctx, 42)
	if err != nil {
		t.Fatalf("GetRelaysByNIP() error after TTL: %v", err)
	}
	if len(relays) != 0 {
		t.Errorf("GetRelaysByNIP() returned %d relays after TTL, want 0", len(relays))
	}
}

func TestLocationIndexTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	entry := &RelayEntry{
		URL:         "wss://location-ttl-test.example.com",
		Name:        "Location TTL Test",
		Health:      "online",
		CountryCode: "US",
	}

	ttl := 20 * time.Second

	err := client.SetRelayEntry(ctx, entry, ttl)
	if err != nil {
		t.Fatalf("SetRelayEntry() error: %v", err)
	}

	// Verify location index contains the relay
	relays, err := client.GetRelaysByLocation(ctx, "US")
	if err != nil {
		t.Fatalf("GetRelaysByLocation() error: %v", err)
	}
	if len(relays) != 1 {
		t.Errorf("GetRelaysByLocation() returned %d relays, want 1", len(relays))
	}

	// Fast forward past TTL
	mr.FastForward(21 * time.Second)

	// Location index should be empty after expiration
	relays, err = client.GetRelaysByLocation(ctx, "US")
	if err != nil {
		t.Fatalf("GetRelaysByLocation() error after TTL: %v", err)
	}
	if len(relays) != 0 {
		t.Errorf("GetRelaysByLocation() returned %d relays after TTL, want 0", len(relays))
	}
}

func TestActivityTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	now := time.Now()
	activity := &Activity{
		Pubkey:    "ttl-test-pubkey",
		Type:      "streaming",
		Details:   "Test stream",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Minute),
	}

	ttl := 15 * time.Second

	err := client.SetActivity(ctx, activity, ttl)
	if err != nil {
		t.Fatalf("SetActivity() error: %v", err)
	}

	// Verify activity exists
	retrieved, err := client.GetActivity(ctx, activity.Pubkey)
	if err != nil {
		t.Fatalf("GetActivity() error: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetActivity() returned nil before TTL expiry")
	}

	// Verify in active streams
	streams, err := client.GetActiveStreams(ctx)
	if err != nil {
		t.Fatalf("GetActiveStreams() error: %v", err)
	}
	found := false
	for _, pk := range streams {
		if pk == activity.Pubkey {
			found = true
			break
		}
	}
	if !found {
		t.Error("activity should be in active streams before TTL expiry")
	}

	// Fast forward past TTL
	mr.FastForward(16 * time.Second)

	// Activity should be expired
	retrieved, err = client.GetActivity(ctx, activity.Pubkey)
	if err != nil {
		t.Fatalf("GetActivity() error after TTL: %v", err)
	}
	if retrieved != nil {
		t.Error("GetActivity() should return nil after TTL expiry")
	}

	// Active streams set should also expire (has same TTL)
	streams, err = client.GetActiveStreams(ctx)
	if err != nil {
		t.Fatalf("GetActiveStreams() error after TTL: %v", err)
	}
	for _, pk := range streams {
		if pk == activity.Pubkey {
			t.Error("activity should not be in active streams after TTL expiry")
		}
	}
}

func TestPubkeyRelayTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	pubkey := "ttl-test-pk"
	relay := "wss://relay-ttl.example.com"
	ttl := 10 * time.Second

	err := client.SetPubkeyRelay(ctx, pubkey, relay, ttl)
	if err != nil {
		t.Fatalf("SetPubkeyRelay() error: %v", err)
	}

	// Verify relay exists
	relays, err := client.GetPubkeyRelays(ctx, pubkey)
	if err != nil {
		t.Fatalf("GetPubkeyRelays() error: %v", err)
	}
	if len(relays) != 1 {
		t.Errorf("GetPubkeyRelays() returned %d relays, want 1", len(relays))
	}

	// Fast forward past TTL
	mr.FastForward(11 * time.Second)

	// Pubkey relays should be empty after expiration
	relays, err = client.GetPubkeyRelays(ctx, pubkey)
	if err != nil {
		t.Fatalf("GetPubkeyRelays() error after TTL: %v", err)
	}
	if len(relays) != 0 {
		t.Errorf("GetPubkeyRelays() returned %d relays after TTL, want 0", len(relays))
	}

	// Inventory marker should also be expired
	invKey := "inventory:" + relay + ":" + pubkey
	_, err = client.rdb.Get(ctx, invKey).Result()
	if err == nil {
		t.Error("inventory marker should be expired after TTL")
	}
}

func TestModerationIndexTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	entry := &RelayEntry{
		URL:        "wss://moderation-ttl.example.com",
		Name:       "Moderation TTL Test",
		Health:     "online",
		Moderation: "strict",
	}

	ttl := 15 * time.Second

	err := client.SetRelayEntry(ctx, entry, ttl)
	if err != nil {
		t.Fatalf("SetRelayEntry() error: %v", err)
	}

	// Verify index contains the relay
	relays, err := client.GetRelaysByModeration(ctx, "strict")
	if err != nil {
		t.Fatalf("GetRelaysByModeration() error: %v", err)
	}
	if len(relays) != 1 {
		t.Errorf("GetRelaysByModeration() returned %d relays, want 1", len(relays))
	}

	// Fast forward past TTL
	mr.FastForward(16 * time.Second)

	// Index should be empty after expiration
	relays, err = client.GetRelaysByModeration(ctx, "strict")
	if err != nil {
		t.Fatalf("GetRelaysByModeration() error after TTL: %v", err)
	}
	if len(relays) != 0 {
		t.Errorf("GetRelaysByModeration() returned %d relays after TTL, want 0", len(relays))
	}
}

func TestContentPolicyIndexTTLExpiration(t *testing.T) {
	client, mr := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	entry := &RelayEntry{
		URL:           "wss://content-policy-ttl.example.com",
		Name:          "Content Policy TTL Test",
		Health:        "online",
		ContentPolicy: "sfw",
	}

	ttl := 15 * time.Second

	err := client.SetRelayEntry(ctx, entry, ttl)
	if err != nil {
		t.Fatalf("SetRelayEntry() error: %v", err)
	}

	// Verify index contains the relay
	relays, err := client.GetRelaysByContentPolicy(ctx, "sfw")
	if err != nil {
		t.Fatalf("GetRelaysByContentPolicy() error: %v", err)
	}
	if len(relays) != 1 {
		t.Errorf("GetRelaysByContentPolicy() returned %d relays, want 1", len(relays))
	}

	// Fast forward past TTL
	mr.FastForward(16 * time.Second)

	// Index should be empty after expiration
	relays, err = client.GetRelaysByContentPolicy(ctx, "sfw")
	if err != nil {
		t.Fatalf("GetRelaysByContentPolicy() error after TTL: %v", err)
	}
	if len(relays) != 0 {
		t.Errorf("GetRelaysByContentPolicy() returned %d relays after TTL, want 0", len(relays))
	}
}

func TestStats(t *testing.T) {
	client, _ := setupTestCache(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("increment and get stat", func(t *testing.T) {
		err := client.IncrementStat(ctx, "relays:total")
		if err != nil {
			t.Fatalf("IncrementStat() error: %v", err)
		}

		err = client.IncrementStat(ctx, "relays:total")
		if err != nil {
			t.Fatalf("IncrementStat() error: %v", err)
		}

		val, err := client.GetStat(ctx, "relays:total")
		if err != nil {
			t.Fatalf("GetStat() error: %v", err)
		}
		if val != 2 {
			t.Errorf("GetStat() = %v, want 2", val)
		}
	})

	t.Run("decrement stat", func(t *testing.T) {
		err := client.DecrementStat(ctx, "relays:total")
		if err != nil {
			t.Fatalf("DecrementStat() error: %v", err)
		}

		val, _ := client.GetStat(ctx, "relays:total")
		if val != 1 {
			t.Errorf("GetStat() after decrement = %v, want 1", val)
		}
	})

	t.Run("set stat", func(t *testing.T) {
		err := client.SetStat(ctx, "relays:online", 100)
		if err != nil {
			t.Fatalf("SetStat() error: %v", err)
		}

		val, _ := client.GetStat(ctx, "relays:online")
		if val != 100 {
			t.Errorf("GetStat() = %v, want 100", val)
		}
	})

	t.Run("get non-existent stat returns 0", func(t *testing.T) {
		val, err := client.GetStat(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetStat() error: %v", err)
		}
		if val != 0 {
			t.Errorf("GetStat() for nonexistent = %v, want 0", val)
		}
	})

	t.Run("get all stats", func(t *testing.T) {
		client.SetStat(ctx, "discovery:nip65", 50)
		client.SetStat(ctx, "discovery:nip66", 25)

		stats, err := client.GetAllStats(ctx)
		if err != nil {
			t.Fatalf("GetAllStats() error: %v", err)
		}

		if len(stats) < 3 {
			t.Errorf("GetAllStats() len = %v, want >= 3", len(stats))
		}
	})
}
