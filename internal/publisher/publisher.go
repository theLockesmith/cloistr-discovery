// Package publisher handles publishing kind 30072 relay directory events to Nostr relays.
// This enables passive federation with other discovery services.
package publisher

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// Publisher publishes kind 30072 relay directory events.
type Publisher struct {
	cfg    *config.Config
	cache  *cache.Client
	sk     string // hex private key
	pk     string // hex public key

	mu            sync.RWMutex
	lastPublish   time.Time
	publishCount  int64
	relaysPublished int64
}

// New creates a new publisher.
func New(cfg *config.Config, cache *cache.Client) (*Publisher, error) {
	p := &Publisher{
		cfg:   cfg,
		cache: cache,
	}

	// Parse private key (supports hex or nsec format)
	if cfg.PrivateKey != "" {
		sk, err := parsePrivateKey(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %w", err)
		}
		p.sk = sk

		// Derive public key
		pk, err := nostr.GetPublicKey(sk)
		if err != nil {
			return nil, fmt.Errorf("failed to derive public key: %w", err)
		}
		p.pk = pk

		slog.Info("publisher initialized", "pubkey", pk[:16]+"...")
	} else {
		slog.Warn("publisher has no private key configured, publishing disabled")
	}

	return p, nil
}

// parsePrivateKey parses a private key from hex or nsec format.
func parsePrivateKey(key string) (string, error) {
	key = strings.TrimSpace(key)

	// Check if it's nsec format
	if strings.HasPrefix(key, "nsec1") {
		_, data, err := nip19.Decode(key)
		if err != nil {
			return "", fmt.Errorf("invalid nsec: %w", err)
		}
		return data.(string), nil
	}

	// Assume hex format - validate it
	if len(key) != 64 {
		return "", fmt.Errorf("hex key must be 64 characters, got %d", len(key))
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", fmt.Errorf("invalid hex key: %w", err)
	}

	return key, nil
}

// Start begins the publishing loop.
func (p *Publisher) Start(ctx context.Context) {
	if p.sk == "" {
		slog.Info("publisher not starting - no private key configured")
		return
	}

	slog.Info("publisher starting",
		"interval_minutes", p.cfg.PublishInterval,
		"relays", p.cfg.PublishRelays,
	)

	ticker := time.NewTicker(time.Duration(p.cfg.PublishInterval) * time.Minute)
	defer ticker.Stop()

	// Publish immediately on startup
	p.publishAll(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("publisher stopping")
			return
		case <-ticker.C:
			p.publishAll(ctx)
		}
	}
}

// publishAll publishes all relay entries to configured relays.
func (p *Publisher) publishAll(ctx context.Context) {
	start := time.Now()
	defer func() {
		metrics.PublishDurationSeconds.Observe(time.Since(start).Seconds())
		metrics.PublishCyclesTotal.Inc()
	}()

	// Get all relay URLs from cache
	urls, err := p.cache.GetAllRelayURLs(ctx)
	if err != nil {
		slog.Error("failed to get relay URLs for publishing", "error", err)
		metrics.PublishErrorsTotal.WithLabelValues("", "cache_error").Inc()
		return
	}

	if len(urls) == 0 {
		slog.Debug("no relays to publish")
		return
	}

	// Build events from cache
	var events []*nostr.Event
	for _, url := range urls {
		entry, err := p.cache.GetRelayEntry(ctx, url)
		if err != nil || entry == nil {
			continue
		}
		events = append(events, p.createEvent(entry))
	}

	slog.Info("publishing relay directory entries", "count", len(events))

	var published int64
	for _, relayURL := range p.cfg.PublishRelays {
		count := p.publishToRelay(ctx, relayURL, events)
		if count > published {
			published = count
		}
	}

	p.mu.Lock()
	p.lastPublish = time.Now()
	p.publishCount++
	p.relaysPublished = published
	p.mu.Unlock()

	slog.Info("published relay directory entries",
		"published", published,
		"total", len(events),
	)

	// Update stats
	p.cache.SetStat(ctx, "publisher:last_publish", time.Now().Unix())
	p.cache.SetStat(ctx, "publisher:relays_published", published)
}

// publishToRelay connects to a single relay, authenticates if needed, and publishes all events.
func (p *Publisher) publishToRelay(ctx context.Context, relayURL string, events []*nostr.Event) int64 {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect to publish relay", "url", relayURL, "error", err)
		metrics.PublishErrorsTotal.WithLabelValues(relayURL, "connection").Inc()
		return 0
	}
	defer relay.Close()

	var published int64
	authAttempted := false

	for _, event := range events {
		err := relay.Publish(ctx, *event)
		if err == nil {
			published++
			metrics.EventsPublishedTotal.WithLabelValues(relayURL).Inc()
			continue
		}

		// Check if auth is required
		errStr := err.Error()
		if strings.Contains(errStr, "auth-required") && !authAttempted {
			authAttempted = true
			slog.Debug("relay requires auth, authenticating", "url", relayURL)

			authErr := relay.Auth(ctx, func(authEvent *nostr.Event) error {
				return authEvent.Sign(p.sk)
			})
			if authErr != nil {
				slog.Warn("NIP-42 auth failed", "url", relayURL, "error", authErr)
				metrics.PublishErrorsTotal.WithLabelValues(relayURL, "auth_failed").Inc()
				return published
			}
			slog.Info("NIP-42 auth successful", "url", relayURL)

			// Retry this event after auth
			if retryErr := relay.Publish(ctx, *event); retryErr != nil {
				slog.Debug("publish still failed after auth", "url", relayURL, "error", retryErr)
				metrics.PublishErrorsTotal.WithLabelValues(relayURL, "publish_after_auth").Inc()
			} else {
				published++
				metrics.EventsPublishedTotal.WithLabelValues(relayURL).Inc()
			}
			continue
		}

		slog.Debug("failed to publish to relay", "url", relayURL, "error", err)
		metrics.PublishErrorsTotal.WithLabelValues(relayURL, "publish").Inc()
	}

	if published > 0 {
		slog.Debug("published to relay", "url", relayURL, "count", published)
	}

	return published
}

// createEvent creates a kind 30072 event from a relay entry.
func (p *Publisher) createEvent(entry *cache.RelayEntry) *nostr.Event {
	tags := nostr.Tags{
		{"d", entry.URL},
		{"relay", entry.URL},
		{"health", entry.Health},
		{"last_checked", strconv.FormatInt(entry.LastChecked.Unix(), 10)},
	}

	// Add optional tags
	if entry.Name != "" {
		tags = append(tags, nostr.Tag{"name", entry.Name})
	}
	if entry.Description != "" {
		tags = append(tags, nostr.Tag{"description", entry.Description})
	}
	if entry.Pubkey != "" {
		tags = append(tags, nostr.Tag{"operator", entry.Pubkey})
	}
	if entry.Software != "" {
		if entry.Version != "" {
			tags = append(tags, nostr.Tag{"software", entry.Software, entry.Version})
		} else {
			tags = append(tags, nostr.Tag{"software", entry.Software})
		}
	}
	if len(entry.SupportedNIPs) > 0 {
		nipTag := nostr.Tag{"nips"}
		for _, nip := range entry.SupportedNIPs {
			nipTag = append(nipTag, strconv.Itoa(nip))
		}
		tags = append(tags, nipTag)
	}
	if entry.LatencyMs > 0 {
		tags = append(tags, nostr.Tag{"latency_ms", strconv.Itoa(entry.LatencyMs)})
	}
	if entry.CountryCode != "" {
		tags = append(tags, nostr.Tag{"location", entry.CountryCode})
	}
	if entry.PaymentRequired {
		tags = append(tags, nostr.Tag{"payment", "paid"})
	} else {
		tags = append(tags, nostr.Tag{"payment", "free"})
	}
	if entry.AuthRequired {
		tags = append(tags, nostr.Tag{"admission", "auth"})
	} else {
		tags = append(tags, nostr.Tag{"admission", "open"})
	}

	// Community & segregation metadata
	if entry.ContentPolicy != "" {
		tags = append(tags, nostr.Tag{"content_policy", entry.ContentPolicy})
	}
	if entry.Moderation != "" {
		tags = append(tags, nostr.Tag{"moderation", entry.Moderation})
	}
	if entry.ModerationPolicy != "" {
		tags = append(tags, nostr.Tag{"moderation_policy", entry.ModerationPolicy})
	}
	if entry.Community != "" {
		tags = append(tags, nostr.Tag{"community", entry.Community})
	}
	for _, lang := range entry.Languages {
		tags = append(tags, nostr.Tag{"language", lang})
	}

	// Aggregated annotation data (topics and atmosphere with counts)
	for topic, count := range entry.Topics {
		tags = append(tags, nostr.Tag{"topic", topic, strconv.Itoa(count)})
	}
	for atm, count := range entry.Atmosphere {
		tags = append(tags, nostr.Tag{"atmosphere", atm, strconv.Itoa(count)})
	}

	// Set expiration (next publish cycle + buffer)
	expiresAt := time.Now().Add(time.Duration(p.cfg.PublishInterval*2) * time.Minute)
	tags = append(tags, nostr.Tag{"expires", strconv.FormatInt(expiresAt.Unix(), 10)})

	event := &nostr.Event{
		Kind:      30072,
		PubKey:    p.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	// Sign the event
	event.Sign(p.sk)

	return event
}

// GetPublicKey returns the publisher's public key.
func (p *Publisher) GetPublicKey() string {
	return p.pk
}

// GetLastPublish returns the time of the last publish cycle.
func (p *Publisher) GetLastPublish() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastPublish
}

// GetPublishCount returns the number of publish cycles completed.
func (p *Publisher) GetPublishCount() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.publishCount
}

// GetRelaysPublished returns the number of relays published in the last cycle.
func (p *Publisher) GetRelaysPublished() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.relaysPublished
}
