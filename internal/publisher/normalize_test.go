package publisher

import (
	"testing"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestNormalizeRelayURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"wss://relay.example.com", "wss://relay.example.com"},
		{"wss://relay.example.com/", "wss://relay.example.com"},
		{"wss://relay.example.com/foo", "wss://relay.example.com"},
		{"wss://relay.example.com/kilo-oscar", "wss://relay.example.com"},
		{"wss://relay.example.com:443/bar", "wss://relay.example.com"},
		{"ws://relay.example.com:80/baz", "ws://relay.example.com"},
		{"wss://relay.example.com:8080/path", "wss://relay.example.com:8080"},
		{"ws://relay.example.com:9090", "ws://relay.example.com:9090"},
		{"wss://relay.onion:443", "wss://relay.onion"},
		{"not-a-url", "not-a-url"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeRelayURL(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeRelayURL(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestDeduplicateByHost(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com":      {URL: "wss://relay.example.com", Health: "online", LatencyMs: 50},
		"wss://relay.example.com/foo":  {URL: "wss://relay.example.com/foo", Health: "online", LatencyMs: 100},
		"wss://relay.example.com/bar":  {URL: "wss://relay.example.com/bar", Health: "degraded", LatencyMs: 30},
		"wss://other.example.com":      {URL: "wss://other.example.com", Health: "online", LatencyMs: 80},
		"wss://other.example.com/path": {URL: "wss://other.example.com/path", Health: "online", LatencyMs: 40},
	}

	result := deduplicateByHost(entries)

	if len(result) != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d", len(result))
	}

	hosts := make(map[string]bool)
	for _, e := range result {
		norm := normalizeRelayURL(e.URL)
		if hosts[norm] {
			t.Errorf("duplicate normalized host: %s", norm)
		}
		hosts[norm] = true
	}
}

func TestDeduplicateByHost_PrefersRootPath(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com/foo": {URL: "wss://relay.example.com/foo", Health: "online", LatencyMs: 50},
		"wss://relay.example.com":     {URL: "wss://relay.example.com", Health: "online", LatencyMs: 100},
	}

	result := deduplicateByHost(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].URL != "wss://relay.example.com" {
		t.Errorf("expected root path URL, got %q", result[0].URL)
	}
}

func TestDeduplicateByHost_PrefersOnline(t *testing.T) {
	entries := map[string]*cache.RelayEntry{
		"wss://relay.example.com/foo": {URL: "wss://relay.example.com/foo", Health: "online", LatencyMs: 50},
		"wss://relay.example.com/bar": {URL: "wss://relay.example.com/bar", Health: "offline", LatencyMs: 30},
	}

	result := deduplicateByHost(entries)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Health != "online" {
		t.Errorf("expected online entry, got %q", result[0].Health)
	}
}

func TestDeduplicateByHost_NoDuplicates(t *testing.T) {
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
