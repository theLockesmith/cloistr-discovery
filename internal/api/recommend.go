// Package api provides HTTP handlers for discovery queries.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// RecommendationResponse is the response for relay recommendations.
type RecommendationResponse struct {
	Recommendations []RecommendedRelay   `json:"recommendations"`
	Total           int                  `json:"total"`
	Criteria        RecommendationInputs `json:"criteria"`
}

// RecommendedRelay represents a relay with its recommendation score.
type RecommendedRelay struct {
	Relay   cache.RelayEntry `json:"relay"`
	Score   int              `json:"score"`
	Reasons []string         `json:"reasons"`
}

// RecommendationInputs captures the criteria used for recommendations.
type RecommendationInputs struct {
	NIPs           []int  `json:"nips,omitempty"`
	Region         string `json:"region,omitempty"`
	ExcludeAuth    bool   `json:"exclude_auth,omitempty"`
	ExcludePayment bool   `json:"exclude_payment,omitempty"`
}

const (
	defaultRecommendLimit = 10
	maxRecommendLimit     = 50
	maxNIPFilters         = 20
	maxNIPValue           = 9999

	// Scoring algorithm:
	// - Health is the primary factor (online=100, degraded=50, offline=excluded)
	// - Latency provides a secondary boost (low=15, medium=10, high=5)
	// - Each matching NIP adds 10 points
	// - Region match adds 20 points
	// - Free access (no auth/payment) adds 10 points each
	// - Relays are ranked by total score descending
	scoreHealthOnline   = 100
	scoreHealthDegraded = 50
	scorePerNIPMatch    = 10
	scoreRegionMatch    = 20
	scoreNoAuth         = 10
	scoreNoPayment      = 10
	scoreLowLatency     = 15
	scoreMedLatency     = 10
	scoreHighLatency    = 5

	// Latency thresholds (milliseconds)
	latencyLowThreshold    = 100
	latencyMediumThreshold = 300
	latencyHighThreshold   = 1000
)

// RecommendRelaysHandler handles GET /api/v1/relays/recommend
// Query params:
//   - nips: comma-separated list of preferred NIPs (boost relays that support them)
//   - region: preferred country code (boost relays in that region)
//   - exclude_auth: if "true", exclude relays requiring authentication
//   - exclude_payment: if "true", exclude relays requiring payment
//   - limit: maximum number of recommendations (default 10, max 50)
func (s *Server) RecommendRelaysHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.QueryDurationSeconds.WithLabelValues("recommend_relays").Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.QueriesTotal.WithLabelValues("recommend_relays").Inc()

	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	q := r.URL.Query()

	// Parse criteria
	criteria := RecommendationInputs{}

	// Parse NIPs (with validation)
	if nipsParam := q.Get("nips"); nipsParam != "" {
		parts := strings.Split(nipsParam, ",")
		if len(parts) > maxNIPFilters {
			parts = parts[:maxNIPFilters]
		}
		for _, nipStr := range parts {
			nip, err := strconv.Atoi(strings.TrimSpace(nipStr))
			if err == nil && nip > 0 && nip <= maxNIPValue {
				criteria.NIPs = append(criteria.NIPs, nip)
			}
		}
	}

	// Parse region (validate ISO 3166-1 alpha-2 format)
	region := strings.ToUpper(strings.TrimSpace(q.Get("region")))
	if len(region) == 2 {
		criteria.Region = region
	}

	// Parse exclusions
	criteria.ExcludeAuth = q.Get("exclude_auth") == "true"
	criteria.ExcludePayment = q.Get("exclude_payment") == "true"

	// Parse limit
	limit := defaultRecommendLimit
	if limitStr := q.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > maxRecommendLimit {
				limit = maxRecommendLimit
			}
		}
	}

	// Get all relay URLs
	allURLs, err := s.cache.GetAllRelayURLs(ctx)
	if err != nil {
		slog.Error("failed to get relay URLs for recommendations", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if len(allURLs) == 0 {
		if err := json.NewEncoder(w).Encode(RecommendationResponse{
			Recommendations: []RecommendedRelay{},
			Total:           0,
			Criteria:        criteria,
		}); err != nil {
			slog.Error("failed to encode empty recommendation response", "error", err)
		}
		return
	}

	// Batch fetch all relay entries
	entries, err := s.cache.GetRelayEntriesBatch(ctx, allURLs)
	if err != nil {
		slog.Error("failed to batch get relay entries for recommendations", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Score and filter relays
	var recommendations []RecommendedRelay
	for _, entry := range entries {
		if entry == nil {
			continue
		}

		// Skip offline relays
		if entry.Health == "offline" {
			continue
		}

		// Apply exclusion filters
		if criteria.ExcludeAuth && entry.AuthRequired {
			continue
		}
		if criteria.ExcludePayment && entry.PaymentRequired {
			continue
		}

		// Calculate score
		score, reasons := scoreRelay(entry, criteria)
		recommendations = append(recommendations, RecommendedRelay{
			Relay:   *entry,
			Score:   score,
			Reasons: reasons,
		})
	}

	// Sort by score descending
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	// Apply limit
	total := len(recommendations)
	if len(recommendations) > limit {
		recommendations = recommendations[:limit]
	}

	resp := RecommendationResponse{
		Recommendations: recommendations,
		Total:           total,
		Criteria:        criteria,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode recommendation response", "error", err)
	}
}

// scoreRelay calculates a recommendation score for a relay based on criteria.
func scoreRelay(entry *cache.RelayEntry, criteria RecommendationInputs) (int, []string) {
	score := 0
	var reasons []string

	// Health score
	switch entry.Health {
	case "online":
		score += scoreHealthOnline
		reasons = append(reasons, "online")
	case "degraded":
		score += scoreHealthDegraded
		reasons = append(reasons, "degraded")
	}

	// Latency score
	if entry.LatencyMs > 0 {
		switch {
		case entry.LatencyMs < latencyLowThreshold:
			score += scoreLowLatency
			reasons = append(reasons, "low_latency")
		case entry.LatencyMs < latencyMediumThreshold:
			score += scoreMedLatency
			reasons = append(reasons, "medium_latency")
		case entry.LatencyMs < latencyHighThreshold:
			score += scoreHighLatency
			reasons = append(reasons, "acceptable_latency")
		}
	}

	// NIP support score
	if len(criteria.NIPs) > 0 {
		nipSet := make(map[int]bool)
		for _, nip := range entry.SupportedNIPs {
			nipSet[nip] = true
		}
		matchedNIPs := 0
		for _, wantedNIP := range criteria.NIPs {
			if nipSet[wantedNIP] {
				matchedNIPs++
				score += scorePerNIPMatch
			}
		}
		if matchedNIPs > 0 {
			reasons = append(reasons, "supports_requested_nips")
		}
	}

	// Region match score
	if criteria.Region != "" && entry.CountryCode == criteria.Region {
		score += scoreRegionMatch
		reasons = append(reasons, "region_match")
	}

	// Free access bonuses
	if !entry.AuthRequired {
		score += scoreNoAuth
		reasons = append(reasons, "no_auth_required")
	}
	if !entry.PaymentRequired {
		score += scoreNoPayment
		reasons = append(reasons, "free")
	}

	return score, reasons
}
