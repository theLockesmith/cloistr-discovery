package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any existing env vars
	envVars := []string{
		"DISCOVERY_PORT", "LOG_LEVEL", "CACHE_URL", "SEED_RELAYS",
		"RELAY_CHECK_INTERVAL", "NIP11_TIMEOUT", "PUBLISH_ENABLED",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Check defaults
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %s, want info", cfg.LogLevel)
	}
	if cfg.CacheURL != "redis://localhost:6379" {
		t.Errorf("CacheURL = %s, want redis://localhost:6379", cfg.CacheURL)
	}
	if cfg.RelayCheckInterval != 300 {
		t.Errorf("RelayCheckInterval = %d, want 300", cfg.RelayCheckInterval)
	}
	if cfg.PublishEnabled != false {
		t.Errorf("PublishEnabled = %v, want false", cfg.PublishEnabled)
	}
	if cfg.NIP65CrawlEnabled != true {
		t.Errorf("NIP65CrawlEnabled = %v, want true", cfg.NIP65CrawlEnabled)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	// Set custom values
	os.Setenv("DISCOVERY_PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("CACHE_URL", "redis://custom:6380")
	os.Setenv("SEED_RELAYS", "wss://relay1.example,wss://relay2.example")
	os.Setenv("PUBLISH_ENABLED", "true")
	defer func() {
		os.Unsetenv("DISCOVERY_PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("CACHE_URL")
		os.Unsetenv("SEED_RELAYS")
		os.Unsetenv("PUBLISH_ENABLED")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %s, want debug", cfg.LogLevel)
	}
	if cfg.CacheURL != "redis://custom:6380" {
		t.Errorf("CacheURL = %s, want redis://custom:6380", cfg.CacheURL)
	}
	if len(cfg.SeedRelays) != 2 {
		t.Errorf("len(SeedRelays) = %d, want 2", len(cfg.SeedRelays))
	}
	if cfg.SeedRelays[0] != "wss://relay1.example" {
		t.Errorf("SeedRelays[0] = %s, want wss://relay1.example", cfg.SeedRelays[0])
	}
	if cfg.PublishEnabled != true {
		t.Errorf("PublishEnabled = %v, want true", cfg.PublishEnabled)
	}
}

func TestGetEnvInt_InvalidValue(t *testing.T) {
	os.Setenv("TEST_INT", "notanumber")
	defer os.Unsetenv("TEST_INT")

	result := getEnvInt("TEST_INT", 42)
	if result != 42 {
		t.Errorf("getEnvInt() = %d, want 42 (default)", result)
	}
}

func TestGetEnvBool_Variants(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"ON", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
		{"no", false},
		{"off", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tt.value)
			defer os.Unsetenv("TEST_BOOL")

			result := getEnvBool("TEST_BOOL", !tt.expected)
			if result != tt.expected {
				t.Errorf("getEnvBool(%q) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestGetEnvBool_InvalidValue(t *testing.T) {
	os.Setenv("TEST_BOOL", "maybe")
	defer os.Unsetenv("TEST_BOOL")

	result := getEnvBool("TEST_BOOL", true)
	if result != true {
		t.Errorf("getEnvBool() = %v, want true (default)", result)
	}
}

func TestGetEnvSlice_Empty(t *testing.T) {
	os.Unsetenv("TEST_SLICE")

	result := getEnvSlice("TEST_SLICE", []string{"default1", "default2"})
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
	if result[0] != "default1" {
		t.Errorf("result[0] = %s, want default1", result[0])
	}
}

func TestGetEnvSlice_SingleValue(t *testing.T) {
	os.Setenv("TEST_SLICE", "single")
	defer os.Unsetenv("TEST_SLICE")

	result := getEnvSlice("TEST_SLICE", []string{})
	if len(result) != 1 {
		t.Errorf("len(result) = %d, want 1", len(result))
	}
	if result[0] != "single" {
		t.Errorf("result[0] = %s, want single", result[0])
	}
}
