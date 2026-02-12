// Package relay handles relay monitoring and health checking.
// Implements Kind 30072 (Relay Directory Entry) from NDP.
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
	"gitlab.com/coldforge/coldforge-discovery/internal/metrics"
)

// NIP11Info represents the NIP-11 relay information document.
type NIP11Info struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Pubkey        string `json:"pubkey"`
	Contact       string `json:"contact"`
	SupportedNIPs []int  `json:"supported_nips"`
	Software      string `json:"software"`
	Version       string `json:"version"`
	Limitation    struct {
		AuthRequired    bool `json:"auth_required"`
		PaymentRequired bool `json:"payment_required"`
	} `json:"limitation"`

	// Extended fields for relay-based segregation (may be absent from most relays)
	ContentPolicy    string   `json:"content_policy,omitempty"`    // anything, sfw, nsfw-allowed, nsfw-only
	Moderation       string   `json:"moderation,omitempty"`        // unmoderated, light, active, strict
	ModerationPolicy string   `json:"moderation_policy,omitempty"` // URL to relay rules
	Community        string   `json:"community,omitempty"`         // community name
	Languages        []string `json:"languages,omitempty"`         // ISO 639-1 codes
}

// Monitor continuously checks relay health and updates the cache.
type Monitor struct {
	cfg    *config.Config
	cache  *cache.Client
	client *http.Client

	mu          sync.RWMutex
	knownRelays map[string]bool

	// Channel for receiving discovered relays from discovery sources
	discoveryInput chan string
}

// NewMonitor creates a new relay monitor.
func NewMonitor(cfg *config.Config, cache *cache.Client) *Monitor {
	return &Monitor{
		cfg:            cfg,
		cache:          cache,
		knownRelays:    make(map[string]bool),
		discoveryInput: make(chan string, 1000),
		client: &http.Client{
			Timeout: time.Duration(cfg.NIP11Timeout) * time.Second,
		},
	}
}

// DiscoveryChannel returns the channel for discovery sources to send relay URLs.
func (m *Monitor) DiscoveryChannel() chan<- string {
	return m.discoveryInput
}

// Start begins the relay monitoring loop.
func (m *Monitor) Start(ctx context.Context) {
	// Seed initial relays
	for _, relay := range m.cfg.SeedRelays {
		m.AddRelay(relay)
	}

	// Add whitelisted relays
	whitelist, err := m.cache.GetWhitelist(ctx)
	if err == nil {
		for _, relay := range whitelist {
			m.AddRelay(relay)
		}
	}

	// Load previously discovered relays from cache so they survive restarts
	seen, err := m.cache.GetSeenRelays(ctx)
	if err == nil && len(seen) > 0 {
		for _, relay := range seen {
			m.AddRelay(relay)
		}
		slog.Info("loaded previously discovered relays", "count", len(seen))
	}

	ticker := time.NewTicker(time.Duration(m.cfg.RelayCheckInterval) * time.Second)
	defer ticker.Stop()

	// Run initial check
	m.checkAllRelays(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("relay monitor stopping")
			return
		case <-ticker.C:
			m.checkAllRelays(ctx)
		case url := <-m.discoveryInput:
			m.handleDiscoveredRelay(ctx, url)
		}
	}
}

// handleDiscoveredRelay processes a relay URL from discovery sources.
func (m *Monitor) handleDiscoveredRelay(ctx context.Context, url string) {
	url = normalizeURL(url)
	if url == "" {
		return
	}

	// Check if blacklisted
	isBlacklisted, err := m.cache.IsBlacklisted(ctx, url)
	if err != nil {
		slog.Error("failed to check blacklist", "url", url, "error", err)
		return
	}
	if isBlacklisted {
		slog.Debug("relay is blacklisted, not adding", "url", url)
		return
	}

	// Check if already known
	m.mu.RLock()
	known := m.knownRelays[url]
	m.mu.RUnlock()

	if known {
		return
	}

	// Add to monitoring
	m.AddRelay(url)
	slog.Debug("added discovered relay to monitoring", "url", url)

	// Optionally do an immediate health check
	go func() {
		entry, err := m.checkRelay(ctx, url)
		if err != nil {
			slog.Debug("initial check for discovered relay failed", "url", url, "error", err)
			entry = &cache.RelayEntry{
				URL:         url,
				Health:      "offline",
				LastChecked: time.Now(),
			}
		}

		if err := m.cache.SetRelayEntry(ctx, entry, time.Hour); err != nil {
			slog.Error("failed to cache relay entry", "url", url, "error", err)
		}
	}()
}

// AddRelay adds a relay to the monitoring list.
func (m *Monitor) AddRelay(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.knownRelays[normalizeURL(url)] = true
}

// RemoveRelay removes a relay from the monitoring list.
func (m *Monitor) RemoveRelay(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.knownRelays, normalizeURL(url))
}

// GetRelays returns all known relay URLs.
func (m *Monitor) GetRelays() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	relays := make([]string, 0, len(m.knownRelays))
	for url := range m.knownRelays {
		relays = append(relays, url)
	}
	return relays
}

// RelayCount returns the number of known relays.
func (m *Monitor) RelayCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.knownRelays)
}

func (m *Monitor) checkAllRelays(ctx context.Context) {
	start := time.Now()
	relays := m.GetRelays()
	slog.Info("checking relays", "count", len(relays))

	// Update monitored relays gauge
	metrics.RelaysMonitored.Set(float64(len(relays)))

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit concurrent checks

	var (
		online   int64
		degraded int64
		offline  int64
		mu       sync.Mutex
	)

	for _, url := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			entry, err := m.checkRelay(ctx, relayURL)
			if err != nil {
				slog.Warn("relay check failed", "url", relayURL, "error", err)
				// Cache as offline
				entry = &cache.RelayEntry{
					URL:         relayURL,
					Health:      "offline",
					LastChecked: time.Now(),
				}
			}

			// Count health statuses and record metrics
			mu.Lock()
			switch entry.Health {
			case "online":
				online++
				metrics.HealthChecksTotal.WithLabelValues("online").Inc()
			case "degraded":
				degraded++
				metrics.HealthChecksTotal.WithLabelValues("degraded").Inc()
			default:
				offline++
				metrics.HealthChecksTotal.WithLabelValues("offline").Inc()
			}
			mu.Unlock()

			if err := m.cache.SetRelayEntry(ctx, entry, time.Hour); err != nil {
				slog.Error("failed to cache relay entry", "url", relayURL, "error", err)
			}
		}(url)
	}

	wg.Wait()

	// Update health gauge metrics
	metrics.RelaysByHealth.WithLabelValues("online").Set(float64(online))
	metrics.RelaysByHealth.WithLabelValues("degraded").Set(float64(degraded))
	metrics.RelaysByHealth.WithLabelValues("offline").Set(float64(offline))

	// Record cycle duration
	metrics.HealthCheckCycleDurationSeconds.Observe(time.Since(start).Seconds())

	// Update stats
	m.cache.SetStat(ctx, "relays:total", int64(len(relays)))
	m.cache.SetStat(ctx, "relays:online", online)
	m.cache.SetStat(ctx, "relays:degraded", degraded)
	m.cache.SetStat(ctx, "relays:offline", offline)

	slog.Info("relay check complete",
		"total", len(relays),
		"online", online,
		"degraded", degraded,
		"offline", offline,
	)
}

func (m *Monitor) checkRelay(ctx context.Context, url string) (*cache.RelayEntry, error) {
	// Convert wss:// to https:// for NIP-11
	httpURL := wsToHTTP(url)

	start := time.Now()
	defer func() {
		metrics.HealthCheckDurationSeconds.Observe(time.Since(start).Seconds())
	}()

	req, err := http.NewRequestWithContext(ctx, "GET", httpURL, nil)
	if err != nil {
		metrics.NIP11FetchErrorsTotal.WithLabelValues("request_create").Inc()
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/nostr+json")

	resp, err := m.client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			metrics.NIP11FetchErrorsTotal.WithLabelValues("timeout").Inc()
		} else {
			metrics.NIP11FetchErrorsTotal.WithLabelValues("connection").Inc()
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		metrics.NIP11FetchErrorsTotal.WithLabelValues("http_error").Inc()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		metrics.NIP11FetchErrorsTotal.WithLabelValues("read_body").Inc()
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var info NIP11Info
	if err := json.Unmarshal(body, &info); err != nil {
		metrics.NIP11FetchErrorsTotal.WithLabelValues("parse").Inc()
		return nil, fmt.Errorf("failed to parse NIP-11: %w", err)
	}

	// Determine health based on latency
	health := "online"
	if latency > 5*time.Second {
		health = "degraded"
	}

	entry := &cache.RelayEntry{
		URL:              url,
		Name:             info.Name,
		Description:      info.Description,
		Pubkey:           info.Pubkey,
		SupportedNIPs:    info.SupportedNIPs,
		Software:         info.Software,
		Version:          info.Version,
		Health:           health,
		LatencyMs:        int(latency.Milliseconds()),
		LastChecked:      time.Now(),
		PaymentRequired:  info.Limitation.PaymentRequired,
		AuthRequired:     info.Limitation.AuthRequired,
		ContentPolicy:    info.ContentPolicy,
		Moderation:       info.Moderation,
		ModerationPolicy: info.ModerationPolicy,
		Community:        info.Community,
		Languages:        info.Languages,
	}

	return entry, nil
}

// normalizeURL ensures consistent URL format.
func normalizeURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	return url
}

// wsToHTTP converts a WebSocket URL to HTTP for NIP-11 requests.
func wsToHTTP(url string) string {
	if strings.HasPrefix(url, "wss://") {
		return "https://" + strings.TrimPrefix(url, "wss://")
	}
	if strings.HasPrefix(url, "ws://") {
		return "http://" + strings.TrimPrefix(url, "ws://")
	}
	return url
}
