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
	"strconv"
	"strings"
	"sync"
	"time"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
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
	lastCheck   time.Time

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

// LastCheck returns the time of the last health check cycle.
func (m *Monitor) LastCheck() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCheck
}

// networkStats holds aggregate statistics for the relay network
type networkStats struct {
	online   int64
	degraded int64
	offline  int64

	// Aggregate counts
	nipCounts           map[int]int
	countryCounts       map[string]int
	contentPolicyCounts map[string]int
	moderationCounts    map[string]int
	softwareCounts      map[string]int
	paymentRequired     int
	paymentFree         int
	authRequired        int
	authOpen            int

	// Latencies for online relays
	latencies []int
}

func newNetworkStats() *networkStats {
	return &networkStats{
		nipCounts:           make(map[int]int),
		countryCounts:       make(map[string]int),
		contentPolicyCounts: make(map[string]int),
		moderationCounts:    make(map[string]int),
		softwareCounts:      make(map[string]int),
		latencies:           make([]int, 0),
	}
}

func (m *Monitor) checkAllRelays(ctx context.Context) {
	start := time.Now()
	relays := m.GetRelays()
	slog.Info("checking relays", "count", len(relays))

	// Update monitored relays gauge
	metrics.RelaysMonitored.Set(float64(len(relays)))

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit concurrent checks

	stats := newNetworkStats()
	var mu sync.Mutex

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

			// Collect stats under lock
			mu.Lock()
			m.collectEntryStats(stats, entry)
			mu.Unlock()

			if err := m.cache.SetRelayEntry(ctx, entry, time.Hour); err != nil {
				slog.Error("failed to cache relay entry", "url", relayURL, "error", err)
			}
		}(url)
	}

	wg.Wait()

	// Update all metrics
	m.updateNetworkMetrics(stats)

	// Record cycle duration
	metrics.HealthCheckCycleDurationSeconds.Observe(time.Since(start).Seconds())

	// Update stats in cache
	m.cache.SetStat(ctx, "relays:total", int64(len(relays)))
	m.cache.SetStat(ctx, "relays:online", stats.online)
	m.cache.SetStat(ctx, "relays:degraded", stats.degraded)
	m.cache.SetStat(ctx, "relays:offline", stats.offline)

	// Update last check time
	m.mu.Lock()
	m.lastCheck = time.Now()
	m.mu.Unlock()

	slog.Info("relay check complete",
		"total", len(relays),
		"online", stats.online,
		"degraded", stats.degraded,
		"offline", stats.offline,
	)
}

// collectEntryStats aggregates stats from a single relay entry (called under lock)
func (m *Monitor) collectEntryStats(stats *networkStats, entry *cache.RelayEntry) {
	// Health status
	switch entry.Health {
	case "online":
		stats.online++
		metrics.HealthChecksTotal.WithLabelValues("online").Inc()
	case "degraded":
		stats.degraded++
		metrics.HealthChecksTotal.WithLabelValues("degraded").Inc()
	default:
		stats.offline++
		metrics.HealthChecksTotal.WithLabelValues("offline").Inc()
	}

	// Only collect detailed stats for online/degraded relays
	if entry.Health == "offline" {
		return
	}

	// NIP support
	for _, nip := range entry.SupportedNIPs {
		stats.nipCounts[nip]++
	}

	// Country
	if entry.CountryCode != "" {
		stats.countryCounts[entry.CountryCode]++
	}

	// Content policy
	if entry.ContentPolicy != "" {
		stats.contentPolicyCounts[entry.ContentPolicy]++
	}

	// Moderation level
	if entry.Moderation != "" {
		stats.moderationCounts[entry.Moderation]++
	}

	// Software
	if entry.Software != "" {
		stats.softwareCounts[entry.Software]++
	}

	// Payment
	if entry.PaymentRequired {
		stats.paymentRequired++
	} else {
		stats.paymentFree++
	}

	// Auth
	if entry.AuthRequired {
		stats.authRequired++
	} else {
		stats.authOpen++
	}

	// Latency (only for responding relays)
	if entry.LatencyMs > 0 {
		stats.latencies = append(stats.latencies, entry.LatencyMs)
	}
}

// updateNetworkMetrics updates all Prometheus metrics from collected stats
func (m *Monitor) updateNetworkMetrics(stats *networkStats) {
	// Health metrics
	metrics.RelaysByHealth.WithLabelValues("online").Set(float64(stats.online))
	metrics.RelaysByHealth.WithLabelValues("degraded").Set(float64(stats.degraded))
	metrics.RelaysByHealth.WithLabelValues("offline").Set(float64(stats.offline))

	// NIP support - reset and set new values
	metrics.RelaysByNIP.Reset()
	for nip, count := range stats.nipCounts {
		metrics.RelaysByNIP.WithLabelValues(strconv.Itoa(nip)).Set(float64(count))
	}

	// Country distribution
	metrics.RelaysByCountry.Reset()
	for country, count := range stats.countryCounts {
		metrics.RelaysByCountry.WithLabelValues(country).Set(float64(count))
	}

	// Content policy distribution
	metrics.RelaysByContentPolicy.Reset()
	for policy, count := range stats.contentPolicyCounts {
		metrics.RelaysByContentPolicy.WithLabelValues(policy).Set(float64(count))
	}

	// Moderation level distribution
	metrics.RelaysByModeration.Reset()
	for level, count := range stats.moderationCounts {
		metrics.RelaysByModeration.WithLabelValues(level).Set(float64(count))
	}

	// Software distribution
	metrics.RelaysBySoftware.Reset()
	for software, count := range stats.softwareCounts {
		metrics.RelaysBySoftware.WithLabelValues(software).Set(float64(count))
	}

	// Payment requirement
	metrics.RelaysByPayment.WithLabelValues("true").Set(float64(stats.paymentRequired))
	metrics.RelaysByPayment.WithLabelValues("false").Set(float64(stats.paymentFree))

	// Auth requirement
	metrics.RelaysByAuth.WithLabelValues("true").Set(float64(stats.authRequired))
	metrics.RelaysByAuth.WithLabelValues("false").Set(float64(stats.authOpen))

	// Latency distribution
	for _, latency := range stats.latencies {
		metrics.RelayLatencyMilliseconds.Observe(float64(latency))
		metrics.RelayLatencySummary.Observe(float64(latency))
	}
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
