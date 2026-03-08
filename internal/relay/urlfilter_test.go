package relay

import "testing"

func TestURLFilter(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		torEnabled bool
		wantValid  bool
		wantTor    bool
		wantReason string
	}{
		// Valid URLs
		{
			name:      "valid wss URL",
			url:       "wss://relay.damus.io",
			wantValid: true,
		},
		{
			name:      "valid ws URL",
			url:       "ws://relay.example.com",
			wantValid: true,
		},
		{
			name:      "valid URL with port",
			url:       "wss://relay.example.com:443",
			wantValid: true,
		},
		{
			name:      "valid URL with path",
			url:       "wss://relay.example.com/nostr",
			wantValid: true,
		},

		// Invalid - wrong scheme
		{
			name:       "https URL",
			url:        "https://relay.example.com",
			wantValid:  false,
			wantReason: "not websocket URL",
		},
		{
			name:       "http URL",
			url:        "http://relay.example.com",
			wantValid:  false,
			wantReason: "not websocket URL",
		},

		// Invalid - empty
		{
			name:       "empty URL",
			url:        "",
			wantValid:  false,
			wantReason: "empty URL",
		},
		{
			name:       "whitespace only",
			url:        "   ",
			wantValid:  false,
			wantReason: "empty URL",
		},

		// Invalid - localhost/loopback
		{
			name:       "localhost",
			url:        "wss://localhost",
			wantValid:  false,
			wantReason: "localhost address",
		},
		{
			name:       "127.0.0.1",
			url:        "wss://127.0.0.1",
			wantValid:  false,
			wantReason: "localhost address",
		},
		{
			name:       "127.0.0.100 loopback",
			url:        "wss://127.0.0.100",
			wantValid:  false,
			wantReason: "localhost address",
		},
		{
			name:       "IPv6 loopback",
			url:        "wss://[::1]",
			wantValid:  false,
			wantReason: "localhost address",
		},

		// Invalid - private IPs
		{
			name:       "10.x.x.x private",
			url:        "wss://10.0.0.1",
			wantValid:  false,
			wantReason: "private IP address",
		},
		{
			name:       "172.16.x.x private",
			url:        "wss://172.16.0.1",
			wantValid:  false,
			wantReason: "private IP address",
		},
		{
			name:       "172.31.x.x private",
			url:        "wss://172.31.255.255",
			wantValid:  false,
			wantReason: "private IP address",
		},
		{
			name:       "192.168.x.x private",
			url:        "wss://192.168.1.1",
			wantValid:  false,
			wantReason: "private IP address",
		},
		{
			name:       "169.254.x.x link-local",
			url:        "wss://169.254.1.1",
			wantValid:  false,
			wantReason: "private IP address",
		},

		// Invalid - special domains
		{
			name:       ".local mDNS",
			url:        "wss://relay.local",
			wantValid:  false,
			wantReason: "local mDNS address",
		},
		{
			name:       "tailscale .ts.net",
			url:        "wss://umbrel.tail6ee2a9.ts.net",
			wantValid:  false,
			wantReason: "Tailscale private address",
		},

		// Invalid - bad TLDs
		{
			name:       "invalid TLD",
			url:        "wss://relay.invalid",
			wantValid:  false,
			wantReason: "invalid TLD",
		},
		{
			name:       "test TLD",
			url:        "wss://relay.test",
			wantValid:  false,
			wantReason: "invalid TLD",
		},
		{
			name:       "localhost TLD",
			url:        "wss://relay.localhost",
			wantValid:  false,
			wantReason: "invalid TLD",
		},

		// Invalid - no domain
		{
			name:       "hostname without domain",
			url:        "wss://relay",
			wantValid:  false,
			wantReason: "hostname without domain",
		},

		// Onion addresses
		{
			name:       "onion without tor",
			url:        "ws://oxz6arhmx7dxrgxveqccafilm4lbkaqegxvgrufw4tbzrdkg22bnt7id.onion",
			torEnabled: false,
			wantValid:  false,
			wantReason: "onion address without Tor proxy",
		},
		{
			name:       "onion with tor enabled",
			url:        "ws://oxz6arhmx7dxrgxveqccafilm4lbkaqegxvgrufw4tbzrdkg22bnt7id.onion",
			torEnabled: true,
			wantValid:  true,
			wantTor:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewURLFilter(tt.torEnabled)
			result := filter.Filter(tt.url)

			if result.Valid != tt.wantValid {
				t.Errorf("Filter(%q).Valid = %v, want %v (reason: %s)",
					tt.url, result.Valid, tt.wantValid, result.Reason)
			}

			if result.RequiresTor != tt.wantTor {
				t.Errorf("Filter(%q).RequiresTor = %v, want %v",
					tt.url, result.RequiresTor, tt.wantTor)
			}

			if tt.wantReason != "" && result.Reason != tt.wantReason {
				t.Errorf("Filter(%q).Reason = %q, want %q",
					tt.url, result.Reason, tt.wantReason)
			}
		})
	}
}

func TestURLFilter_PublicIP(t *testing.T) {
	// Test that non-private IPs are valid
	filter := NewURLFilter(false)

	validIPs := []string{
		"wss://8.8.8.8",      // Google DNS
		"wss://1.1.1.1",      // Cloudflare
		"wss://172.15.0.1",   // Just outside 172.16-31 range
		"wss://172.32.0.1",   // Just outside 172.16-31 range
		"wss://192.167.1.1",  // Not 192.168
		"wss://168.254.1.1",  // Not 169.254
	}

	for _, url := range validIPs {
		result := filter.Filter(url)
		if !result.Valid {
			t.Errorf("Filter(%q) should be valid, got invalid: %s", url, result.Reason)
		}
	}
}
