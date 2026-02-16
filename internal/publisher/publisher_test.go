package publisher

import (
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/nbd-wtf/go-nostr"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// setupTestPublisher creates a test publisher with miniredis cache.
func setupTestPublisher(t *testing.T, cfg *config.Config) (*Publisher, *miniredis.Miniredis, *cache.Client) {
	t.Helper()

	mr := miniredis.RunT(t)

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("Failed to create cache client: %v", err)
	}

	if cfg == nil {
		cfg = &config.Config{
			PublishEnabled:  true,
			PublishRelays:   []string{"wss://relay.example.com"},
			PublishInterval: 10,
			PrivateKey:      "",
		}
	}

	publisher, err := New(cfg, cacheClient)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	return publisher, mr, cacheClient
}

func TestNew(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cacheClient, _ := cache.New("redis://" + mr.Addr())
	defer cacheClient.Close()

	tests := []struct {
		name       string
		privateKey string
		wantErr    bool
		wantPubkey bool
	}{
		{
			name:       "valid hex private key",
			privateKey: "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
			wantErr:    false,
			wantPubkey: true,
		},
		{
			name:       "valid nsec private key",
			privateKey: "nsec1vl029mgpspedva04g90vltkh6fvh240zqtv9k0t9af8935ke9laqsnlfe5",
			wantErr:    false,
			wantPubkey: true,
		},
		{
			name:       "no private key",
			privateKey: "",
			wantErr:    false,
			wantPubkey: false,
		},
		{
			name:       "invalid hex key (too short)",
			privateKey: "abc123",
			wantErr:    true,
			wantPubkey: false,
		},
		{
			name:       "invalid hex key (not hex)",
			privateKey: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			wantErr:    true,
			wantPubkey: false,
		},
		{
			name:       "invalid nsec key",
			privateKey: "nsec1invalid",
			wantErr:    true,
			wantPubkey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				PublishEnabled:  true,
				PublishRelays:   []string{"wss://relay.example.com"},
				PublishInterval: 10,
				PrivateKey:      tt.privateKey,
			}

			publisher, err := New(cfg, cacheClient)

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

			if publisher == nil {
				t.Fatal("New() returned nil publisher")
			}

			if tt.wantPubkey {
				if publisher.pk == "" {
					t.Error("New() expected public key, got empty string")
				}
				if publisher.sk == "" {
					t.Error("New() expected private key to be set, got empty string")
				}
				if len(publisher.pk) != 64 {
					t.Errorf("New() public key length = %d, want 64", len(publisher.pk))
				}
			} else {
				if publisher.pk != "" {
					t.Error("New() expected no public key, got non-empty string")
				}
				if publisher.sk != "" {
					t.Error("New() expected no private key, got non-empty string")
				}
			}

			if publisher.cfg != cfg {
				t.Error("New() config not set correctly")
			}

			if publisher.cache != cacheClient {
				t.Error("New() cache not set correctly")
			}
		})
	}
}

func TestParsePrivateKey(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantLen   int
		checkHex  bool
	}{
		{
			name:     "valid hex key (64 chars)",
			input:    "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
			wantErr:  false,
			wantLen:  64,
			checkHex: true,
		},
		{
			name:     "valid hex key with whitespace",
			input:    "  a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890  ",
			wantErr:  false,
			wantLen:  64,
			checkHex: true,
		},
		{
			name:     "valid nsec key",
			input:    "nsec1vl029mgpspedva04g90vltkh6fvh240zqtv9k0t9af8935ke9laqsnlfe5",
			wantErr:  false,
			wantLen:  64,
			checkHex: true,
		},
		{
			name:     "nsec key with whitespace",
			input:    "  nsec1vl029mgpspedva04g90vltkh6fvh240zqtv9k0t9af8935ke9laqsnlfe5  ",
			wantErr:  false,
			wantLen:  64,
			checkHex: true,
		},
		{
			name:    "hex key too short",
			input:   "a1b2c3d4e5f67890",
			wantErr: true,
		},
		{
			name:    "hex key too long",
			input:   "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890abc",
			wantErr: true,
		},
		{
			name:    "invalid hex characters",
			input:   "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			wantErr: true,
		},
		{
			name:    "invalid nsec format",
			input:   "nsec1tooshort",
			wantErr: true,
		},
		{
			name:    "invalid nsec checksum",
			input:   "nsec1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePrivateKey(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("parsePrivateKey() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parsePrivateKey() unexpected error: %v", err)
				return
			}

			if len(result) != tt.wantLen {
				t.Errorf("parsePrivateKey() result length = %d, want %d", len(result), tt.wantLen)
			}

			if tt.checkHex {
				// Verify it's valid hex
				for _, r := range result {
					if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
						t.Errorf("parsePrivateKey() result contains non-hex character: %c", r)
						break
					}
				}
			}
		})
	}
}

func TestCreateEvent(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	tests := []struct {
		name       string
		entry      *cache.RelayEntry
		checkTags  map[string][]string
		checkCount int
	}{
		{
			name: "minimal relay entry",
			entry: &cache.RelayEntry{
				URL:         "wss://minimal.example.com",
				Health:      "online",
				LastChecked: time.Now(),
			},
			checkTags: map[string][]string{
				"d":            {"wss://minimal.example.com"},
				"relay":        {"wss://minimal.example.com"},
				"health":       {"online"},
				"payment":      {"free"},
				"admission":    {"open"},
			},
			checkCount: 7, // d, relay, health, last_checked, payment, admission, expires
		},
		{
			name: "full relay entry",
			entry: &cache.RelayEntry{
				URL:             "wss://full.example.com",
				Name:            "Full Relay",
				Description:     "A fully featured test relay",
				Pubkey:          "pubkey123",
				SupportedNIPs:   []int{1, 11, 42},
				Software:        "nostr-rs-relay",
				Version:         "0.8.0",
				Health:          "online",
				LatencyMs:       50,
				LastChecked:     time.Now(),
				CountryCode:     "US",
				PaymentRequired: true,
				AuthRequired:    true,
				ContentPolicy:   "sfw",
				Moderation:      "strict",
				ModerationPolicy: "https://example.com/rules",
				Community:       "Bitcoin",
				Languages:       []string{"en", "es"},
			},
			checkTags: map[string][]string{
				"d":                 {"wss://full.example.com"},
				"relay":             {"wss://full.example.com"},
				"health":            {"online"},
				"name":              {"Full Relay"},
				"description":       {"A fully featured test relay"},
				"operator":          {"pubkey123"},
				"software":          {"nostr-rs-relay", "0.8.0"},
				"nips":              {"1", "11", "42"},
				"latency_ms":        {"50"},
				"location":          {"US"},
				"payment":           {"paid"},
				"admission":         {"auth"},
				"content_policy":    {"sfw"},
				"moderation":        {"strict"},
				"moderation_policy": {"https://example.com/rules"},
				"community":         {"Bitcoin"},
			},
			checkCount: 19, // All above + last_checked + expires + 2 language tags
		},
		{
			name: "relay with topics and atmosphere",
			entry: &cache.RelayEntry{
				URL:         "wss://community.example.com",
				Health:      "online",
				LastChecked: time.Now(),
				Topics: map[string]int{
					"bitcoin":   5,
					"lightning": 3,
				},
				Atmosphere: map[string]int{
					"chill":  10,
					"active": 7,
				},
			},
			checkTags: map[string][]string{
				"d":      {"wss://community.example.com"},
				"relay":  {"wss://community.example.com"},
				"health": {"online"},
			},
			checkCount: 11, // d, relay, health, last_checked, payment, admission, expires + 2 topics + 2 atmosphere
		},
		{
			name: "relay with software but no version",
			entry: &cache.RelayEntry{
				URL:         "wss://noversion.example.com",
				Health:      "degraded",
				LastChecked: time.Now(),
				Software:    "custom-relay",
			},
			checkTags: map[string][]string{
				"d":        {"wss://noversion.example.com"},
				"relay":    {"wss://noversion.example.com"},
				"health":   {"degraded"},
				"software": {"custom-relay"},
			},
			checkCount: 8, // d, relay, health, last_checked, software, payment, admission, expires
		},
		{
			name: "offline relay",
			entry: &cache.RelayEntry{
				URL:         "wss://offline.example.com",
				Health:      "offline",
				LastChecked: time.Now(),
			},
			checkTags: map[string][]string{
				"d":      {"wss://offline.example.com"},
				"relay":  {"wss://offline.example.com"},
				"health": {"offline"},
			},
			checkCount: 7, // d, relay, health, last_checked, payment, admission, expires
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := publisher.createEvent(tt.entry)

			if event == nil {
				t.Fatal("createEvent() returned nil event")
			}

			if event.Kind != 30072 {
				t.Errorf("event.Kind = %d, want 30072", event.Kind)
			}

			if event.PubKey != publisher.pk {
				t.Errorf("event.PubKey = %s, want %s", event.PubKey, publisher.pk)
			}

			if event.Content != "" {
				t.Errorf("event.Content = %s, want empty string", event.Content)
			}

			if len(event.Tags) < tt.checkCount {
				t.Errorf("event.Tags count = %d, want at least %d", len(event.Tags), tt.checkCount)
			}

			// Check specific tags
			for tagName, expectedValues := range tt.checkTags {
				found := false
				for _, tag := range event.Tags {
					if len(tag) > 0 && tag[0] == tagName {
						found = true
						// Check values match
						if len(tag)-1 != len(expectedValues) {
							t.Errorf("tag %s has %d values, want %d", tagName, len(tag)-1, len(expectedValues))
						}
						for i, expected := range expectedValues {
							if i+1 < len(tag) && tag[i+1] != expected {
								t.Errorf("tag %s value[%d] = %s, want %s", tagName, i, tag[i+1], expected)
							}
						}
						break
					}
				}
				if !found {
					t.Errorf("createEvent() missing tag: %s", tagName)
				}
			}

			// Verify expires tag is set
			hasExpires := false
			for _, tag := range event.Tags {
				if len(tag) > 0 && tag[0] == "expires" {
					hasExpires = true
					break
				}
			}
			if !hasExpires {
				t.Error("createEvent() missing expires tag")
			}

			// Verify last_checked tag is set
			hasLastChecked := false
			for _, tag := range event.Tags {
				if len(tag) > 0 && tag[0] == "last_checked" {
					hasLastChecked = true
					break
				}
			}
			if !hasLastChecked {
				t.Error("createEvent() missing last_checked tag")
			}

			// Verify event is signed
			if event.Sig == "" {
				t.Error("createEvent() event is not signed")
			}

			// Verify signature is valid
			ok, err := event.CheckSignature()
			if err != nil {
				t.Errorf("createEvent() signature check error: %v", err)
			}
			if !ok {
				t.Error("createEvent() invalid signature")
			}
		})
	}
}

func TestCreateEvent_LanguageTags(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:         "wss://multilang.example.com",
		Health:      "online",
		LastChecked: time.Now(),
		Languages:   []string{"en", "es", "fr"},
	}

	event := publisher.createEvent(entry)

	// Count language tags
	langCount := 0
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "language" {
			langCount++
		}
	}

	if langCount != 3 {
		t.Errorf("createEvent() language tag count = %d, want 3", langCount)
	}
}

func TestCreateEvent_TopicsAndAtmosphere(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:         "wss://annotated.example.com",
		Health:      "online",
		LastChecked: time.Now(),
		Topics: map[string]int{
			"bitcoin": 10,
			"nostr":   5,
		},
		Atmosphere: map[string]int{
			"chill":  8,
			"active": 12,
		},
	}

	event := publisher.createEvent(entry)

	// Count topic tags
	topicCount := 0
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "topic" {
			topicCount++
			// Verify count is present
			if len(tag) < 3 {
				t.Error("topic tag should have topic name and count")
			}
		}
	}

	if topicCount != 2 {
		t.Errorf("createEvent() topic tag count = %d, want 2", topicCount)
	}

	// Count atmosphere tags
	atmCount := 0
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "atmosphere" {
			atmCount++
			// Verify count is present
			if len(tag) < 3 {
				t.Error("atmosphere tag should have atmosphere name and count")
			}
		}
	}

	if atmCount != 2 {
		t.Errorf("createEvent() atmosphere tag count = %d, want 2", atmCount)
	}
}

func TestGetPublicKey(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
		expectKey  bool
	}{
		{
			name:       "with private key",
			privateKey: "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
			expectKey:  true,
		},
		{
			name:       "without private key",
			privateKey: "",
			expectKey:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				PublishEnabled:  true,
				PublishRelays:   []string{"wss://relay.example.com"},
				PublishInterval: 10,
				PrivateKey:      tt.privateKey,
			}

			publisher, _, cacheClient := setupTestPublisher(t, cfg)
			defer cacheClient.Close()

			pk := publisher.GetPublicKey()

			if tt.expectKey {
				if pk == "" {
					t.Error("GetPublicKey() returned empty string, expected public key")
				}
				if len(pk) != 64 {
					t.Errorf("GetPublicKey() length = %d, want 64", len(pk))
				}
			} else {
				if pk != "" {
					t.Errorf("GetPublicKey() = %s, want empty string", pk)
				}
			}
		})
	}
}

func TestGetLastPublish(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	t.Run("initial value is zero time", func(t *testing.T) {
		lastPublish := publisher.GetLastPublish()
		if !lastPublish.IsZero() {
			t.Errorf("GetLastPublish() = %v, want zero time", lastPublish)
		}
	})

	t.Run("returns updated value", func(t *testing.T) {
		now := time.Now()
		publisher.mu.Lock()
		publisher.lastPublish = now
		publisher.mu.Unlock()

		lastPublish := publisher.GetLastPublish()
		if lastPublish.IsZero() {
			t.Error("GetLastPublish() returned zero time after update")
		}

		// Allow for small time difference
		if lastPublish.Sub(now).Abs() > time.Second {
			t.Errorf("GetLastPublish() = %v, want approximately %v", lastPublish, now)
		}
	})
}

func TestGetPublishCount(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	t.Run("initial value is zero", func(t *testing.T) {
		count := publisher.GetPublishCount()
		if count != 0 {
			t.Errorf("GetPublishCount() = %d, want 0", count)
		}
	})

	t.Run("returns incremented value", func(t *testing.T) {
		publisher.mu.Lock()
		publisher.publishCount = 5
		publisher.mu.Unlock()

		count := publisher.GetPublishCount()
		if count != 5 {
			t.Errorf("GetPublishCount() = %d, want 5", count)
		}
	})
}

func TestGetRelaysPublished(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	t.Run("initial value is zero", func(t *testing.T) {
		count := publisher.GetRelaysPublished()
		if count != 0 {
			t.Errorf("GetRelaysPublished() = %d, want 0", count)
		}
	})

	t.Run("returns updated value", func(t *testing.T) {
		publisher.mu.Lock()
		publisher.relaysPublished = 42
		publisher.mu.Unlock()

		count := publisher.GetRelaysPublished()
		if count != 42 {
			t.Errorf("GetRelaysPublished() = %d, want 42", count)
		}
	})
}

func TestPublisher_ConcurrentAccess(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	// Test concurrent access to getter methods
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				publisher.GetPublicKey()
				publisher.GetLastPublish()
				publisher.GetPublishCount()
				publisher.GetRelaysPublished()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic
}

func TestPublisher_CreateEventSignatureValidity(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:         "wss://test.example.com",
		Health:      "online",
		LastChecked: time.Now(),
	}

	// Create multiple events and verify all signatures are valid
	for i := 0; i < 10; i++ {
		event := publisher.createEvent(entry)

		ok, err := event.CheckSignature()
		if err != nil {
			t.Errorf("iteration %d: signature check error: %v", i, err)
		}
		if !ok {
			t.Errorf("iteration %d: invalid signature", i)
		}

		// Verify pubkey matches
		if event.PubKey != publisher.pk {
			t.Errorf("iteration %d: event pubkey = %s, want %s", i, event.PubKey, publisher.pk)
		}
	}
}

func TestPublisher_CreateEventExpiration(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 15, // 15 minutes
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:         "wss://test.example.com",
		Health:      "online",
		LastChecked: time.Now(),
	}

	event := publisher.createEvent(entry)

	// Find expires tag
	var expiresValue string
	for _, tag := range event.Tags {
		if len(tag) > 1 && tag[0] == "expires" {
			expiresValue = tag[1]
			break
		}
	}

	if expiresValue == "" {
		t.Fatal("createEvent() missing expires tag")
	}

	// Parse expires timestamp
	var expiresTime int64
	_, err := fmt.Sscanf(expiresValue, "%d", &expiresTime)
	if err != nil {
		t.Fatalf("failed to parse expires timestamp: %v", err)
	}

	// Verify expires is approximately 2 * PublishInterval from now
	expectedExpires := time.Now().Add(time.Duration(cfg.PublishInterval*2) * time.Minute)
	actualExpires := time.Unix(expiresTime, 0)

	diff := actualExpires.Sub(expectedExpires).Abs()
	if diff > time.Minute {
		t.Errorf("expires time difference = %v, want < 1 minute", diff)
	}
}

func TestPublisher_NIPTagFormatting(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:           "wss://test.example.com",
		Health:        "online",
		LastChecked:   time.Now(),
		SupportedNIPs: []int{1, 11, 42, 50, 65},
	}

	event := publisher.createEvent(entry)

	// Find nips tag
	var nipsTag nostr.Tag
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "nips" {
			nipsTag = tag
			break
		}
	}

	if nipsTag == nil {
		t.Fatal("createEvent() missing nips tag")
	}

	// Verify format: ["nips", "1", "11", "42", "50", "65"]
	expectedNIPs := []string{"nips", "1", "11", "42", "50", "65"}
	if len(nipsTag) != len(expectedNIPs) {
		t.Errorf("nips tag length = %d, want %d", len(nipsTag), len(expectedNIPs))
	}

	for i, expected := range expectedNIPs {
		if i < len(nipsTag) && nipsTag[i] != expected {
			t.Errorf("nips tag[%d] = %s, want %s", i, nipsTag[i], expected)
		}
	}
}

func TestPublisher_EmptyNIPsHandling(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:           "wss://test.example.com",
		Health:        "online",
		LastChecked:   time.Now(),
		SupportedNIPs: []int{}, // Empty NIPs
	}

	event := publisher.createEvent(entry)

	// Verify no nips tag is added when SupportedNIPs is empty
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "nips" {
			t.Error("createEvent() should not add nips tag when SupportedNIPs is empty")
		}
	}
}

func TestPublisher_ZeroLatencyHandling(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	entry := &cache.RelayEntry{
		URL:         "wss://test.example.com",
		Health:      "online",
		LastChecked: time.Now(),
		LatencyMs:   0, // Zero latency
	}

	event := publisher.createEvent(entry)

	// Verify no latency_ms tag is added when LatencyMs is 0
	for _, tag := range event.Tags {
		if len(tag) > 0 && tag[0] == "latency_ms" {
			t.Error("createEvent() should not add latency_ms tag when LatencyMs is 0")
		}
	}
}

func TestPublisher_PaymentAndAdmissionTags(t *testing.T) {
	cfg := &config.Config{
		PublishEnabled:  true,
		PublishRelays:   []string{"wss://relay.example.com"},
		PublishInterval: 10,
		PrivateKey:      "a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890a1b2c3d4e5f67890",
	}

	publisher, _, cacheClient := setupTestPublisher(t, cfg)
	defer cacheClient.Close()

	tests := []struct {
		name            string
		paymentRequired bool
		authRequired    bool
		wantPayment     string
		wantAdmission   string
	}{
		{
			name:            "free and open",
			paymentRequired: false,
			authRequired:    false,
			wantPayment:     "free",
			wantAdmission:   "open",
		},
		{
			name:            "paid and open",
			paymentRequired: true,
			authRequired:    false,
			wantPayment:     "paid",
			wantAdmission:   "open",
		},
		{
			name:            "free and auth",
			paymentRequired: false,
			authRequired:    true,
			wantPayment:     "free",
			wantAdmission:   "auth",
		},
		{
			name:            "paid and auth",
			paymentRequired: true,
			authRequired:    true,
			wantPayment:     "paid",
			wantAdmission:   "auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &cache.RelayEntry{
				URL:             "wss://test.example.com",
				Health:          "online",
				LastChecked:     time.Now(),
				PaymentRequired: tt.paymentRequired,
				AuthRequired:    tt.authRequired,
			}

			event := publisher.createEvent(entry)

			// Find payment tag
			var paymentValue string
			for _, tag := range event.Tags {
				if len(tag) > 1 && tag[0] == "payment" {
					paymentValue = tag[1]
					break
				}
			}

			if paymentValue != tt.wantPayment {
				t.Errorf("payment tag value = %s, want %s", paymentValue, tt.wantPayment)
			}

			// Find admission tag
			var admissionValue string
			for _, tag := range event.Tags {
				if len(tag) > 1 && tag[0] == "admission" {
					admissionValue = tag[1]
					break
				}
			}

			if admissionValue != tt.wantAdmission {
				t.Errorf("admission tag value = %s, want %s", admissionValue, tt.wantAdmission)
			}
		})
	}
}
