// Package api provides HTTP handlers for the discovery service.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
)

const (
	// wotFetchTimeout is the timeout for fetching WoT data.
	wotFetchTimeout = 15 * time.Second

	// wotMaxFollowsToProcess limits follows processed to avoid excessive fetches.
	wotMaxFollowsToProcess = 100

	// wotMaxConcurrentFetches limits parallel relay connections.
	wotMaxConcurrentFetches = 10
)

// WoTScores contains the computed Web of Trust relay scores for a user.
type WoTScores struct {
	RelayScores  map[string]int // relay URL -> number of follows using it
	FollowsCount int            // Total follows analyzed
}

// GetWoTRelayScores computes relay scores based on user's follows' relay lists.
// Uses cached data when available, fetches and computes when needed.
func (s *Server) GetWoTRelayScores(ctx context.Context, pubkey string) (*WoTScores, error) {
	// Check cache first
	cached, err := s.cache.GetWoTRelayScores(ctx, pubkey)
	if err != nil {
		slog.Error("failed to get cached WoT scores", "pubkey", pubkey, "error", err)
	}
	if cached != nil {
		return &WoTScores{
			RelayScores:  cached.RelayScores,
			FollowsCount: cached.FollowsCount,
		}, nil
	}

	// Cache miss - compute WoT scores
	scores, err := s.computeWoTScores(ctx, pubkey)
	if err != nil {
		return nil, err
	}

	// Cache the result
	cacheEntry := &cache.WoTRelayScoresEntry{
		Pubkey:       pubkey,
		RelayScores:  scores.RelayScores,
		FollowsCount: scores.FollowsCount,
		ComputedAt:   time.Now(),
	}
	if err := s.cache.SetWoTRelayScores(ctx, cacheEntry); err != nil {
		slog.Error("failed to cache WoT scores", "pubkey", pubkey, "error", err)
	}

	return scores, nil
}

// computeWoTScores fetches follows and their relay lists to compute relay scores.
func (s *Server) computeWoTScores(ctx context.Context, pubkey string) (*WoTScores, error) {
	// Step 1: Get user's follows (NIP-02 contact list)
	follows, err := s.getFollows(ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to get follows: %w", err)
	}

	if len(follows) == 0 {
		return &WoTScores{
			RelayScores:  make(map[string]int),
			FollowsCount: 0,
		}, nil
	}

	// Limit follows to process
	if len(follows) > wotMaxFollowsToProcess {
		follows = follows[:wotMaxFollowsToProcess]
	}

	// Step 2: Fetch NIP-65 relay lists for each follow
	relayScores := make(map[string]int)
	var mu sync.Mutex

	// Use semaphore for concurrency control
	sem := make(chan struct{}, wotMaxConcurrentFetches)
	var wg sync.WaitGroup

	fetchCtx, cancel := context.WithTimeout(ctx, wotFetchTimeout)
	defer cancel()

	for _, followPubkey := range follows {
		wg.Add(1)
		go func(pk string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-fetchCtx.Done():
				return
			}

			// Get relay list for this follow
			relays, err := s.getFollowRelays(fetchCtx, pk)
			if err != nil {
				slog.Debug("failed to get relays for follow", "pubkey", pk, "error", err)
				return
			}

			// Count relay usage
			mu.Lock()
			for _, relayURL := range relays {
				relayScores[relayURL]++
			}
			mu.Unlock()
		}(followPubkey)
	}

	wg.Wait()

	return &WoTScores{
		RelayScores:  relayScores,
		FollowsCount: len(follows),
	}, nil
}

// getFollows retrieves a user's NIP-02 contact list (follows).
func (s *Server) getFollows(ctx context.Context, pubkey string) ([]string, error) {
	// Check cache first
	cached, err := s.cache.GetUserContacts(ctx, pubkey)
	if err != nil {
		slog.Error("failed to get cached contacts", "pubkey", pubkey, "error", err)
	}
	if cached != nil {
		return cached.Follows, nil
	}

	// Fetch from relays
	follows, err := s.fetchContactList(ctx, pubkey)
	if err != nil {
		return nil, err
	}

	// Cache the result
	cacheEntry := &cache.UserContactsEntry{
		Pubkey:    pubkey,
		Follows:   follows,
		FetchedAt: time.Now(),
	}
	if err := s.cache.SetUserContacts(ctx, cacheEntry); err != nil {
		slog.Error("failed to cache contacts", "pubkey", pubkey, "error", err)
	}

	return follows, nil
}

// fetchContactList fetches NIP-02 (Kind 3) contact list from relays.
func (s *Server) fetchContactList(ctx context.Context, pubkey string) ([]string, error) {
	relaysToQuery := s.cfg.SeedRelays
	if len(relaysToQuery) > nip65MaxRelaysToQuery {
		relaysToQuery = relaysToQuery[:nip65MaxRelaysToQuery]
	}

	fetchCtx, cancel := context.WithTimeout(ctx, nip65FetchTimeout)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan *nostr.Event, len(relaysToQuery))

	for _, relayURL := range relaysToQuery {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			event, err := fetchContactsFromRelay(fetchCtx, url, pubkey)
			if err != nil {
				slog.Debug("failed to fetch contacts from relay", "relay", url, "pubkey", pubkey, "error", err)
				return
			}
			if event != nil {
				results <- event
			}
		}(relayURL)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Take newest event
	var newestEvent *nostr.Event
	for event := range results {
		if newestEvent == nil || event.CreatedAt > newestEvent.CreatedAt {
			newestEvent = event
		}
	}

	if newestEvent == nil {
		return nil, nil // No contacts found is not an error
	}

	return parseContactTags(newestEvent.Tags), nil
}

// fetchContactsFromRelay fetches Kind 3 contact list from a single relay.
func fetchContactsFromRelay(ctx context.Context, relayURL, pubkey string) (*nostr.Event, error) {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer relay.Close()

	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{3}, // NIP-02 contact list
			Authors: []string{pubkey},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
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

// parseContactTags extracts followed pubkeys from NIP-02 event tags.
func parseContactTags(tags nostr.Tags) []string {
	var follows []string
	seen := make(map[string]bool)

	for _, tag := range tags {
		// NIP-02 uses "p" tags: ["p", "pubkey", "relay_url", "petname"]
		if len(tag) >= 2 && tag[0] == "p" {
			pk := tag[1]
			if pk == "" || len(pk) != 64 {
				continue
			}
			if seen[pk] {
				continue
			}
			seen[pk] = true
			follows = append(follows, pk)
		}
	}

	return follows
}

// getFollowRelays gets the relay list for a followed user.
// Uses cached NIP-65 data or fetches if needed.
func (s *Server) getFollowRelays(ctx context.Context, pubkey string) ([]string, error) {
	// Check NIP-65 cache first
	cached, err := s.cache.GetUserNIP65(ctx, pubkey)
	if err != nil {
		slog.Debug("failed to get cached NIP-65 for follow", "pubkey", pubkey, "error", err)
	}
	if cached != nil {
		relays := make([]string, 0, len(cached.Relays))
		for _, r := range cached.Relays {
			if r.Write { // Count write relays as "where they post"
				relays = append(relays, r.URL)
			}
		}
		return relays, nil
	}

	// Fetch NIP-65 for this follow
	entries, _, err := s.fetchUserNIP65(ctx, pubkey)
	if err != nil {
		return nil, err
	}

	// Cache it
	if len(entries) > 0 {
		cacheEntry := &cache.UserNIP65Entry{
			Pubkey:    pubkey,
			FetchedAt: time.Now(),
			Relays:    make([]cache.UserRelayData, len(entries)),
		}
		for i, e := range entries {
			cacheEntry.Relays[i] = cache.UserRelayData{
				URL:   e.URL,
				Read:  e.Read,
				Write: e.Write,
			}
		}
		if err := s.cache.SetUserNIP65(ctx, cacheEntry); err != nil {
			slog.Debug("failed to cache NIP-65 for follow", "pubkey", pubkey, "error", err)
		}
	}

	// Return write relays
	relays := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Write {
			relays = append(relays, e.URL)
		}
	}
	return relays, nil
}
