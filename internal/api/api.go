// Package api provides HTTP and WebSocket handlers for discovery queries.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// Prometheus metrics
var (
	queriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "discovery_queries_total",
			Help: "Total number of discovery queries by type",
		},
		[]string{"type"},
	)
	relaysMonitored = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "discovery_relays_monitored",
			Help: "Number of relays being monitored",
		},
	)
	relaysOnline = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "discovery_relays_online",
			Help: "Number of relays currently online",
		},
	)
	pubkeysIndexed = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "discovery_pubkeys_indexed",
			Help: "Number of unique pubkeys in routing index",
		},
	)
	activitiesTracked = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "discovery_activities_tracked",
			Help: "Number of active activity announcements",
		},
	)
)

func init() {
	prometheus.MustRegister(queriesTotal)
	prometheus.MustRegister(relaysMonitored)
	prometheus.MustRegister(relaysOnline)
	prometheus.MustRegister(pubkeysIndexed)
	prometheus.MustRegister(activitiesTracked)
}

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
}

// RelaysHandler handles GET /api/v1/relays
// Query params:
//   - health: filter by health status (online, degraded, offline)
//   - nips: filter by supported NIPs (comma-separated)
//   - location: filter by country code
func (s *Server) RelaysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queriesTotal.WithLabelValues("relays").Inc()

	ctx := r.Context()
	q := r.URL.Query()

	var relayURLs []string
	hasFilters := false

	// Filter by NIP if specified
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
				// Intersection
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

	// If no filters specified, return all known relays
	if !hasFilters {
		urls, err := s.cache.GetAllRelayURLs(ctx)
		if err != nil {
			slog.Error("failed to get all relay URLs", "error", err)
		} else {
			relayURLs = urls
		}
	}

	// Get relay entries
	var relays []cache.RelayEntry
	healthFilter := q.Get("health")

	for _, url := range relayURLs {
		entry, err := s.cache.GetRelayEntry(ctx, url)
		if err != nil {
			slog.Error("failed to get relay entry", "url", url, "error", err)
			continue
		}
		if entry == nil {
			continue
		}
		if healthFilter != "" && entry.Health != healthFilter {
			continue
		}
		relays = append(relays, *entry)
	}

	resp := RelaysResponse{
		Relays: relays,
		Total:  len(relays),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// PubkeyResponse is the response for pubkey queries.
type PubkeyResponse struct {
	Pubkey string   `json:"pubkey"`
	Relays []string `json:"relays"`
}

// PubkeyHandler handles GET /api/v1/pubkey/{pubkey}/relays
func (s *Server) PubkeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queriesTotal.WithLabelValues("pubkey").Inc()

	// Extract pubkey from path: /api/v1/pubkey/{pubkey}/relays
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pubkey/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "pubkey required", http.StatusBadRequest)
		return
	}
	pubkey := parts[0]

	// Validate pubkey format (64 hex chars)
	if len(pubkey) != 64 {
		http.Error(w, "invalid pubkey format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	relays, err := s.cache.GetPubkeyRelays(ctx, pubkey)
	if err != nil {
		slog.Error("failed to get pubkey relays", "pubkey", pubkey, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := PubkeyResponse{
		Pubkey: pubkey,
		Relays: relays,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ActivityResponse is the response for activity queries.
type ActivityResponse struct {
	Activities []cache.Activity `json:"activities"`
	Total      int              `json:"total"`
}

// ActivityHandler handles GET /api/v1/activity/{type}
// Example: /api/v1/activity/streams
func (s *Server) ActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queriesTotal.WithLabelValues("activity").Inc()

	// Extract activity type from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/activity/")
	activityType := strings.TrimSuffix(path, "/")

	ctx := r.Context()

	var activities []cache.Activity

	switch activityType {
	case "streams":
		pubkeys, err := s.cache.GetActiveStreams(ctx)
		if err != nil {
			slog.Error("failed to get active streams", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		for _, pk := range pubkeys {
			activity, err := s.cache.GetActivity(ctx, pk)
			if err != nil || activity == nil {
				continue
			}
			activities = append(activities, *activity)
		}
	default:
		http.Error(w, "unknown activity type", http.StatusBadRequest)
		return
	}

	resp := ActivityResponse{
		Activities: activities,
		Total:      len(activities),
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
