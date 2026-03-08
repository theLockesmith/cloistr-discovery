// Package api provides HTTP handlers for the discovery service.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

const (
	// reviewsFetchTimeout is the timeout for fetching reviews.
	reviewsFetchTimeout = 10 * time.Second

	// reviewsMaxToFetch limits reviews fetched per relay.
	reviewsMaxToFetch = 100

	// Kind 30078 is NIP-78 arbitrary custom app data (addressable).
	// We use d-tag "relay-review:{relay_url}" to identify relay reviews.
	kindAppData = 30078
)

// RelayReviewsResponse is the response for relay reviews queries.
type RelayReviewsResponse struct {
	RelayURL       string             `json:"relay_url"`
	Reviews        []RelayReviewEntry `json:"reviews"`
	AverageRating  float64            `json:"average_rating"`
	TotalReviews   int                `json:"total_reviews"`
	WoTAverage     float64            `json:"wot_average,omitempty"`     // Average weighted by WoT
	WoTReviewCount int                `json:"wot_review_count,omitempty"` // Reviews from followed users
	FetchedAt      time.Time          `json:"fetched_at"`
	CacheHit       bool               `json:"cache_hit"`
}

// RelayReviewEntry represents a single relay review.
type RelayReviewEntry struct {
	Pubkey    string    `json:"pubkey"`
	Rating    int       `json:"rating"` // 1-5 stars
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	InNetwork bool      `json:"in_network,omitempty"` // True if reviewer is in user's WoT
}

// RelayReviewsHandler handles GET /api/v1/relay/reviews
// Query params:
//   - url: relay URL to get reviews for (required)
//   - pubkey: user's pubkey for WoT-weighted ratings (optional)
func (s *Server) RelayReviewsHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.QueryDurationSeconds.WithLabelValues("relay_reviews").Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.QueriesTotal.WithLabelValues("relay_reviews").Inc()

	w.Header().Set("Content-Type", "application/json")

	q := r.URL.Query()

	// Get relay URL
	relayURL := q.Get("url")
	if relayURL == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "url parameter required"})
		return
	}

	// Validate URL format
	if !strings.HasPrefix(relayURL, "wss://") && !strings.HasPrefix(relayURL, "ws://") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid relay URL format"})
		return
	}

	ctx := r.Context()

	// Check for optional pubkey for WoT weighting
	var wotFollows map[string]bool
	if pubkey := q.Get("pubkey"); pubkey != "" {
		if err := validatePubkey(pubkey); err == nil {
			follows, err := s.getFollows(ctx, pubkey)
			if err == nil && len(follows) > 0 {
				wotFollows = make(map[string]bool)
				for _, f := range follows {
					wotFollows[f] = true
				}
			}
		}
	}

	// Check cache first
	cached, err := s.cache.GetRelayReviews(ctx, relayURL)
	if err != nil {
		slog.Error("failed to get cached reviews", "relay", relayURL, "error", err)
	}

	var reviews []RelayReviewEntry
	var fetchedAt time.Time
	var cacheHit bool

	if cached != nil {
		cacheHit = true
		fetchedAt = cached.FetchedAt
		reviews = make([]RelayReviewEntry, len(cached.Reviews))
		for i, r := range cached.Reviews {
			reviews[i] = RelayReviewEntry{
				Pubkey:    r.Pubkey,
				Rating:    r.Rating,
				Comment:   r.Comment,
				CreatedAt: r.CreatedAt,
			}
		}
	} else {
		// Fetch reviews from relays
		var fetchErr error
		reviews, fetchErr = s.fetchRelayReviews(ctx, relayURL)
		if fetchErr != nil {
			slog.Error("failed to fetch reviews", "relay", relayURL, "error", fetchErr)
			// Continue with empty reviews rather than failing
		}
		fetchedAt = time.Now()

		// Cache the result
		if len(reviews) > 0 {
			cacheEntry := &cache.RelayReviewsEntry{
				RelayURL:  relayURL,
				FetchedAt: fetchedAt,
				Reviews:   make([]cache.RelayReview, len(reviews)),
			}
			for i, r := range reviews {
				cacheEntry.Reviews[i] = cache.RelayReview{
					Pubkey:    r.Pubkey,
					Rating:    r.Rating,
					Comment:   r.Comment,
					CreatedAt: r.CreatedAt,
				}
			}
			// Compute averages for cache
			if len(reviews) > 0 {
				var sum int
				for _, r := range reviews {
					sum += r.Rating
				}
				cacheEntry.AverageRating = float64(sum) / float64(len(reviews))
				cacheEntry.TotalReviews = len(reviews)
			}
			if err := s.cache.SetRelayReviews(ctx, cacheEntry); err != nil {
				slog.Error("failed to cache reviews", "relay", relayURL, "error", err)
			}
		}
	}

	// Mark reviews from user's network and compute WoT-weighted average
	var wotSum, wotCount int
	for i := range reviews {
		if wotFollows != nil && wotFollows[reviews[i].Pubkey] {
			reviews[i].InNetwork = true
			wotSum += reviews[i].Rating
			wotCount++
		}
	}

	// Compute overall average
	var avgRating float64
	if len(reviews) > 0 {
		var sum int
		for _, r := range reviews {
			sum += r.Rating
		}
		avgRating = float64(sum) / float64(len(reviews))
	}

	// Compute WoT average
	var wotAverage float64
	if wotCount > 0 {
		wotAverage = float64(wotSum) / float64(wotCount)
	}

	resp := RelayReviewsResponse{
		RelayURL:       relayURL,
		Reviews:        reviews,
		AverageRating:  avgRating,
		TotalReviews:   len(reviews),
		WoTAverage:     wotAverage,
		WoTReviewCount: wotCount,
		FetchedAt:      fetchedAt,
		CacheHit:       cacheHit,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode reviews response", "error", err)
	}
}

// fetchRelayReviews fetches Kind 30078 relay review events from seed relays.
func (s *Server) fetchRelayReviews(ctx context.Context, relayURL string) ([]RelayReviewEntry, error) {
	relaysToQuery := s.cfg.SeedRelays
	if len(relaysToQuery) > nip65MaxRelaysToQuery {
		relaysToQuery = relaysToQuery[:nip65MaxRelaysToQuery]
	}

	fetchCtx, cancel := context.WithTimeout(ctx, reviewsFetchTimeout)
	defer cancel()

	var wg sync.WaitGroup
	reviewsCh := make(chan *nostr.Event, reviewsMaxToFetch)

	// d-tag value for relay reviews
	dTag := "relay-review:" + relayURL

	for _, seedRelay := range relaysToQuery {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			events, err := fetchReviewsFromRelay(fetchCtx, url, dTag)
			if err != nil {
				slog.Debug("failed to fetch reviews from relay", "relay", url, "error", err)
				return
			}
			for _, event := range events {
				select {
				case reviewsCh <- event:
				default:
					return // Channel full
				}
			}
		}(seedRelay)
	}

	go func() {
		wg.Wait()
		close(reviewsCh)
	}()

	// Collect and deduplicate reviews (by pubkey, take newest)
	reviewsByPubkey := make(map[string]*nostr.Event)
	for event := range reviewsCh {
		if existing, ok := reviewsByPubkey[event.PubKey]; !ok || event.CreatedAt > existing.CreatedAt {
			reviewsByPubkey[event.PubKey] = event
		}
	}

	// Parse reviews
	var reviews []RelayReviewEntry
	for _, event := range reviewsByPubkey {
		review := parseReviewEvent(event)
		if review != nil && review.Rating >= 1 && review.Rating <= 5 {
			reviews = append(reviews, *review)
		}
	}

	// Sort by created_at descending (newest first)
	sort.Slice(reviews, func(i, j int) bool {
		return reviews[i].CreatedAt.After(reviews[j].CreatedAt)
	})

	return reviews, nil
}

// fetchReviewsFromRelay fetches Kind 30078 events with relay-review d-tag from a single relay.
func fetchReviewsFromRelay(ctx context.Context, relayURL, dTag string) ([]*nostr.Event, error) {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer relay.Close()

	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{kindAppData},
			Tags:  nostr.TagMap{"d": []string{dTag}},
			Limit: reviewsMaxToFetch,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	defer sub.Unsub()

	var events []*nostr.Event
	timeout := time.After(5 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return events, ctx.Err()
		case event, ok := <-sub.Events:
			if !ok {
				return events, nil
			}
			if event != nil {
				events = append(events, event)
			}
		case <-timeout:
			return events, nil
		}
	}
}

// parseReviewEvent extracts review data from a Kind 30078 event.
// Expected content format (JSON):
//
//	{
//	  "rating": 4,
//	  "comment": "Great relay, fast and reliable"
//	}
func parseReviewEvent(event *nostr.Event) *RelayReviewEntry {
	if event == nil {
		return nil
	}

	var reviewData struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}

	if err := json.Unmarshal([]byte(event.Content), &reviewData); err != nil {
		// Try legacy format: just a number as rating
		// or plain text as comment with rating in tags
		return nil
	}

	return &RelayReviewEntry{
		Pubkey:    event.PubKey,
		Rating:    reviewData.Rating,
		Comment:   reviewData.Comment,
		CreatedAt: event.CreatedAt.Time(),
	}
}

// GetRelayReviewSummary returns a summary of reviews for inclusion in relay metadata.
// This is used to enrich relay entries with review data.
func (s *Server) GetRelayReviewSummary(ctx context.Context, relayURL string) (avgRating float64, totalReviews int) {
	cached, err := s.cache.GetRelayReviews(ctx, relayURL)
	if err != nil || cached == nil {
		return 0, 0
	}
	return cached.AverageRating, cached.TotalReviews
}
