// Package api provides HTTP handlers for the discovery service.
package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

const (
	// nip65FetchTimeout is the maximum time to wait for NIP-65 event fetch.
	nip65FetchTimeout = 10 * time.Second

	// nip65MaxRelaysToQuery is the number of seed relays to query in parallel.
	nip65MaxRelaysToQuery = 3
)

// ErrNoEventsFound indicates no NIP-65 events were found for the pubkey.
var ErrNoEventsFound = errors.New("no NIP-65 events found")

// UserRelayResponse is the response for user relay list queries.
type UserRelayResponse struct {
	Pubkey      string           `json:"pubkey"`
	Relays      []UserRelayEntry `json:"relays"`
	Total       int              `json:"total"`
	FetchedAt   time.Time        `json:"fetched_at"`
	CacheHit    bool             `json:"cache_hit"`
	FetchErrors []string         `json:"fetch_errors,omitempty"`
}

// UserRelayEntry represents a single relay from the user's NIP-65 list.
type UserRelayEntry struct {
	URL       string           `json:"url"`
	Read      bool             `json:"read"`
	Write     bool             `json:"write"`
	Monitored bool             `json:"monitored"`
	Health    *RelayHealthInfo `json:"health,omitempty"`
}

// RelayHealthInfo contains health information from our relay monitoring.
type RelayHealthInfo struct {
	Status        string    `json:"status"`
	LatencyMs     int       `json:"latency_ms"`
	LastChecked   time.Time `json:"last_checked"`
	SupportedNIPs []int     `json:"supported_nips,omitempty"`
	Name          string    `json:"name,omitempty"`
}

// UserRelaysHandler handles GET /api/v1/users/{pubkey}/relays
// Fetches a user's NIP-65 relay list and enriches with monitored relay health data.
func (s *Server) UserRelaysHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.NIP65UserLookupDurationSeconds.Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse pubkey from path: /api/v1/users/{pubkey}/relays
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	path = strings.TrimSuffix(path, "/relays")
	pubkey := path

	// Validate pubkey
	if err := validatePubkey(pubkey); err != nil {
		metrics.NIP65UserLookupErrors.WithLabelValues("invalid_pubkey").Inc()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Check cache first
	cached, err := s.cache.GetUserNIP65(ctx, pubkey)
	if err != nil {
		slog.Error("failed to get cached NIP-65 data", "pubkey", pubkey, "error", err)
	}

	var entries []UserRelayEntry
	var fetchedAt time.Time
	var cacheHit bool
	var fetchErrors []string

	if cached != nil {
		// Cache hit
		cacheHit = true
		fetchedAt = cached.FetchedAt
		entries = make([]UserRelayEntry, len(cached.Relays))
		for i, r := range cached.Relays {
			entries[i] = UserRelayEntry{
				URL:   r.URL,
				Read:  r.Read,
				Write: r.Write,
			}
		}
		metrics.NIP65UserLookupTotal.WithLabelValues("true").Inc()
	} else {
		// Cache miss - return 404 immediately instead of blocking on relay fetches
		// The NIP-65 crawler will eventually populate the cache for active users
		metrics.NIP65UserLookupTotal.WithLabelValues("false").Inc()
		metrics.NIP65UserLookupErrors.WithLabelValues("cache_miss").Inc()
		slog.Debug("NIP-65 cache miss", "pubkey", pubkey[:16]+"...")
		http.Error(w, "no relay list found for pubkey", http.StatusNotFound)
		return
	}

	// Enrich with health data from monitored relays
	entries = s.enrichWithHealth(ctx, entries)

	resp := UserRelayResponse{
		Pubkey:      pubkey,
		Relays:      entries,
		Total:       len(entries),
		FetchedAt:   fetchedAt,
		CacheHit:    cacheHit,
		FetchErrors: fetchErrors,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// validatePubkey validates a Nostr public key (64 hex characters).
func validatePubkey(pubkey string) error {
	if pubkey == "" {
		return fmt.Errorf("pubkey is required")
	}
	if len(pubkey) != 64 {
		return fmt.Errorf("pubkey must be 64 hex characters")
	}
	if _, err := hex.DecodeString(pubkey); err != nil {
		return fmt.Errorf("pubkey must be valid hex")
	}
	return nil
}

// fetchUserNIP65 fetches Kind 10002 events for a pubkey from seed relays.
// Returns relay entries with read/write markers and a list of relay URLs that failed.
func (s *Server) fetchUserNIP65(ctx context.Context, pubkey string) ([]UserRelayEntry, []string, error) {
	// Select relays to query
	relaysToQuery := s.cfg.SeedRelays
	if len(relaysToQuery) > nip65MaxRelaysToQuery {
		relaysToQuery = relaysToQuery[:nip65MaxRelaysToQuery]
	}

	// Create timeout context
	fetchCtx, cancel := context.WithTimeout(ctx, nip65FetchTimeout)
	defer cancel()

	// Fetch in parallel
	var wg sync.WaitGroup
	results := make(chan *nostr.Event, len(relaysToQuery))
	errorsCh := make(chan string, len(relaysToQuery))

	for _, relayURL := range relaysToQuery {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			event, err := fetchNIP65FromRelay(fetchCtx, url, pubkey)
			if err != nil {
				slog.Debug("failed to fetch NIP-65 from relay", "relay", url, "pubkey", pubkey, "error", err)
				errorsCh <- url
				return
			}
			if event != nil {
				results <- event
			}
		}(relayURL)
	}

	// Wait and close channels
	go func() {
		wg.Wait()
		close(results)
		close(errorsCh)
	}()

	// Collect results, take newest event by created_at
	var newestEvent *nostr.Event
	var fetchErrors []string

	for event := range results {
		if newestEvent == nil || event.CreatedAt > newestEvent.CreatedAt {
			newestEvent = event
		}
	}
	for url := range errorsCh {
		fetchErrors = append(fetchErrors, url)
	}

	if newestEvent == nil {
		return nil, fetchErrors, ErrNoEventsFound
	}

	return parseNIP65Tags(newestEvent.Tags), fetchErrors, nil
}

// fetchNIP65FromRelay fetches the NIP-65 event for a pubkey from a single relay.
func fetchNIP65FromRelay(ctx context.Context, relayURL, pubkey string) (*nostr.Event, error) {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer relay.Close()

	// Subscribe to kind 10002 events from this pubkey
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{10002},
			Authors: []string{pubkey},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	defer sub.Unsub()

	// Wait for event with timeout
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case event, ok := <-sub.Events:
		if !ok || event == nil {
			return nil, nil
		}
		return event, nil
	case <-time.After(5 * time.Second):
		return nil, nil
	}
}

// parseNIP65Tags extracts relay URLs and read/write markers from NIP-65 event tags.
func parseNIP65Tags(tags nostr.Tags) []UserRelayEntry {
	var entries []UserRelayEntry

	for _, tag := range tags {
		// NIP-65 uses "r" tags: ["r", "wss://relay.example.com", "read"|"write"]
		// Third element is optional; if missing, relay is used for both read and write
		if len(tag) >= 2 && tag[0] == "r" {
			url := tag[1]
			if url == "" {
				continue
			}

			entry := UserRelayEntry{
				URL:   url,
				Read:  true,
				Write: true,
			}

			if len(tag) >= 3 {
				marker := tag[2]
				if marker == "read" {
					entry.Write = false
				} else if marker == "write" {
					entry.Read = false
				}
			}

			entries = append(entries, entry)
		}
	}

	return entries
}

// enrichWithHealth adds health information from monitored relays.
func (s *Server) enrichWithHealth(ctx context.Context, entries []UserRelayEntry) []UserRelayEntry {
	if len(entries) == 0 {
		return entries
	}

	// Collect all relay URLs
	urls := make([]string, len(entries))
	for i, e := range entries {
		urls[i] = e.URL
	}

	// Batch fetch from cache
	relayEntries, err := s.cache.GetRelayEntriesBatch(ctx, urls)
	if err != nil {
		slog.Error("failed to batch fetch relay entries for enrichment", "error", err)
		return entries
	}

	// Enrich with health data
	for i, relayEntry := range relayEntries {
		if relayEntry != nil {
			entries[i].Monitored = true
			entries[i].Health = &RelayHealthInfo{
				Status:        relayEntry.Health,
				LatencyMs:     relayEntry.LatencyMs,
				LastChecked:   relayEntry.LastChecked,
				SupportedNIPs: relayEntry.SupportedNIPs,
				Name:          relayEntry.Name,
			}
		}
	}

	return entries
}
