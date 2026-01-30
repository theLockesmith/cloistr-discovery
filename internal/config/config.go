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
	SeedRelays          []string // Initial relays to monitor
	RelayCheckInterval  int      // Seconds between relay health checks
	NIP11Timeout        int      // Seconds to wait for NIP-11 response

	// Inventory settings
	InventoryTTL int // Hours before inventory expires

	// Activity settings
	ActivityTTL int // Minutes before activity expires

	// Publishing settings
	PublishRelay string // Relay to publish discovery events to
	PrivateKey   string // nsec for signing events (optional, uses NIP-46 if empty)
	BunkerURL    string // NIP-46 bunker URL for signing
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Port:               getEnvInt("DISCOVERY_PORT", 8080),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		CacheURL:           getEnv("CACHE_URL", "redis://localhost:6379"),
		SeedRelays:         getEnvSlice("SEED_RELAYS", []string{"wss://relay.damus.io", "wss://nos.lol", "wss://relay.nostr.band"}),
		RelayCheckInterval: getEnvInt("RELAY_CHECK_INTERVAL", 300),
		NIP11Timeout:       getEnvInt("NIP11_TIMEOUT", 10),
		InventoryTTL:       getEnvInt("INVENTORY_TTL", 12),
		ActivityTTL:        getEnvInt("ACTIVITY_TTL", 15),
		PublishRelay:       getEnv("PUBLISH_RELAY", "wss://relay.cloistr.xyz"),
		PrivateKey:         getEnv("NOSTR_PRIVATE_KEY", ""),
		BunkerURL:          getEnv("BUNKER_URL", ""),
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

func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
