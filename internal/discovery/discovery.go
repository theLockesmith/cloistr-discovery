// Package discovery coordinates relay discovery from multiple sources.
package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/config"
)

const (
	// discoveryChannelBuffer is the buffer size for the internal discovery channel.
	discoveryChannelBuffer = 1000

	// nip65ConcurrentConnections limits parallel connections during NIP-65 crawls.
	nip65ConcurrentConnections = 5

	// nip65CrawlTimeout is the timeout for connecting to a single relay during crawl.
	nip65CrawlTimeout = 30 * time.Second

	// nip65EventTimeout is the timeout for receiving events from a relay.
	nip65EventTimeout = 20 * time.Second

	// nip65EventLimit is the maximum number of NIP-65 events to fetch per relay.
	nip65EventLimit = 500

	// peerDiscoveryTimeout is the timeout for peer discovery operations.
	peerDiscoveryTimeout = 30 * time.Second

	// hostedFetchTimeout is the timeout for fetching hosted relay lists.
	hostedFetchTimeout = 30 * time.Second
)

// DiscoveredRelay represents a relay discovered from a source.
type DiscoveredRelay struct {
	URL    string
	Source string // "hosted", "nip65", "nip66", "peers", "manual"
}

// Coordinator manages all discovery sources and deduplicates discovered relays.
type Coordinator struct {
	cfg   *config.Config
	cache *cache.Client

	// Channel to send discovered relays to the relay monitor
	output chan<- string

	// Internal channel for sources to send discoveries
	discoveries chan DiscoveredRelay

	// Sources
	hostedFetcher *HostedFetcher
	nip65Crawler  *NIP65Crawler
	nip66Consumer *NIP66Consumer
	peerDiscovery *PeerDiscovery

	mu      sync.RWMutex
	running bool
}

// NewCoordinator creates a new discovery coordinator.
func NewCoordinator(cfg *config.Config, cache *cache.Client, output chan<- string) *Coordinator {
	discoveries := make(chan DiscoveredRelay, discoveryChannelBuffer)

	c := &Coordinator{
		cfg:         cfg,
		cache:       cache,
		output:      output,
		discoveries: discoveries,
	}

	// Initialize sources based on config
	if cfg.HostedRelayListURL != "" {
		c.hostedFetcher = NewHostedFetcher(cfg, discoveries)
	}

	if cfg.NIP65CrawlEnabled {
		c.nip65Crawler = NewNIP65Crawler(cfg, cache, discoveries)
	}

	if cfg.NIP66Enabled {
		c.nip66Consumer = NewNIP66Consumer(cfg, cache, discoveries)
	}

	if cfg.PeerDiscoveryEnabled {
		c.peerDiscovery = NewPeerDiscovery(cfg, cache, discoveries)
	}

	return c
}

// Start begins the discovery coordinator and all enabled sources.
func (c *Coordinator) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	slog.Info("discovery coordinator starting",
		"hosted", c.hostedFetcher != nil,
		"nip65", c.nip65Crawler != nil,
		"nip66", c.nip66Consumer != nil,
		"peers", c.peerDiscovery != nil,
	)

	// Start discovery processor
	go c.processDiscoveries(ctx)

	// Start enabled sources
	if c.hostedFetcher != nil {
		go c.hostedFetcher.Start(ctx)
	}

	if c.nip65Crawler != nil {
		go c.nip65Crawler.Start(ctx)
	}

	if c.nip66Consumer != nil {
		go c.nip66Consumer.Start(ctx)
	}

	if c.peerDiscovery != nil {
		go c.peerDiscovery.Start(ctx)
	}

	<-ctx.Done()
	slog.Info("discovery coordinator stopped")
}

// processDiscoveries receives discovered relays, deduplicates, filters, and forwards them.
func (c *Coordinator) processDiscoveries(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case discovered := <-c.discoveries:
			c.handleDiscoveredRelay(ctx, discovered)
		}
	}
}

// handleDiscoveredRelay processes a single discovered relay.
func (c *Coordinator) handleDiscoveredRelay(ctx context.Context, discovered DiscoveredRelay) {
	url := normalizeRelayURL(discovered.URL)
	if url == "" {
		return
	}

	// Check blacklist
	isBlacklisted, err := c.cache.IsBlacklisted(ctx, url)
	if err != nil {
		slog.Error("failed to check blacklist", "url", url, "error", err)
		return
	}
	if isBlacklisted {
		slog.Debug("relay is blacklisted, skipping", "url", url)
		return
	}

	// Check if already seen (deduplication)
	isNew, err := c.cache.MarkRelaySeen(ctx, url)
	if err != nil {
		slog.Error("failed to mark relay seen", "url", url, "error", err)
		return
	}
	if !isNew {
		// Already processed this relay
		return
	}

	// Update stats
	c.cache.IncrementStat(ctx, "discovery:"+discovered.Source)
	c.cache.IncrementStat(ctx, "discovery:total")

	slog.Debug("discovered new relay",
		"url", url,
		"source", discovered.Source,
	)

	// Forward to relay monitor (non-blocking)
	select {
	case c.output <- url:
	default:
		slog.Warn("relay monitor channel full, dropping relay", "url", url)
	}
}

// SubmitRelay manually submits a relay for discovery.
func (c *Coordinator) SubmitRelay(ctx context.Context, url string) error {
	c.discoveries <- DiscoveredRelay{
		URL:    url,
		Source: "manual",
	}
	return nil
}

// normalizeRelayURL normalizes a relay URL.
func normalizeRelayURL(url string) string {
	if url == "" {
		return ""
	}

	// Ensure wss:// or ws:// prefix
	if len(url) < 6 {
		return ""
	}

	if url[:6] != "wss://" && url[:5] != "ws://" {
		// Try adding wss://
		url = "wss://" + url
	}

	// Remove trailing slash
	if url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}

	return url
}

// GetStats returns discovery statistics.
func (c *Coordinator) GetStats(ctx context.Context) (map[string]int64, error) {
	return c.cache.GetAllStats(ctx)
}

// LastFetchTimes holds the last fetch time for each source.
type LastFetchTimes struct {
	Hosted time.Time `json:"hosted,omitempty"`
	NIP65  time.Time `json:"nip65,omitempty"`
	NIP66  time.Time `json:"nip66,omitempty"`
	Peers  time.Time `json:"peers,omitempty"`
}

// GetLastFetchTimes returns the last fetch time for each source.
func (c *Coordinator) GetLastFetchTimes() LastFetchTimes {
	times := LastFetchTimes{}

	if c.hostedFetcher != nil {
		times.Hosted = c.hostedFetcher.LastFetch()
	}
	if c.nip65Crawler != nil {
		times.NIP65 = c.nip65Crawler.LastCrawl()
	}
	if c.nip66Consumer != nil {
		times.NIP66 = c.nip66Consumer.LastConsume()
	}
	if c.peerDiscovery != nil {
		times.Peers = c.peerDiscovery.LastDiscover()
	}

	return times
}

// NIP65LastCrawl returns the last crawl time for NIP-65, or zero if disabled.
func (c *Coordinator) NIP65LastCrawl() time.Time {
	if c.nip65Crawler != nil {
		return c.nip65Crawler.LastCrawl()
	}
	return time.Time{}
}

// NIP66LastConsume returns the last consume time for NIP-66, or zero if disabled.
func (c *Coordinator) NIP66LastConsume() time.Time {
	if c.nip66Consumer != nil {
		return c.nip66Consumer.LastConsume()
	}
	return time.Time{}
}

// IsNIP65Enabled returns true if NIP-65 crawling is enabled.
func (c *Coordinator) IsNIP65Enabled() bool {
	return c.nip65Crawler != nil
}

// IsNIP66Enabled returns true if NIP-66 consumption is enabled.
func (c *Coordinator) IsNIP66Enabled() bool {
	return c.nip66Consumer != nil
}
