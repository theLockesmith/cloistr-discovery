// Package inventory handles relay inventory indexing.
// Implements Kind 30066 (Relay Inventory) from NDP.
// Tracks which relays have content from which pubkeys.
package inventory

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// Indexer subscribes to relay inventories and builds the routing index.
type Indexer struct {
	cfg   *config.Config
	cache *cache.Client

	mu           sync.RWMutex
	subscriptions map[string]context.CancelFunc
}

// NewIndexer creates a new inventory indexer.
func NewIndexer(cfg *config.Config, cache *cache.Client) *Indexer {
	return &Indexer{
		cfg:           cfg,
		cache:         cache,
		subscriptions: make(map[string]context.CancelFunc),
	}
}

// Start begins inventory indexing.
func (i *Indexer) Start(ctx context.Context) {
	slog.Info("inventory indexer started")

	// TODO: Subscribe to discovery relays for Kind 30066 events
	// For now, we'll process inventory events as they come in

	<-ctx.Done()
	i.stopAllSubscriptions()
	slog.Info("inventory indexer stopped")
}

// ProcessInventory processes a Kind 30066 relay inventory event.
// The event contains a bloom filter of pubkeys the relay has content for.
func (i *Indexer) ProcessInventory(ctx context.Context, event *nostr.Event) error {
	if event.Kind != 30066 {
		return nil
	}

	// Extract relay URL from 'd' tag
	var relayURL string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			relayURL = tag[1]
			break
		}
	}
	if relayURL == "" {
		slog.Warn("inventory event missing relay URL", "id", event.ID)
		return nil
	}

	// Extract pubkeys from 'p' tags
	// In a full implementation, this would use bloom filters for efficiency
	var pubkeys []string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			pubkeys = append(pubkeys, tag[1])
		}
	}

	ttl := time.Duration(i.cfg.InventoryTTL) * time.Hour

	// Index each pubkey -> relay mapping
	for _, pk := range pubkeys {
		if err := i.cache.SetPubkeyRelay(ctx, pk, relayURL, ttl); err != nil {
			slog.Error("failed to index pubkey relay", "pubkey", pk, "relay", relayURL, "error", err)
		}
	}

	slog.Debug("processed inventory", "relay", relayURL, "pubkeys", len(pubkeys))
	return nil
}

// SubscribeToRelay subscribes to a relay for inventory updates.
func (i *Indexer) SubscribeToRelay(ctx context.Context, relayURL string) error {
	i.mu.Lock()
	if _, exists := i.subscriptions[relayURL]; exists {
		i.mu.Unlock()
		return nil // Already subscribed
	}

	subCtx, cancel := context.WithCancel(ctx)
	i.subscriptions[relayURL] = cancel
	i.mu.Unlock()

	go func() {
		i.subscribeLoop(subCtx, relayURL)
	}()

	return nil
}

func (i *Indexer) subscribeLoop(ctx context.Context, relayURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		relay, err := nostr.RelayConnect(ctx, relayURL)
		if err != nil {
			slog.Error("failed to connect to relay", "url", relayURL, "error", err)
			time.Sleep(30 * time.Second)
			continue
		}

		// Subscribe to Kind 30066 events
		sub, err := relay.Subscribe(ctx, []nostr.Filter{
			{Kinds: []int{30066}},
		})
		if err != nil {
			slog.Error("failed to subscribe", "url", relayURL, "error", err)
			relay.Close()
			time.Sleep(30 * time.Second)
			continue
		}

		for {
			select {
			case <-ctx.Done():
				sub.Unsub()
				relay.Close()
				return
			case event, ok := <-sub.Events:
				if !ok {
					break
				}
				if err := i.ProcessInventory(ctx, event); err != nil {
					slog.Error("failed to process inventory", "error", err)
				}
			}
		}

		relay.Close()
		time.Sleep(5 * time.Second) // Brief delay before reconnect
	}
}

func (i *Indexer) stopAllSubscriptions() {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, cancel := range i.subscriptions {
		cancel()
	}
	i.subscriptions = make(map[string]context.CancelFunc)
}

// UnsubscribeFromRelay stops the subscription to a relay.
func (i *Indexer) UnsubscribeFromRelay(relayURL string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if cancel, exists := i.subscriptions[relayURL]; exists {
		cancel()
		delete(i.subscriptions, relayURL)
	}
}
