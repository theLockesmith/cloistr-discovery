package publisher

import (
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/config"
)

func TestDetectNetwork(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"wss://relay.damus.io", "clearnet"},
		{"wss://example.onion", "tor"},
		{"wss://relay.example.i2p", "i2p"},
		{"wss://relay.example.loki", "loki"},
		{"wss://RELAY.ONION.EXAMPLE.COM", "tor"}, // Case insensitive
		{"ws://localhost:8080", "clearnet"},
	}

	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			result := detectNetwork(tc.url)
			if result != tc.expected {
				t.Errorf("detectNetwork(%q) = %q, want %q", tc.url, result, tc.expected)
			}
		})
	}
}

func TestCreateAnnouncementEvent(t *testing.T) {
	cfg := &config.Config{
		NIP66PublishInterval: 3600,
		NIP11Timeout:         10,
	}

	// Generate test keys
	sk := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	publisher := &NIP66Publisher{
		cfg: cfg,
		sk:  sk,
		pk:  pk,
	}

	event := publisher.createAnnouncementEvent()

	// Verify event kind
	if event.Kind != 10166 {
		t.Errorf("event.Kind = %d, want 10166", event.Kind)
	}

	// Verify pubkey
	if event.PubKey != pk {
		t.Errorf("event.PubKey = %q, want %q", event.PubKey, pk)
	}

	// Verify frequency tag
	var foundFrequency bool
	for _, tag := range event.Tags {
		if tag[0] == "frequency" {
			foundFrequency = true
			if tag[1] != "3600" {
				t.Errorf("frequency tag = %q, want %q", tag[1], "3600")
			}
		}
	}
	if !foundFrequency {
		t.Error("missing frequency tag")
	}

	// Verify check type tags
	checkTypes := make(map[string]bool)
	for _, tag := range event.Tags {
		if tag[0] == "c" {
			checkTypes[tag[1]] = true
		}
	}
	for _, expected := range []string{"open", "read", "nip11"} {
		if !checkTypes[expected] {
			t.Errorf("missing check type tag: %s", expected)
		}
	}

	// Verify signature is valid
	valid, err := event.CheckSignature()
	if err != nil {
		t.Errorf("signature check error: %v", err)
	}
	if !valid {
		t.Error("invalid signature")
	}
}

func TestCreateRelayStatusEvent(t *testing.T) {
	cfg := &config.Config{
		NIP66PublishInterval: 3600,
	}

	// Generate test keys
	sk := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	publisher := &NIP66Publisher{
		cfg: cfg,
		sk:  sk,
		pk:  pk,
	}

	entry := &cache.RelayEntry{
		URL:           "wss://relay.example.com",
		Name:          "Example Relay",
		Description:   "A test relay",
		Health:        "online",
		LatencyMs:     150,
		SupportedNIPs: []int{1, 11, 42},
		AuthRequired:  true,
		PaymentRequired: false,
		Software:      "strfry",
		Version:       "1.0.0",
		LastChecked:   time.Now(),
	}

	event := publisher.createRelayStatusEvent(entry)

	// Verify event kind
	if event.Kind != 30166 {
		t.Errorf("event.Kind = %d, want 30166", event.Kind)
	}

	// Verify pubkey
	if event.PubKey != pk {
		t.Errorf("event.PubKey = %q, want %q", event.PubKey, pk)
	}

	// Verify d tag (relay URL)
	var dTag string
	for _, tag := range event.Tags {
		if tag[0] == "d" {
			dTag = tag[1]
			break
		}
	}
	if dTag != entry.URL {
		t.Errorf("d tag = %q, want %q", dTag, entry.URL)
	}

	// Verify rtt-open tag
	var rttTag string
	for _, tag := range event.Tags {
		if tag[0] == "rtt-open" {
			rttTag = tag[1]
			break
		}
	}
	if rttTag != "150" {
		t.Errorf("rtt-open tag = %q, want %q", rttTag, "150")
	}

	// Verify network tag
	var networkTag string
	for _, tag := range event.Tags {
		if tag[0] == "n" {
			networkTag = tag[1]
			break
		}
	}
	if networkTag != "clearnet" {
		t.Errorf("n tag = %q, want %q", networkTag, "clearnet")
	}

	// Verify NIP tags (one per NIP)
	nipTags := make(map[string]bool)
	for _, tag := range event.Tags {
		if tag[0] == "N" {
			nipTags[tag[1]] = true
		}
	}
	for _, nip := range []string{"1", "11", "42"} {
		if !nipTags[nip] {
			t.Errorf("missing N tag for NIP %s", nip)
		}
	}

	// Verify R tags
	rTags := make(map[string]bool)
	for _, tag := range event.Tags {
		if tag[0] == "R" {
			rTags[tag[1]] = true
		}
	}
	if !rTags["auth"] {
		t.Error("missing R tag for auth")
	}
	if !rTags["!payment"] {
		t.Error("missing R tag for !payment")
	}

	// Verify content has NIP-11 info
	if event.Content == "" {
		t.Error("event.Content should not be empty")
	}

	// Verify signature is valid
	valid, err := event.CheckSignature()
	if err != nil {
		t.Errorf("signature check error: %v", err)
	}
	if !valid {
		t.Error("invalid signature")
	}
}

func TestCreateRelayStatusEvent_TorRelay(t *testing.T) {
	cfg := &config.Config{
		NIP66PublishInterval: 3600,
	}

	sk := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	publisher := &NIP66Publisher{
		cfg: cfg,
		sk:  sk,
		pk:  pk,
	}

	entry := &cache.RelayEntry{
		URL:    "wss://somerelay.onion",
		Health: "online",
	}

	event := publisher.createRelayStatusEvent(entry)

	// Verify network tag is tor
	var networkTag string
	for _, tag := range event.Tags {
		if tag[0] == "n" {
			networkTag = tag[1]
			break
		}
	}
	if networkTag != "tor" {
		t.Errorf("n tag = %q, want %q", networkTag, "tor")
	}
}

func TestNIP66Publisher_GetMethods(t *testing.T) {
	publisher := &NIP66Publisher{}

	// Initial values should be zero/empty
	if !publisher.GetLastPublish().IsZero() {
		t.Error("GetLastPublish should return zero time initially")
	}
	if publisher.GetPublishCount() != 0 {
		t.Error("GetPublishCount should return 0 initially")
	}
	if publisher.GetRelaysPublished() != 0 {
		t.Error("GetRelaysPublished should return 0 initially")
	}
}
