package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/config"
)

// PeerDiscovery discovers relays from trusted discovery service peers (kind 30072).
type PeerDiscovery struct {
	cfg    *config.Config
	cache  *cache.Client
	output chan<- DiscoveredRelay

	mu           sync.RWMutex
	lastDiscover time.Time
}

// NewPeerDiscovery creates a new peer discovery component.
func NewPeerDiscovery(cfg *config.Config, cache *cache.Client, output chan<- DiscoveredRelay) *PeerDiscovery {
	return &PeerDiscovery{
		cfg:    cfg,
		cache:  cache,
		output: output,
	}
}

// Start begins discovering relays from trusted peers.
func (p *PeerDiscovery) Start(ctx context.Context) {
	slog.Info("peer discovery starting")

	// Subscribe to seed relays for kind 30069 events from trusted peers
	for _, relayURL := range p.cfg.SeedRelays {
		go p.subscribeToRelay(ctx, relayURL)
	}

	<-ctx.Done()
	slog.Info("peer discovery stopped")
}

// subscribeToRelay subscribes to a relay for peer discovery events.
func (p *PeerDiscovery) subscribeToRelay(ctx context.Context, relayURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		p.discoverFromRelay(ctx, relayURL)

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(peerDiscoveryTimeout):
		}
	}
}

// discoverFromRelay connects to a relay and discovers from peer events.
func (p *PeerDiscovery) discoverFromRelay(ctx context.Context, relayURL string) {
	// Get trusted peers from config and cache
	trustedPeers := p.getTrustedPeers(ctx)
	if len(trustedPeers) == 0 {
		slog.Debug("no trusted peers configured, skipping peer discovery")
		return
	}

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect for peer discovery", "url", relayURL, "error", err)
		return
	}
	defer relay.Close()

	// Subscribe to kind 30072 (relay directory entry) events from trusted peers
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{30072},
			Authors: trustedPeers,
		},
	})
	if err != nil {
		slog.Debug("failed to subscribe for peer discovery", "url", relayURL, "error", err)
		return
	}
	defer sub.Unsub()

	slog.Debug("subscribed for peer discovery events",
		"relay", relayURL,
		"trusted_peers", len(trustedPeers),
	)

eventLoop:
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events:
			if !ok {
				break eventLoop
			}
			p.processPeerEvent(ctx, event)
		}
	}
}

// getTrustedPeers returns the combined list of trusted peers from config and cache.
func (p *PeerDiscovery) getTrustedPeers(ctx context.Context) []string {
	peers := make(map[string]bool)

	// Add from config
	for _, pubkey := range p.cfg.TrustedDiscoveryPeers {
		if pubkey != "" {
			peers[pubkey] = true
		}
	}

	// Add from cache
	cachedPeers, err := p.cache.GetTrustedPeers(ctx)
	if err == nil {
		for _, pubkey := range cachedPeers {
			peers[pubkey] = true
		}
	}

	result := make([]string, 0, len(peers))
	for pubkey := range peers {
		result = append(result, pubkey)
	}

	return result
}

// processPeerEvent extracts relay URLs from a peer's relay directory entry.
func (p *PeerDiscovery) processPeerEvent(ctx context.Context, event *nostr.Event) {
	if event.Kind != 30072 {
		return
	}

	// Extract relay URL from "d" tag or "relay" tag
	var relayURL string
	for _, tag := range event.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "d":
				relayURL = tag[1]
			case "relay":
				if relayURL == "" {
					relayURL = tag[1]
				}
			}
		}
	}

	if relayURL == "" {
		return
	}

	p.mu.Lock()
	p.lastDiscover = time.Now()
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return
	case p.output <- DiscoveredRelay{URL: relayURL, Source: "peers"}:
		slog.Debug("discovered relay from peer",
			"url", relayURL,
			"peer", event.PubKey[:16],
		)
	}
}

// LastDiscover returns the time of the last discovery from peers.
func (p *PeerDiscovery) LastDiscover() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastDiscover
}
