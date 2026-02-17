package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/backoff"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// NIP66Consumer discovers relays by consuming NIP-66 relay monitor events (kind 30166).
type NIP66Consumer struct {
	cfg     *config.Config
	output  chan<- DiscoveredRelay
	backoff *backoff.Tracker

	mu          sync.RWMutex
	lastConsume time.Time
}

// NewNIP66Consumer creates a new NIP-66 consumer.
func NewNIP66Consumer(cfg *config.Config, output chan<- DiscoveredRelay) *NIP66Consumer {
	return &NIP66Consumer{
		cfg:     cfg,
		output:  output,
		backoff: backoff.DefaultTracker(),
	}
}

// Start begins consuming NIP-66 events from seed relays.
func (n *NIP66Consumer) Start(ctx context.Context) {
	slog.Info("NIP-66 consumer starting")

	// Subscribe to seed relays for kind 30166 events
	for _, relayURL := range n.cfg.SeedRelays {
		go n.subscribeToRelay(ctx, relayURL)
	}

	<-ctx.Done()
	slog.Info("NIP-66 consumer stopped")
}

// subscribeToRelay subscribes to a relay for NIP-66 events.
func (n *NIP66Consumer) subscribeToRelay(ctx context.Context, relayURL string) {
	b := n.backoff.Get(relayURL)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		connected := n.consumeFromRelay(ctx, relayURL)

		if connected {
			// Reset backoff on successful connection
			b.Reset()
		}

		// Wait with exponential backoff before reconnecting
		wait := b.Next()
		slog.Debug("waiting before reconnecting", "relay", relayURL, "wait", wait)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// consumeFromRelay connects to a relay and consumes NIP-66 events.
// Returns true if connection was successful (even if it later disconnected).
func (n *NIP66Consumer) consumeFromRelay(ctx context.Context, relayURL string) bool {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect for NIP-66", "url", relayURL, "error", err)
		return false
	}
	defer relay.Close()

	// Subscribe to kind 30166 (relay monitor) events
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{30166},
			// No limit - we want ongoing subscription
		},
	})
	if err != nil {
		slog.Debug("failed to subscribe for NIP-66", "url", relayURL, "error", err)
		return false
	}
	defer sub.Unsub()

	metrics.NIP66ConnectionsActive.Inc()
	defer metrics.NIP66ConnectionsActive.Dec()

	slog.Info("subscribed for NIP-66 events", "relay", relayURL)

eventLoop:
	for {
		select {
		case <-ctx.Done():
			return true
		case event, ok := <-sub.Events:
			if !ok {
				break eventLoop
			}
			n.processNIP66Event(ctx, event)
		}
	}

	return true
}

// processNIP66Event extracts relay URLs from a NIP-66 event.
func (n *NIP66Consumer) processNIP66Event(ctx context.Context, event *nostr.Event) {
	if event.Kind != 30166 {
		return
	}

	metrics.NIP66EventsConsumed.Inc()

	// NIP-66 uses "d" tag for the relay URL
	var relayURL string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			relayURL = tag[1]
			break
		}
	}

	if relayURL == "" {
		return
	}

	n.mu.Lock()
	n.lastConsume = time.Now()
	n.mu.Unlock()

	select {
	case <-ctx.Done():
		return
	case n.output <- DiscoveredRelay{URL: relayURL, Source: "nip66"}:
		metrics.NIP66RelaysDiscovered.Inc()
		slog.Debug("discovered relay from NIP-66", "url", relayURL, "monitor", event.PubKey[:16])
	}
}

// LastConsume returns the time of the last event consumed.
func (n *NIP66Consumer) LastConsume() time.Time {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.lastConsume
}
