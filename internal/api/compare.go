// Package api provides HTTP handlers for discovery queries.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

const (
	minCompareRelays = 2
	maxCompareRelays = 10
)

// CompareResponse is the response for relay comparison.
type CompareResponse struct {
	Relays     []CompareRelay `json:"relays"`
	Comparison ComparisonData `json:"comparison"`
	Error      string         `json:"error,omitempty"`
}

// CompareRelay represents a relay in the comparison with its full data.
type CompareRelay struct {
	URL      string             `json:"url"`
	Found    bool               `json:"found"`
	Relay    *cache.RelayEntry  `json:"relay,omitempty"`
	Features *RelayFeatureSummary `json:"features,omitempty"`
}

// RelayFeatureSummary provides a quick feature overview for comparison.
type RelayFeatureSummary struct {
	NIPCount        int      `json:"nip_count"`
	HasNIP42Auth    bool     `json:"has_nip42_auth"`
	HasNIP65Lists   bool     `json:"has_nip65_lists"`
	HasNIP96Media   bool     `json:"has_nip96_media"`
	HasNIP50Search  bool     `json:"has_nip50_search"`
	LatencyCategory string   `json:"latency_category"` // fast, medium, slow, unknown
	AccessType      string   `json:"access_type"`      // free, auth-required, paid, paid+auth
}

// ComparisonData provides aggregate comparison insights.
type ComparisonData struct {
	CommonNIPs     []int             `json:"common_nips"`
	FastestRelay   string            `json:"fastest_relay,omitempty"`
	HealthySummary map[string]int    `json:"healthy_summary"` // online: 2, degraded: 1, etc.
	NIPCoverage    map[int][]string  `json:"nip_coverage"`    // NIP -> list of relay URLs that support it
}

// CompareRelaysHandler handles GET /api/v1/relays/compare
// Query params:
//   - urls: comma-separated list of relay URLs to compare (2-10 relays)
//
// Example: /api/v1/relays/compare?urls=wss://relay1.com,wss://relay2.com
func (s *Server) CompareRelaysHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		metrics.QueryDurationSeconds.WithLabelValues("compare_relays").Observe(time.Since(start).Seconds())
	}()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics.QueriesTotal.WithLabelValues("compare_relays").Inc()

	w.Header().Set("Content-Type", "application/json")

	// Parse URLs
	urlsParam := r.URL.Query().Get("urls")
	if urlsParam == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CompareResponse{Error: "urls parameter required"})
		return
	}

	urls := parseRelayURLs(urlsParam)
	if len(urls) < minCompareRelays {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CompareResponse{Error: "at least 2 relay URLs required"})
		return
	}
	if len(urls) > maxCompareRelays {
		urls = urls[:maxCompareRelays]
	}

	ctx := r.Context()

	// Batch fetch relay entries
	entries, err := s.cache.GetRelayEntriesBatch(ctx, urls)
	if err != nil {
		slog.Error("failed to batch get relay entries for comparison", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(CompareResponse{Error: "internal server error"})
		return
	}

	// Build comparison response
	relays := make([]CompareRelay, len(urls))
	var foundEntries []*cache.RelayEntry

	for i, url := range urls {
		relays[i] = CompareRelay{
			URL:   url,
			Found: entries[i] != nil,
		}
		if entries[i] != nil {
			relays[i].Relay = entries[i]
			relays[i].Features = extractFeatures(entries[i])
			foundEntries = append(foundEntries, entries[i])
		}
	}

	// Build comparison data
	comparison := buildComparisonData(foundEntries)

	resp := CompareResponse{
		Relays:     relays,
		Comparison: comparison,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode comparison response", "error", err)
	}
}

// parseRelayURLs parses and validates relay URLs from comma-separated string.
func parseRelayURLs(param string) []string {
	parts := strings.Split(param, ",")
	urls := make([]string, 0, len(parts))
	seen := make(map[string]bool)

	for _, part := range parts {
		url := strings.TrimSpace(part)
		// Validate URL format
		if !strings.HasPrefix(url, "wss://") && !strings.HasPrefix(url, "ws://") {
			continue
		}
		// Deduplicate
		if seen[url] {
			continue
		}
		seen[url] = true
		urls = append(urls, url)
	}

	return urls
}

// extractFeatures creates a feature summary for quick comparison.
func extractFeatures(entry *cache.RelayEntry) *RelayFeatureSummary {
	features := &RelayFeatureSummary{
		NIPCount: len(entry.SupportedNIPs),
	}

	// Check for specific NIPs
	for _, nip := range entry.SupportedNIPs {
		switch nip {
		case 42:
			features.HasNIP42Auth = true
		case 65:
			features.HasNIP65Lists = true
		case 96:
			features.HasNIP96Media = true
		case 50:
			features.HasNIP50Search = true
		}
	}

	// Latency category
	switch {
	case entry.LatencyMs == 0:
		features.LatencyCategory = "unknown"
	case entry.LatencyMs < 100:
		features.LatencyCategory = "fast"
	case entry.LatencyMs < 500:
		features.LatencyCategory = "medium"
	default:
		features.LatencyCategory = "slow"
	}

	// Access type
	switch {
	case entry.PaymentRequired && entry.AuthRequired:
		features.AccessType = "paid+auth"
	case entry.PaymentRequired:
		features.AccessType = "paid"
	case entry.AuthRequired:
		features.AccessType = "auth-required"
	default:
		features.AccessType = "free"
	}

	return features
}

// buildComparisonData generates aggregate comparison insights.
func buildComparisonData(entries []*cache.RelayEntry) ComparisonData {
	data := ComparisonData{
		HealthySummary: make(map[string]int),
		NIPCoverage:    make(map[int][]string),
	}

	if len(entries) == 0 {
		return data
	}

	// Track NIPs across all relays
	nipCount := make(map[int]int)
	var fastestLatency int = -1
	var fastestURL string

	for _, entry := range entries {
		// Health summary
		data.HealthySummary[entry.Health]++

		// Track fastest relay
		if entry.LatencyMs > 0 && (fastestLatency == -1 || entry.LatencyMs < fastestLatency) {
			fastestLatency = entry.LatencyMs
			fastestURL = entry.URL
		}

		// NIP coverage
		for _, nip := range entry.SupportedNIPs {
			nipCount[nip]++
			data.NIPCoverage[nip] = append(data.NIPCoverage[nip], entry.URL)
		}
	}

	data.FastestRelay = fastestURL

	// Find common NIPs (supported by ALL relays)
	for nip, count := range nipCount {
		if count == len(entries) {
			data.CommonNIPs = append(data.CommonNIPs, nip)
		}
	}

	return data
}
