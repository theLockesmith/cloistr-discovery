// Package api provides HTTP and WebSocket handlers for discovery queries.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// Server handles API requests.
type Server struct {
	cfg   *config.Config
	cache *cache.Client
}

// New creates a new API server.
func New(cfg *config.Config, cache *cache.Client) *Server {
	return &Server{
		cfg:   cfg,
		cache: cache,
	}
}

// MetricsHandler serves Prometheus metrics.
func (s *Server) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}

// RelaysResponse is the response for relay queries.
type RelaysResponse struct {
	Relays []cache.RelayEntry `json:"relays"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit,omitempty"`
	Offset int                `json:"offset,omitempty"`
}

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// RelaysHandler handles GET /api/v1/relays
// Query params:
//   - health: filter by health status (online, degraded, offline)
//   - nips: filter by supported NIPs (comma-separated, AND logic)
//   - location: filter by country code
//   - topic: filter by topic (OR logic if multiple)
//   - atmosphere: filter by atmosphere (OR logic if multiple)
//   - content_policy: filter by content policy (anything, sfw, nsfw-allowed, nsfw-only)
//   - moderation: filter by minimum moderation level (unmoderated, light, active, strict)
//   - language: filter by language (ISO 639-1 code)
//   - community: filter by community name
//   - limit: maximum number of results (default 100, max 1000)
//   - offset: number of results to skip for pagination
func (s *Server) RelaysHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.QueryDurationSeconds.WithLabelValues("relays").Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.QueriesTotal.WithLabelValues("relays").Inc()

	ctx := r.Context()
	q := r.URL.Query()

	var relayURLs []string
	hasFilters := false

	// Filter by NIP if specified (AND logic - must support all)
	if nipsParam := q.Get("nips"); nipsParam != "" {
		hasFilters = true
		nips := strings.Split(nipsParam, ",")
		for _, nipStr := range nips {
			nip, err := strconv.Atoi(strings.TrimSpace(nipStr))
			if err != nil {
				continue
			}
			urls, err := s.cache.GetRelaysByNIP(ctx, nip)
			if err != nil {
				slog.Error("failed to get relays by NIP", "nip", nip, "error", err)
				continue
			}
			if len(relayURLs) == 0 {
				relayURLs = urls
			} else {
				relayURLs = intersect(relayURLs, urls)
			}
		}
	}

	// Filter by location if specified
	if loc := q.Get("location"); loc != "" {
		hasFilters = true
		urls, err := s.cache.GetRelaysByLocation(ctx, loc)
		if err != nil {
			slog.Error("failed to get relays by location", "location", loc, "error", err)
		} else {
			if len(relayURLs) == 0 {
				relayURLs = urls
			} else {
				relayURLs = intersect(relayURLs, urls)
			}
		}
	}

	// Filter by topic if specified (OR logic - match any)
	if topics, ok := q["topic"]; ok && len(topics) > 0 {
		hasFilters = true
		var topicURLs []string
		for _, topic := range topics {
			urls, err := s.cache.GetRelaysByTopic(ctx, topic)
			if err != nil {
				slog.Error("failed to get relays by topic", "topic", topic, "error", err)
				continue
			}
			topicURLs = union(topicURLs, urls)
		}
		if len(relayURLs) == 0 {
			relayURLs = topicURLs
		} else {
			relayURLs = intersect(relayURLs, topicURLs)
		}
	}

	// Filter by atmosphere if specified (OR logic - match any)
	if atmospheres, ok := q["atmosphere"]; ok && len(atmospheres) > 0 {
		hasFilters = true
		var atmURLs []string
		for _, atm := range atmospheres {
			urls, err := s.cache.GetRelaysByAtmosphere(ctx, atm)
			if err != nil {
				slog.Error("failed to get relays by atmosphere", "atmosphere", atm, "error", err)
				continue
			}
			atmURLs = union(atmURLs, urls)
		}
		if len(relayURLs) == 0 {
			relayURLs = atmURLs
		} else {
			relayURLs = intersect(relayURLs, atmURLs)
		}
	}

	// Filter by content policy if specified
	if cp := q.Get("content_policy"); cp != "" {
		hasFilters = true
		urls, err := s.cache.GetRelaysByContentPolicy(ctx, cp)
		if err != nil {
			slog.Error("failed to get relays by content policy", "content_policy", cp, "error", err)
		} else {
			if len(relayURLs) == 0 {
				relayURLs = urls
			} else {
				relayURLs = intersect(relayURLs, urls)
			}
		}
	}

	// Filter by moderation level if specified (minimum level matching)
	if mod := q.Get("moderation"); mod != "" {
		hasFilters = true
		modLevels := moderationLevelsAtOrAbove(mod)
		var modURLs []string
		for _, level := range modLevels {
			urls, err := s.cache.GetRelaysByModeration(ctx, level)
			if err != nil {
				slog.Error("failed to get relays by moderation", "moderation", level, "error", err)
				continue
			}
			modURLs = union(modURLs, urls)
		}
		if len(relayURLs) == 0 {
			relayURLs = modURLs
		} else {
			relayURLs = intersect(relayURLs, modURLs)
		}
	}

	// Filter by language if specified
	if lang := q.Get("language"); lang != "" {
		hasFilters = true
		urls, err := s.cache.GetRelaysByLanguage(ctx, lang)
		if err != nil {
			slog.Error("failed to get relays by language", "language", lang, "error", err)
		} else {
			if len(relayURLs) == 0 {
				relayURLs = urls
			} else {
				relayURLs = intersect(relayURLs, urls)
			}
		}
	}

	// Filter by community if specified
	if community := q.Get("community"); community != "" {
		hasFilters = true
		urls, err := s.cache.GetRelaysByCommunity(ctx, community)
		if err != nil {
			slog.Error("failed to get relays by community", "community", community, "error", err)
		} else {
			if len(relayURLs) == 0 {
				relayURLs = urls
			} else {
				relayURLs = intersect(relayURLs, urls)
			}
		}
	}

	// If no filters specified, return all known relays
	if !hasFilters {
		urls, err := s.cache.GetAllRelayURLs(ctx)
		if err != nil {
			slog.Error("failed to get all relay URLs", "error", err)
		} else {
			relayURLs = urls
		}
	}

	// Parse pagination parameters
	limit := defaultLimit
	if limitStr := q.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > maxLimit {
				limit = maxLimit
			}
		}
	}

	offset := 0
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get relay entries using batch retrieval
	healthFilter := q.Get("health")

	entries, err := s.cache.GetRelayEntriesBatch(ctx, relayURLs)
	if err != nil {
		slog.Error("failed to batch get relay entries", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Filter by health and collect valid entries
	var relays []cache.RelayEntry
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if healthFilter != "" && entry.Health != healthFilter {
			continue
		}
		relays = append(relays, *entry)
	}

	// Apply pagination
	total := len(relays)
	if offset >= total {
		relays = nil
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		relays = relays[offset:end]
	}

	resp := RelaysResponse{
		Relays: relays,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}


// intersect returns the intersection of two string slices.
func intersect(a, b []string) []string {
	m := make(map[string]bool)
	for _, v := range a {
		m[v] = true
	}
	var result []string
	for _, v := range b {
		if m[v] {
			result = append(result, v)
		}
	}
	return result
}

// union returns the union of two string slices (deduplicated).
func union(a, b []string) []string {
	m := make(map[string]bool)
	for _, v := range a {
		m[v] = true
	}
	for _, v := range b {
		m[v] = true
	}
	result := make([]string, 0, len(m))
	for v := range m {
		result = append(result, v)
	}
	return result
}

// moderationLevelsAtOrAbove returns all moderation levels at or above the given level.
// Ordering: unmoderated < light < active < strict
func moderationLevelsAtOrAbove(level string) []string {
	levels := []string{"unmoderated", "light", "active", "strict"}
	var result []string
	found := false
	for _, l := range levels {
		if l == level {
			found = true
		}
		if found {
			result = append(result, l)
		}
	}
	if !found {
		return []string{level}
	}
	return result
}
