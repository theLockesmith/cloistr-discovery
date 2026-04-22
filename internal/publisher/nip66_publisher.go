// Package publisher handles publishing NIP-66 relay monitor events to Nostr relays.
package publisher

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/config"
	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// NIP66Publisher publishes NIP-66 relay monitor events (kinds 10166 and 30166).
type NIP66Publisher struct {
	cfg   *config.Config
	cache *cache.Client
	sk    string // hex private key
	pk    string // hex public key

	mu                sync.RWMutex
	lastPublish       time.Time
	publishCount      int64
	relaysPublished   int64
	announcementSent  bool
}

// NewNIP66Publisher creates a new NIP-66 publisher.
// It reuses the same private key as the main publisher.
func NewNIP66Publisher(cfg *config.Config, cache *cache.Client, sk, pk string) *NIP66Publisher {
	return &NIP66Publisher{
		cfg:   cfg,
		cache: cache,
		sk:    sk,
		pk:    pk,
	}
}

// Start begins the NIP-66 publishing loop.
func (p *NIP66Publisher) Start(ctx context.Context) {
	if p.sk == "" {
		slog.Info("NIP-66 publisher not starting - no private key configured")
		return
	}

	if !p.cfg.NIP66PublishEnabled {
		slog.Info("NIP-66 publisher not starting - disabled in config")
		return
	}

	slog.Info("NIP-66 publisher starting",
		"interval_seconds", p.cfg.NIP66PublishInterval,
		"relays", p.cfg.PublishRelays,
		"pubkey", p.pk[:16]+"...",
	)

	// Publish monitor announcement (kind 10166) immediately
	p.publishAnnouncement(ctx)

	// Publish relay status events (kind 30166) immediately
	p.publishRelayStatus(ctx)

	ticker := time.NewTicker(time.Duration(p.cfg.NIP66PublishInterval) * time.Second)
	defer ticker.Stop()

	// Refresh announcement every 24 hours
	announcementTicker := time.NewTicker(24 * time.Hour)
	defer announcementTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("NIP-66 publisher stopping")
			return
		case <-ticker.C:
			p.publishRelayStatus(ctx)
		case <-announcementTicker.C:
			p.publishAnnouncement(ctx)
		}
	}
}

// publishAnnouncement publishes a kind 10166 relay monitor announcement event.
func (p *NIP66Publisher) publishAnnouncement(ctx context.Context) {
	slog.Info("publishing NIP-66 monitor announcement (kind 10166)")

	event := p.createAnnouncementEvent()

	for _, relayURL := range p.cfg.PublishRelays {
		if err := p.publishEvent(ctx, relayURL, event, "10166"); err != nil {
			slog.Warn("failed to publish monitor announcement",
				"relay", relayURL,
				"error", err,
			)
		} else {
			slog.Info("published monitor announcement", "relay", relayURL)
		}
	}

	p.mu.Lock()
	p.announcementSent = true
	p.mu.Unlock()
}

// publishRelayStatus publishes kind 30166 relay status events for all monitored relays.
func (p *NIP66Publisher) publishRelayStatus(ctx context.Context) {
	start := time.Now()
	defer func() {
		metrics.NIP66PublishDurationSeconds.Observe(time.Since(start).Seconds())
		metrics.NIP66PublishCyclesTotal.Inc()
	}()

	// Get all relay URLs from cache
	urls, err := p.cache.GetAllRelayURLs(ctx)
	if err != nil {
		slog.Error("failed to get relay URLs for NIP-66 publishing", "error", err)
		return
	}

	if len(urls) == 0 {
		slog.Debug("no relays to publish for NIP-66")
		return
	}

	// Build events from cache
	var events []*nostr.Event
	for _, url := range urls {
		entry, err := p.cache.GetRelayEntry(ctx, url)
		if err != nil || entry == nil {
			continue
		}
		// Only publish healthy relays (online or degraded) to avoid noise
		if entry.Health == "offline" {
			continue
		}
		events = append(events, p.createRelayStatusEvent(entry))
	}

	slog.Info("publishing NIP-66 relay status events (kind 30166)", "count", len(events))

	var published int64
	for _, relayURL := range p.cfg.PublishRelays {
		count := p.publishBatch(ctx, relayURL, events)
		if count > published {
			published = count
		}
	}

	p.mu.Lock()
	p.lastPublish = time.Now()
	p.publishCount++
	p.relaysPublished = published
	p.mu.Unlock()

	metrics.NIP66RelaysPublished.Set(float64(published))

	slog.Info("published NIP-66 relay status events",
		"published", published,
		"total", len(events),
	)
}

// createAnnouncementEvent creates a kind 10166 monitor announcement event.
func (p *NIP66Publisher) createAnnouncementEvent() *nostr.Event {
	tags := nostr.Tags{
		// Required: frequency in seconds
		{"frequency", strconv.Itoa(p.cfg.NIP66PublishInterval)},
		// Check types we perform
		{"c", "open"},
		{"c", "read"},
		{"c", "nip11"},
		// Timeout in milliseconds
		{"timeout", strconv.Itoa(p.cfg.NIP11Timeout * 1000)},
		{"timeout", strconv.Itoa(p.cfg.NIP11Timeout * 1000), "open"},
		{"timeout", strconv.Itoa(p.cfg.NIP11Timeout * 1000), "nip11"},
	}

	event := &nostr.Event{
		Kind:      10166,
		PubKey:    p.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	event.Sign(p.sk)
	return event
}

// createRelayStatusEvent creates a kind 30166 relay status event.
func (p *NIP66Publisher) createRelayStatusEvent(entry *cache.RelayEntry) *nostr.Event {
	tags := nostr.Tags{
		// Required: d tag with relay URL
		{"d", entry.URL},
	}

	// Round-trip time (latency)
	if entry.LatencyMs > 0 {
		tags = append(tags, nostr.Tag{"rtt-open", strconv.Itoa(entry.LatencyMs)})
	}

	// Network type (detect from URL)
	network := detectNetwork(entry.URL)
	tags = append(tags, nostr.Tag{"n", network})

	// Supported NIPs (one tag per NIP as per spec)
	for _, nip := range entry.SupportedNIPs {
		tags = append(tags, nostr.Tag{"N", strconv.Itoa(nip)})
	}

	// Requirements
	if entry.AuthRequired {
		tags = append(tags, nostr.Tag{"R", "auth"})
	} else {
		tags = append(tags, nostr.Tag{"R", "!auth"})
	}
	if entry.PaymentRequired {
		tags = append(tags, nostr.Tag{"R", "payment"})
	} else {
		tags = append(tags, nostr.Tag{"R", "!payment"})
	}

	// Geohash if we have country code (placeholder - need actual geohash)
	if entry.CountryCode != "" {
		// Note: This is a country code, not a geohash. Full geohash support
		// requires lat/lon which we'll add in Phase 2 with GeoIP integration.
		// For now, we skip the g tag rather than publish incorrect data.
	}

	// Content: NIP-11 info document as JSON
	content := ""
	if entry.Name != "" || entry.Description != "" || len(entry.SupportedNIPs) > 0 {
		nip11 := map[string]interface{}{}
		if entry.Name != "" {
			nip11["name"] = entry.Name
		}
		if entry.Description != "" {
			nip11["description"] = entry.Description
		}
		if entry.Pubkey != "" {
			nip11["pubkey"] = entry.Pubkey
		}
		if entry.Software != "" {
			nip11["software"] = entry.Software
		}
		if entry.Version != "" {
			nip11["version"] = entry.Version
		}
		if len(entry.SupportedNIPs) > 0 {
			nip11["supported_nips"] = entry.SupportedNIPs
		}
		if jsonBytes, err := json.Marshal(nip11); err == nil {
			content = string(jsonBytes)
		}
	}

	event := &nostr.Event{
		Kind:      30166,
		PubKey:    p.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   content,
	}

	event.Sign(p.sk)
	return event
}

// detectNetwork determines the network type from a relay URL.
func detectNetwork(url string) string {
	url = strings.ToLower(url)
	if strings.Contains(url, ".onion") {
		return "tor"
	}
	if strings.Contains(url, ".i2p") {
		return "i2p"
	}
	if strings.Contains(url, ".loki") {
		return "loki"
	}
	return "clearnet"
}

// publishEvent publishes a single event to a relay.
func (p *NIP66Publisher) publishEvent(ctx context.Context, relayURL string, event *nostr.Event, kind string) error {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		metrics.NIP66PublishErrorsTotal.WithLabelValues(kind, relayURL, "connection").Inc()
		return err
	}
	defer relay.Close()

	if err := relay.Publish(ctx, *event); err != nil {
		// Check if auth is required
		if strings.Contains(err.Error(), "auth-required") {
			if authErr := relay.Auth(ctx, func(authEvent *nostr.Event) error {
				return authEvent.Sign(p.sk)
			}); authErr != nil {
				metrics.NIP66PublishErrorsTotal.WithLabelValues(kind, relayURL, "auth_failed").Inc()
				return authErr
			}
			// Retry after auth
			if retryErr := relay.Publish(ctx, *event); retryErr != nil {
				metrics.NIP66PublishErrorsTotal.WithLabelValues(kind, relayURL, "publish_after_auth").Inc()
				return retryErr
			}
		} else {
			metrics.NIP66PublishErrorsTotal.WithLabelValues(kind, relayURL, "publish").Inc()
			return err
		}
	}

	metrics.NIP66EventsPublished.WithLabelValues(kind, relayURL).Inc()
	return nil
}

// publishBatch publishes multiple events to a relay.
func (p *NIP66Publisher) publishBatch(ctx context.Context, relayURL string, events []*nostr.Event) int64 {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect for NIP-66 batch publish", "url", relayURL, "error", err)
		metrics.NIP66PublishErrorsTotal.WithLabelValues("30166", relayURL, "connection").Inc()
		return 0
	}
	defer relay.Close()

	var published int64
	authAttempted := false

	for _, event := range events {
		err := relay.Publish(ctx, *event)
		if err == nil {
			published++
			metrics.NIP66EventsPublished.WithLabelValues("30166", relayURL).Inc()
			continue
		}

		// Handle auth
		if strings.Contains(err.Error(), "auth-required") && !authAttempted {
			authAttempted = true
			if authErr := relay.Auth(ctx, func(authEvent *nostr.Event) error {
				return authEvent.Sign(p.sk)
			}); authErr != nil {
				slog.Warn("NIP-42 auth failed for NIP-66 publishing", "url", relayURL, "error", authErr)
				metrics.NIP66PublishErrorsTotal.WithLabelValues("30166", relayURL, "auth_failed").Inc()
				return published
			}
			// Retry this event
			if retryErr := relay.Publish(ctx, *event); retryErr == nil {
				published++
				metrics.NIP66EventsPublished.WithLabelValues("30166", relayURL).Inc()
			}
			continue
		}

		metrics.NIP66PublishErrorsTotal.WithLabelValues("30166", relayURL, "publish").Inc()
	}

	return published
}

// GetLastPublish returns the time of the last publish cycle.
func (p *NIP66Publisher) GetLastPublish() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastPublish
}

// GetPublishCount returns the number of publish cycles completed.
func (p *NIP66Publisher) GetPublishCount() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.publishCount
}

// GetRelaysPublished returns the number of relays published in the last cycle.
func (p *NIP66Publisher) GetRelaysPublished() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.relaysPublished
}
