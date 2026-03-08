// Package relay handles relay monitoring and health checking.
package relay

import (
	"net"
	"net/url"
	"strings"
)

// URLFilterResult represents the result of URL validation.
type URLFilterResult struct {
	Valid       bool
	Reason      string
	RequiresTor bool // True if URL is .onion and needs Tor proxy
}

// URLFilter validates and filters relay URLs before health checks.
type URLFilter struct {
	torProxyEnabled bool
}

// NewURLFilter creates a new URL filter.
// torProxyEnabled indicates whether a Tor proxy is configured.
func NewURLFilter(torProxyEnabled bool) *URLFilter {
	return &URLFilter{
		torProxyEnabled: torProxyEnabled,
	}
}

// Filter checks if a URL is valid for health checking.
func (f *URLFilter) Filter(rawURL string) URLFilterResult {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return URLFilterResult{Valid: false, Reason: "empty URL"}
	}

	// Must be ws:// or wss://
	if !strings.HasPrefix(rawURL, "wss://") && !strings.HasPrefix(rawURL, "ws://") {
		return URLFilterResult{Valid: false, Reason: "not websocket URL"}
	}

	// Parse URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return URLFilterResult{Valid: false, Reason: "malformed URL: " + err.Error()}
	}

	host := parsed.Hostname()
	if host == "" {
		return URLFilterResult{Valid: false, Reason: "no hostname"}
	}

	// Check for .onion addresses
	if strings.HasSuffix(host, ".onion") {
		if f.torProxyEnabled {
			return URLFilterResult{Valid: true, RequiresTor: true}
		}
		return URLFilterResult{Valid: false, Reason: "onion address without Tor proxy"}
	}

	// Check for localhost/loopback
	if f.isLocalhost(host) {
		return URLFilterResult{Valid: false, Reason: "localhost address"}
	}

	// Check for private/reserved IP ranges
	if f.isPrivateIP(host) {
		return URLFilterResult{Valid: false, Reason: "private IP address"}
	}

	// Check for .local addresses (mDNS)
	if strings.HasSuffix(host, ".local") {
		return URLFilterResult{Valid: false, Reason: "local mDNS address"}
	}

	// Check for Tailscale addresses (unless explicitly allowed)
	if strings.HasSuffix(host, ".ts.net") {
		return URLFilterResult{Valid: false, Reason: "Tailscale private address"}
	}

	// Check hostname has at least one dot (basic domain validation)
	// This should come before hasInvalidTLD since that also rejects single-part hostnames
	if !strings.Contains(host, ".") {
		return URLFilterResult{Valid: false, Reason: "hostname without domain"}
	}

	// Check for invalid TLDs
	if f.hasInvalidTLD(host) {
		return URLFilterResult{Valid: false, Reason: "invalid TLD"}
	}

	return URLFilterResult{Valid: true}
}

// isLocalhost checks if the host is localhost or loopback.
func (f *URLFilter) isLocalhost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	// Check for 127.x.x.x range
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4[0] == 127
		}
	}

	return false
}

// isPrivateIP checks if the host is a private/reserved IP address.
func (f *URLFilter) isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	// Check standard private ranges
	// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
	if ip4 := ip.To4(); ip4 != nil {
		// 10.x.x.x
		if ip4[0] == 10 {
			return true
		}
		// 172.16.x.x - 172.31.x.x
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.x.x
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 169.254.x.x (link-local)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}

	// Check IPv6 private ranges
	if ip.To4() == nil {
		// fc00::/7 (unique local)
		if len(ip) >= 1 && (ip[0]&0xfe) == 0xfc {
			return true
		}
		// fe80::/10 (link-local)
		if len(ip) >= 2 && ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
			return true
		}
	}

	return false
}

// hasInvalidTLD checks for obviously invalid TLDs.
func (f *URLFilter) hasInvalidTLD(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return true
	}

	tld := strings.ToLower(parts[len(parts)-1])

	// List of invalid/reserved TLDs
	invalidTLDs := map[string]bool{
		"invalid":   true,
		"localhost": true,
		"test":      true,
		"example":   true,
		"internal":  true,
		"lan":       true,
		"home":      true,
		"corp":      true,
		"mail":      true,
		"intranet":  true,
	}

	return invalidTLDs[tld]
}
