// Package api provides HTTP handlers for discovery queries.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// NIP46ScoreResponse is the response for NIP-46 suitability scoring.
type NIP46ScoreResponse struct {
	URL            string   `json:"url"`
	Score          int      `json:"score"`           // 0-100
	Recommendation string   `json:"recommendation"`  // recommended, acceptable, warning, avoid
	Reasons        []string `json:"reasons"`         // Explanation of score factors
	Error          string   `json:"error,omitempty"` // Error message if lookup failed
}

// NIP46 scoring thresholds
const (
	nip46ScoreRecommended = 80
	nip46ScoreAcceptable  = 50
	nip46ScoreWarning     = 20

	// Latency thresholds (milliseconds)
	nip46LatencyGood = 500
	nip46LatencyPoor = 1000
)

// NIP46ScoreHandler handles GET /api/v1/relay/nip46-score?url=
// Returns a suitability score for using a relay with NIP-46 remote signing.
func (s *Server) NIP46ScoreHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.QueryDurationSeconds.WithLabelValues("nip46_score").Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.QueriesTotal.WithLabelValues("nip46_score").Inc()

	relayURL := r.URL.Query().Get("url")
	if relayURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NIP46ScoreResponse{Error: "relay URL required"})
		return
	}

	// Validate URL format
	if !strings.HasPrefix(relayURL, "wss://") && !strings.HasPrefix(relayURL, "ws://") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NIP46ScoreResponse{
			URL:   relayURL,
			Error: "invalid relay URL: must start with wss:// or ws://",
		})
		return
	}

	ctx := r.Context()
	entry, err := s.cache.GetRelayEntry(ctx, relayURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(NIP46ScoreResponse{
			URL:   relayURL,
			Error: "internal server error",
		})
		return
	}

	if entry == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(NIP46ScoreResponse{
			URL:            relayURL,
			Score:          0,
			Recommendation: "unknown",
			Reasons:        []string{"relay not found in discovery database"},
		})
		return
	}

	// Calculate NIP-46 suitability score
	score, reasons := calculateNIP46Score(entry)

	resp := NIP46ScoreResponse{
		URL:            relayURL,
		Score:          score,
		Recommendation: scoreToRecommendation(score),
		Reasons:        reasons,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// calculateNIP46Score computes a 0-100 score for NIP-46 suitability.
// Returns the score and a list of reasons explaining the scoring factors.
func calculateNIP46Score(entry *cache.RelayEntry) (int, []string) {
	var reasons []string

	// Check for NIP-46 support (required)
	hasNIP46 := false
	for _, nip := range entry.SupportedNIPs {
		if nip == 46 {
			hasNIP46 = true
			break
		}
	}

	if !hasNIP46 {
		return 0, []string{"relay does not advertise NIP-46 support"}
	}
	reasons = append(reasons, "relay advertises NIP-46 support")

	// Check health status (required to be online)
	if entry.Health == "offline" {
		return 0, []string{"relay is offline"}
	}

	// Base score of 100 if NIP-46 supported and not offline
	score := 100

	// Health deductions
	if entry.Health == "degraded" {
		score -= 20
		reasons = append(reasons, "relay health is degraded (-20)")
	} else if entry.Health == "online" {
		reasons = append(reasons, "relay is online")
	}

	// Latency deductions
	if entry.LatencyMs > nip46LatencyPoor {
		score -= 30
		reasons = append(reasons, "high latency >1000ms (-30)")
	} else if entry.LatencyMs > nip46LatencyGood {
		score -= 10
		reasons = append(reasons, "moderate latency >500ms (-10)")
	} else if entry.LatencyMs > 0 {
		reasons = append(reasons, "good latency")
	}

	// Payment required deduction
	if entry.PaymentRequired {
		score -= 15
		reasons = append(reasons, "payment required may block anonymous clients (-15)")
	}

	// Auth required deduction (minor - NIP-42 is standard)
	if entry.AuthRequired {
		score -= 5
		reasons = append(reasons, "NIP-42 auth required adds connection complexity (-5)")
	}

	// Ensure score doesn't go negative
	if score < 0 {
		score = 0
	}

	return score, reasons
}

// scoreToRecommendation converts a numeric score to a recommendation string.
func scoreToRecommendation(score int) string {
	switch {
	case score >= nip46ScoreRecommended:
		return "recommended"
	case score >= nip46ScoreAcceptable:
		return "acceptable"
	case score >= nip46ScoreWarning:
		return "warning"
	default:
		return "avoid"
	}
}

