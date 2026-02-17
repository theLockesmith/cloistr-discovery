package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
)

// HostedFetcher fetches relay lists from a hosted URL.
type HostedFetcher struct {
	cfg    *config.Config
	client *http.Client
	output chan<- DiscoveredRelay

	mu        sync.RWMutex
	lastFetch time.Time
}

// NewHostedFetcher creates a new hosted relay list fetcher.
func NewHostedFetcher(cfg *config.Config, output chan<- DiscoveredRelay) *HostedFetcher {
	return &HostedFetcher{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		output: output,
	}
}

// Start begins periodic fetching of the hosted relay list.
func (h *HostedFetcher) Start(ctx context.Context) {
	slog.Info("hosted fetcher starting", "url", h.cfg.HostedRelayListURL)

	// Initial fetch
	h.fetch(ctx)

	// If interval is 0, only fetch once
	if h.cfg.HostedRelayListInterval == 0 {
		slog.Info("hosted fetcher configured for single fetch, stopping")
		return
	}

	ticker := time.NewTicker(time.Duration(h.cfg.HostedRelayListInterval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("hosted fetcher stopped")
			return
		case <-ticker.C:
			h.fetch(ctx)
		}
	}
}

// fetch retrieves the relay list from the configured URL.
func (h *HostedFetcher) fetch(ctx context.Context) {
	slog.Debug("fetching hosted relay list", "url", h.cfg.HostedRelayListURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.HostedRelayListURL, nil)
	if err != nil {
		slog.Error("failed to create request", "error", err)
		return
	}

	resp, err := h.client.Do(req)
	if err != nil {
		slog.Error("failed to fetch hosted relay list", "url", h.cfg.HostedRelayListURL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("hosted relay list returned non-OK status", "status", resp.StatusCode)
		return
	}

	// Try to parse as JSON first, then as newline-separated text
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read response body", "error", err)
		return
	}

	var relays []string

	// Try JSON array first
	if err := json.Unmarshal(body, &relays); err != nil {
		// Try as newline-separated text
		relays = h.parseTextList(string(body))
	}

	h.mu.Lock()
	h.lastFetch = time.Now()
	h.mu.Unlock()

	slog.Info("fetched hosted relay list", "count", len(relays))

	// Send discovered relays
	for _, url := range relays {
		url = strings.TrimSpace(url)
		if url == "" || strings.HasPrefix(url, "#") {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case h.output <- DiscoveredRelay{URL: url, Source: "hosted"}:
		}
	}
}

// parseTextList parses a newline-separated list of relay URLs.
func (h *HostedFetcher) parseTextList(text string) []string {
	var relays []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			relays = append(relays, line)
		}
	}
	return relays
}

// LastFetch returns the time of the last successful fetch.
func (h *HostedFetcher) LastFetch() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastFetch
}
