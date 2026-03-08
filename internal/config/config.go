// Package config handles configuration loading for coldforge-discovery.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the discovery service.
type Config struct {
	// Server settings
	Port     int
	LogLevel string

	// Cache settings (Dragonfly/Redis)
	CacheURL string

	// Relay monitoring settings
	SeedRelays         []string // Initial relays to monitor
	RelayCheckInterval int      // Seconds between relay health checks
	NIP11Timeout       int      // Seconds to wait for NIP-11 response

	// Inventory settings
	InventoryTTL int // Hours before inventory expires

	// Activity settings
	ActivityTTL int // Minutes before activity expires

	// Publishing settings
	PublishEnabled  bool     // Enable publishing kind 30069 events
	PublishRelays   []string // Relays to publish discovery events to
	PublishInterval int      // Minutes between publish cycles
	PrivateKey      string   // hex or nsec private key for signing events
	BunkerURL       string   // NIP-46 bunker URL for signing (alternative to PrivateKey)

	// Discovery source settings
	HostedRelayListURL      string   // URL to fetch relay list from (JSON or newline-separated)
	HostedRelayListInterval int      // Minutes between fetches (0 = fetch once on startup)
	NIP65CrawlEnabled       bool     // Enable NIP-65 crawling (kind 10002 user relay lists)
	NIP65CrawlInterval      int      // Minutes between crawl cycles
	NIP66Enabled            bool     // Enable NIP-66 consumption (kind 30166 relay monitors)
	PeerDiscoveryEnabled    bool     // Enable peer discovery (kind 30069 from trusted peers)
	TrustedDiscoveryPeers   []string // Pubkeys of trusted discovery services

	// Admin interface settings
	AdminEnabled  bool   // Enable admin interface
	AdminAPIKey   string // API key for admin auth (if set, takes precedence)
	AdminUsername string // Basic auth username (fallback if no API key)
	AdminPassword string // Basic auth password

	// DNS/Network optimization settings
	TorProxyURL        string // SOCKS5 proxy URL for Tor (e.g., socks5://localhost:9050)
	DNSCacheSuccessTTL int    // Hours to cache successful DNS lookups (default: 1)
	DNSCacheFailureTTL int    // Hours to cache NXDOMAIN results (default: 24)
	DNSCacheTimeoutTTL int    // Minutes to cache timeout results (default: 30)
	StaggeredChecks    bool   // Enable staggered health checks (default: true)
	ChecksPerSecond    int    // Max relay checks per second when staggered (default: 3)
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		// Server settings
		Port:     getEnvInt("DISCOVERY_PORT", 8080),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		// Cache settings
		CacheURL: getEnv("CACHE_URL", "redis://localhost:6379"),

		// Relay monitoring settings
		SeedRelays:         getEnvSlice("SEED_RELAYS", []string{"wss://relay.damus.io", "wss://nos.lol", "wss://relay.nostr.band"}),
		RelayCheckInterval: getEnvInt("RELAY_CHECK_INTERVAL", 300),
		NIP11Timeout:       getEnvInt("NIP11_TIMEOUT", 10),

		// Inventory/Activity settings
		InventoryTTL: getEnvInt("INVENTORY_TTL", 12),
		ActivityTTL:  getEnvInt("ACTIVITY_TTL", 15),

		// Publishing settings
		PublishEnabled:  getEnvBool("PUBLISH_ENABLED", false),
		PublishRelays:   getEnvSlice("PUBLISH_RELAYS", []string{"wss://relay.cloistr.xyz"}),
		PublishInterval: getEnvInt("PUBLISH_INTERVAL", 10),
		PrivateKey:      getEnv("NOSTR_PRIVATE_KEY", ""),
		BunkerURL:       getEnv("BUNKER_URL", ""),

		// Discovery source settings
		HostedRelayListURL:      getEnv("HOSTED_RELAY_LIST_URL", ""),
		HostedRelayListInterval: getEnvInt("HOSTED_RELAY_LIST_INTERVAL", 60),
		NIP65CrawlEnabled:       getEnvBool("NIP65_CRAWL_ENABLED", true),
		NIP65CrawlInterval:      getEnvInt("NIP65_CRAWL_INTERVAL", 30),
		NIP66Enabled:            getEnvBool("NIP66_ENABLED", true),
		PeerDiscoveryEnabled:    getEnvBool("PEER_DISCOVERY_ENABLED", true),
		TrustedDiscoveryPeers:   getEnvSlice("TRUSTED_DISCOVERY_PEERS", []string{}),

		// Admin interface settings
		AdminEnabled:  getEnvBool("ADMIN_ENABLED", true),
		AdminAPIKey:   getEnv("ADMIN_API_KEY", ""),
		AdminUsername: getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),

		// DNS/Network optimization settings
		TorProxyURL:        getEnv("TOR_PROXY_URL", ""),
		DNSCacheSuccessTTL: getEnvInt("DNS_CACHE_SUCCESS_TTL", 1),
		DNSCacheFailureTTL: getEnvInt("DNS_CACHE_FAILURE_TTL", 24),
		DNSCacheTimeoutTTL: getEnvInt("DNS_CACHE_TIMEOUT_TTL", 30),
		StaggeredChecks:    getEnvBool("STAGGERED_CHECKS", true),
		ChecksPerSecond:    getEnvInt("CHECKS_PER_SECOND", 3),
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return defaultValue
}

func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
