package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// NIP65Crawler discovers relays by crawling NIP-65 relay list events (kind 10002).
type NIP65Crawler struct {
	cfg    *config.Config
	cache  *cache.Client
	output chan<- DiscoveredRelay

	mu        sync.RWMutex
	lastCrawl time.Time
}

// NewNIP65Crawler creates a new NIP-65 crawler.
func NewNIP65Crawler(cfg *config.Config, cache *cache.Client, output chan<- DiscoveredRelay) *NIP65Crawler {
	return &NIP65Crawler{
		cfg:    cfg,
		cache:  cache,
		output: output,
	}
}

// Start begins periodic crawling of NIP-65 events from seed relays.
func (n *NIP65Crawler) Start(ctx context.Context) {
	slog.Info("NIP-65 crawler starting", "interval_min", n.cfg.NIP65CrawlInterval)

	// Initial crawl
	n.crawl(ctx)

	ticker := time.NewTicker(time.Duration(n.cfg.NIP65CrawlInterval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("NIP-65 crawler stopped")
			return
		case <-ticker.C:
			n.crawl(ctx)
		}
	}
}

// crawl fetches NIP-65 events from seed relays and extracts relay URLs.
func (n *NIP65Crawler) crawl(ctx context.Context) {
	start := time.Now()
	defer func() {
		metrics.NIP65CrawlDurationSeconds.Observe(time.Since(start).Seconds())
		metrics.NIP65CrawlsTotal.Inc()
	}()

	slog.Debug("starting NIP-65 crawl")

	// Get all known relays (seed + discovered)
	relays := n.cfg.SeedRelays

	// Also get from whitelist
	whitelist, err := n.cache.GetWhitelist(ctx)
	if err == nil {
		relays = append(relays, whitelist...)
	}

	// Deduplicate
	seen := make(map[string]bool)
	uniqueRelays := make([]string, 0, len(relays))
	for _, r := range relays {
		if !seen[r] {
			seen[r] = true
			uniqueRelays = append(uniqueRelays, r)
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // Limit concurrent connections

	for _, relayURL := range uniqueRelays {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			n.crawlRelay(ctx, url)
		}(relayURL)
	}

	wg.Wait()

	n.mu.Lock()
	n.lastCrawl = time.Now()
	n.mu.Unlock()

	slog.Debug("NIP-65 crawl complete")
}

// crawlRelay fetches NIP-65 events from a single relay.
func (n *NIP65Crawler) crawlRelay(ctx context.Context, relayURL string) {
	// Create a timeout context for this relay
	crawlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	relay, err := nostr.RelayConnect(crawlCtx, relayURL)
	if err != nil {
		slog.Debug("failed to connect for NIP-65 crawl", "url", relayURL, "error", err)
		return
	}
	defer relay.Close()

	// Subscribe to recent kind 10002 events
	sub, err := relay.Subscribe(crawlCtx, []nostr.Filter{
		{
			Kinds: []int{10002},
			Limit: 500, // Get a sample of recent relay lists
		},
	})
	if err != nil {
		slog.Debug("failed to subscribe for NIP-65", "url", relayURL, "error", err)
		return
	}
	defer sub.Unsub()

	// Process events with timeout
	timeout := time.After(20 * time.Second)
	eventCount := 0
	relayCount := 0

eventLoop:
	for {
		select {
		case <-crawlCtx.Done():
			break eventLoop
		case <-timeout:
			break eventLoop
		case event, ok := <-sub.Events:
			if !ok {
				break eventLoop
			}
			eventCount++
			relayCount += n.processNIP65Event(crawlCtx, event)
		}
	}

	if eventCount > 0 {
		slog.Debug("processed NIP-65 events",
			"relay", relayURL,
			"events", eventCount,
			"relays_discovered", relayCount,
		)
	}
}

// processNIP65Event extracts relay URLs from a NIP-65 event.
func (n *NIP65Crawler) processNIP65Event(ctx context.Context, event *nostr.Event) int {
	if event.Kind != 10002 {
		return 0
	}

	metrics.NIP65EventsProcessed.Inc()

	count := 0
	for _, tag := range event.Tags {
		// NIP-65 uses "r" tags: ["r", "wss://relay.example.com", "read"|"write"]
		if len(tag) >= 2 && tag[0] == "r" {
			url := tag[1]
			if url != "" {
				select {
				case <-ctx.Done():
					return count
				case n.output <- DiscoveredRelay{URL: url, Source: "nip65"}:
					count++
					metrics.NIP65RelaysDiscovered.Inc()
				}
			}
		}
	}

	return count
}

// LastCrawl returns the time of the last crawl.
func (n *NIP65Crawler) LastCrawl() time.Time {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.lastCrawl
}
