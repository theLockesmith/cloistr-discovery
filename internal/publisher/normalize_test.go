package publisher

import (
	"sort"
	"testing"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestCanonicalizeRelayURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"wss://relay.example.com", "wss://relay.example.com"},
		{"wss://relay.example.com/", "wss://relay.example.com"},
		{"wss://relay.example.com/foo", "wss://relay.example.com/foo"},
		{"wss://relay.example.com/kilo-oscar", "wss://relay.example.com/kilo-oscar"},
		{"wss://relay.example.com:443/bar", "wss://relay.example.com/bar"},
		{"ws://relay.example.com:80/baz", "ws://relay.example.com/baz"},
		{"wss://relay.example.com:8080/path", "wss://relay.example.com:8080/path"},
		{"ws://relay.example.com:9090", "ws://relay.example.com:9090"},
		{"wss://relay.onion:443", "wss://relay.onion"},
		{"wss://RELAY.EXAMPLE.COM/Foo", "wss://relay.example.com/Foo"},
		{"wss://relay.example.com/path/", "wss://relay.example.com/path"},
		{"wss://relay.example.com/path?q=1#frag", "wss://relay.example.com/path"},
		{"not-a-url", "not-a-url"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := canonicalizeRelayURL(tc.input)
			if result != tc.expected {
				t.Errorf("canonicalizeRelayURL(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestHostKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"wss://relay.example.com/foo", "wss://relay.example.com"},
		{"wss://relay.example.com:443/bar", "wss://relay.example.com"},
		{"wss://relay.example.com:8080/path", "wss://relay.example.com:8080"},
		{"wss://RELAY.EXAMPLE.COM", "wss://relay.example.com"},
		{"not-a-url", "not-a-url"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := hostKey(tc.input)
			if result != tc.expected {
				t.Errorf("hostKey(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestDeduplicateByHost_CollapsesBogusPathVariants(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com": {
			URL: "wss://relay.example.com", Health: "online",
			Name: "Example Relay", Software: "strfry", SupportedNIPs: []int{1, 11},
		},
		"wss://relay.example.com/delta-lantern": {
			URL: "wss://relay.example.com/delta-lantern", Health: "online",
			Name: "Example Relay", Software: "strfry", SupportedNIPs: []int{1, 11},
		},
		"wss://relay.example.com/flint-nexus": {
			URL: "wss://relay.example.com/flint-nexus", Health: "degraded",
		},
	}

	result := deduplicateByHost(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry (bogus variants collapsed), got %d", len(result))
	}
	if result[0].URL != "wss://relay.example.com" {
		t.Errorf("expected root URL, got %q", result[0].URL)
	}
}

func TestDeduplicateByHost_KeepsLegitimatePathRelay(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://filter.nostr.wine": {
			URL: "wss://filter.nostr.wine", Health: "online",
			Name: "filter.nostr.wine", Software: "strfry", SupportedNIPs: []int{1, 11, 50},
		},
		"wss://filter.nostr.wine/npub1abc123": {
			URL: "wss://filter.nostr.wine/npub1abc123", Health: "online",
			Name: "Personal Filter", Software: "filter-relay", SupportedNIPs: []int{1, 11},
		},
	}

	result := deduplicateByHost(entries)

	if len(result) != 2 {
		t.Fatalf("expected 2 entries (legitimate path relay kept), got %d", len(result))
	}

	urls := make([]string, len(result))
	for i, e := range result {
		urls[i] = e.URL
	}
	sort.Strings(urls)

	if urls[0] != "wss://filter.nostr.wine" || urls[1] != "wss://filter.nostr.wine/npub1abc123" {
		t.Errorf("expected both root and path relay, got %v", urls)
	}
}

func TestDeduplicateByHost_CollapsesIdenticalNIP11(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com": {
			URL: "wss://relay.example.com", Health: "online",
			Name: "My Relay", Software: "strfry", Version: "1.0", SupportedNIPs: []int{1, 11},
		},
		"wss://relay.example.com/path-a": {
			URL: "wss://relay.example.com/path-a", Health: "online",
			Name: "My Relay", Software: "strfry", Version: "1.0", SupportedNIPs: []int{1, 11},
		},
		"wss://relay.example.com/path-b": {
			URL: "wss://relay.example.com/path-b", Health: "online",
			Name: "My Relay", Software: "strfry", Version: "1.0", SupportedNIPs: []int{1, 11},
		},
	}

	result := deduplicateByHost(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry (identical NIP-11 collapsed), got %d", len(result))
	}
	if result[0].URL != "wss://relay.example.com" {
		t.Errorf("expected root URL, got %q", result[0].URL)
	}
}

func TestDeduplicateByHost_NoRootKeepsBestVariant(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com/alpha": {URL: "wss://relay.example.com/alpha", Health: "degraded"},
		"wss://relay.example.com/bravo": {URL: "wss://relay.example.com/bravo", Health: "online"},
	}

	result := deduplicateByHost(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Health != "online" {
		t.Errorf("expected healthiest variant, got health=%q", result[0].Health)
	}
}

func TestDeduplicateByHost_DifferentHostsUntouched(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://a.example.com": {URL: "wss://a.example.com", Health: "online"},
		"wss://b.example.com": {URL: "wss://b.example.com", Health: "online"},
		"wss://c.example.com": {URL: "wss://c.example.com", Health: "online"},
	}

	result := deduplicateByHost(entries)

	if len(result) != 3 {
		t.Fatalf("expected 3 entries (no dedup needed), got %d", len(result))
	}
}

func TestDeduplicateByHost_DefaultPortVariants(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com": {
			URL: "wss://relay.example.com", Health: "online",
			Name: "Relay", Software: "strfry",
		},
		"wss://relay.example.com:443/ivory": {
			URL: "wss://relay.example.com:443/ivory", Health: "online",
		},
	}

	result := deduplicateByHost(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry (port variant grouped with root), got %d", len(result))
	}
}

func TestDeduplicateByHost_InboxRelaySurvives(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://hbr.coracle.social": {
			URL: "wss://hbr.coracle.social", Health: "online",
			Name: "HBR", Software: "strfry", SupportedNIPs: []int{1, 11},
		},
		"wss://hbr.coracle.social/chat/general": {
			URL: "wss://hbr.coracle.social/chat/general", Health: "online",
			Name: "HBR Chat General", Software: "haven", SupportedNIPs: []int{1, 11, 29},
		},
	}

	result := deduplicateByHost(entries)

	if len(result) != 2 {
		t.Fatalf("expected 2 entries (distinct path relay survives), got %d", len(result))
	}
}
