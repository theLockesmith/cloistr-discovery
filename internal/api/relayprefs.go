// Package api provides HTTP handlers for the discovery service.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

const (
	// relayPrefsFetchTimeout is the maximum time to wait for relay prefs fetch.
	relayPrefsFetchTimeout = 10 * time.Second

	// cloistrRelayURL is the default relay for querying cloistr-specific events.
	cloistrRelayURL = "wss://relay.cloistr.xyz"

	// cloistrRelaysDTag is the d-tag for cloistr relay preferences (kind:30078).
	cloistrRelaysDTag = "cloistr-relays"
)

// RelayPrefsResponse is the response for relay preferences queries.
type RelayPrefsResponse struct {
	Pubkey   string            `json:"pubkey"`
	Relays   []RelayPrefsEntry `json:"relays"`
	Source   string            `json:"source"` // "cloistr-relays", "nip65", or "default"
	CachedAt time.Time         `json:"cached_at"`
}

// RelayPrefsEntry represents a single relay preference.
type RelayPrefsEntry struct {
	URL   string `json:"url"`
	Read  bool   `json:"read"`
	Write bool   `json:"write"`
}

// RelayPrefsHandler handles GET /api/v1/relay-prefs/{pubkey}
// Returns the user's relay preferences, checking cloistr-relays (kind:30078) first,
// then falling back to NIP-65 (kind:10002), then returning default empty list.
func (s *Server) RelayPrefsHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.QueryDurationSeconds.WithLabelValues("relay_prefs").Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.QueriesTotal.WithLabelValues("relay_prefs").Inc()

	// Parse pubkey from path: /api/v1/relay-prefs/{pubkey}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/relay-prefs/")
	pubkey := strings.TrimSuffix(path, "/")

	// Validate pubkey
	if err := validatePubkey(pubkey); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Check cache first
	cached, err := s.cache.GetRelayPrefs(ctx, pubkey)
	if err != nil {
		slog.Error("failed to get cached relay prefs", "pubkey", pubkey, "error", err)
	}

	if cached != nil {
		// Cache hit - return immediately
		resp := RelayPrefsResponse{
			Pubkey:   cached.Pubkey,
			Source:   cached.Source,
			CachedAt: cached.CachedAt,
			Relays:   make([]RelayPrefsEntry, len(cached.Relays)),
		}
		for i, r := range cached.Relays {
			resp.Relays[i] = RelayPrefsEntry{
				URL:   r.URL,
				Read:  r.Read,
				Write: r.Write,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Cache miss - fetch from relay
	relays, source, fetchErr := s.fetchRelayPrefs(ctx, pubkey)
	if fetchErr != nil {
		slog.Error("failed to fetch relay prefs", "pubkey", pubkey, "error", fetchErr)
		// On error, still return default
		relays = []RelayPrefsEntry{}
		source = "default"
	}

	// Ensure relays is never nil (JSON serializes nil as null, but clients expect [])
	if relays == nil {
		relays = []RelayPrefsEntry{}
	}

	cachedAt := time.Now()

	// Cache the result
	cacheEntry := &cache.RelayPrefsEntry{
		Pubkey:   pubkey,
		Source:   source,
		CachedAt: cachedAt,
		Relays:   make([]cache.UserRelayData, len(relays)),
	}
	for i, r := range relays {
		cacheEntry.Relays[i] = cache.UserRelayData{
			URL:   r.URL,
			Read:  r.Read,
			Write: r.Write,
		}
	}
	if err := s.cache.SetRelayPrefs(ctx, cacheEntry); err != nil {
		slog.Error("failed to cache relay prefs", "pubkey", pubkey, "error", err)
	}

	resp := RelayPrefsResponse{
		Pubkey:   pubkey,
		Relays:   relays,
		Source:   source,
		CachedAt: cachedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// fetchRelayPrefs fetches relay preferences for a pubkey.
// Tries kind:30078 d=cloistr-relays first, then kind:10002 (NIP-65).
// Returns empty list with source="default" if nothing found.
func (s *Server) fetchRelayPrefs(ctx context.Context, pubkey string) ([]RelayPrefsEntry, string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, relayPrefsFetchTimeout)
	defer cancel()

	// Connect to relay.cloistr.xyz
	relay, err := nostr.RelayConnect(fetchCtx, cloistrRelayURL)
	if err != nil {
		slog.Debug("failed to connect to cloistr relay", "error", err)
		return nil, "default", err
	}
	defer relay.Close()

	// First try kind:30078 d=cloistr-relays
	cloistrEvent, err := s.fetchCloistrRelays(fetchCtx, relay, pubkey)
	if err != nil {
		slog.Debug("failed to fetch cloistr-relays event", "pubkey", pubkey, "error", err)
	}
	if cloistrEvent != nil {
		relays := parseRelayTags(cloistrEvent.Tags)
		if len(relays) > 0 {
			return relays, "cloistr-relays", nil
		}
	}

	// Fall back to kind:10002 (NIP-65)
	nip65Event, err := s.fetchNIP65Event(fetchCtx, relay, pubkey)
	if err != nil {
		slog.Debug("failed to fetch NIP-65 event", "pubkey", pubkey, "error", err)
	}
	if nip65Event != nil {
		relays := parseRelayTags(nip65Event.Tags)
		if len(relays) > 0 {
			return relays, "nip65", nil
		}
	}

	// Nothing found - return default
	return nil, "default", nil
}

// fetchCloistrRelays fetches kind:30078 d=cloistr-relays event for a pubkey.
func (s *Server) fetchCloistrRelays(ctx context.Context, relay *nostr.Relay, pubkey string) (*nostr.Event, error) {
	// Kind 30078 is addressable, filter by d-tag
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{30078},
			Authors: []string{pubkey},
			Tags:    nostr.TagMap{"d": []string{cloistrRelaysDTag}},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, err
	}
	defer sub.Unsub()

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

// fetchNIP65Event fetches kind:10002 (NIP-65) event for a pubkey.
func (s *Server) fetchNIP65Event(ctx context.Context, relay *nostr.Relay, pubkey string) (*nostr.Event, error) {
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{10002},
			Authors: []string{pubkey},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, err
	}
	defer sub.Unsub()

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

// parseRelayTags extracts relay URLs and read/write markers from event tags.
// Works for both kind:30078 and kind:10002 (both use "r" tags with same format).
func parseRelayTags(tags nostr.Tags) []RelayPrefsEntry {
	var entries []RelayPrefsEntry

	for _, tag := range tags {
		// Both kinds use "r" tags: ["r", "wss://relay.example.com", "read"|"write"]
		// Third element is optional; if missing, relay is used for both read and write
		if len(tag) >= 2 && tag[0] == "r" {
			url := tag[1]
			if url == "" {
				continue
			}

			entry := RelayPrefsEntry{
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
