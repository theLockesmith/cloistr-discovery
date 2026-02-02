// Package admin provides the admin interface for managing the discovery service.
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
	"gitlab.com/coldforge/coldforge-discovery/internal/discovery"
	"gitlab.com/coldforge/coldforge-discovery/internal/relay"
)

// PublisherInterface is an interface for getting publisher statistics.
type PublisherInterface interface {
	GetPublicKey() string
	GetLastPublish() time.Time
	GetPublishCount() int64
	GetRelaysPublished() int64
}

// PublisherInfo contains publisher statistics for the dashboard.
type PublisherInfo struct {
	LastPublish     time.Time `json:"last_publish"`
	PublishCount    int64     `json:"publish_count"`
	RelaysPublished int64     `json:"relays_published"`
	PublicKey       string    `json:"public_key"`
}

// Server handles admin API requests.
type Server struct {
	cfg         *config.Config
	cache       *cache.Client
	monitor     *relay.Monitor
	coordinator *discovery.Coordinator
	publisher   PublisherInterface
}

// NewServer creates a new admin server.
func NewServer(cfg *config.Config, cache *cache.Client, monitor *relay.Monitor, coordinator *discovery.Coordinator) *Server {
	return &Server{
		cfg:         cfg,
		cache:       cache,
		monitor:     monitor,
		coordinator: coordinator,
	}
}

// SetPublisher sets the publisher for stats reporting.
func (s *Server) SetPublisher(p PublisherInterface) {
	s.publisher = p
}

// AuthMiddleware validates API key or basic auth credentials.
func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check API key in header or query param
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.URL.Query().Get("api_key")
		}

		if s.cfg.AdminAPIKey != "" && apiKey == s.cfg.AdminAPIKey {
			next(w, r)
			return
		}

		// Fall back to basic auth
		username, password, ok := r.BasicAuth()
		if ok && s.cfg.AdminPassword != "" &&
			username == s.cfg.AdminUsername && password == s.cfg.AdminPassword {
			next(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

// Handler routes admin requests to the appropriate handler.
func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "dashboard" || path == "":
		s.DashboardHandler(w, r)
	case path == "relays":
		s.RelaysHandler(w, r)
	case strings.HasPrefix(path, "relays/"):
		s.RelayHandler(w, r)
	case path == "whitelist":
		s.WhitelistHandler(w, r)
	case strings.HasPrefix(path, "whitelist/"):
		s.WhitelistItemHandler(w, r)
	case path == "blacklist":
		s.BlacklistHandler(w, r)
	case strings.HasPrefix(path, "blacklist/"):
		s.BlacklistItemHandler(w, r)
	case path == "peers":
		s.PeersHandler(w, r)
	case strings.HasPrefix(path, "peers/"):
		s.PeerHandler(w, r)
	default:
		http.NotFound(w, r)
	}
}

// DashboardResponse contains dashboard statistics.
type DashboardResponse struct {
	Relays    RelayStats     `json:"relays"`
	Discovery DiscoveryInfo  `json:"discovery"`
	Lists     ListCounts     `json:"lists"`
	Activity  ActivityStats  `json:"activity"`
	Publisher *PublisherInfo `json:"publisher,omitempty"`
}

// RelayStats contains relay health statistics.
type RelayStats struct {
	Total    int64 `json:"total"`
	Online   int64 `json:"online"`
	Degraded int64 `json:"degraded"`
	Offline  int64 `json:"offline"`
}

// DiscoveryInfo contains discovery source statistics.
type DiscoveryInfo struct {
	Sources   map[string]int64       `json:"sources"`
	LastRun   discovery.LastFetchTimes `json:"last_run"`
}

// ListCounts contains whitelist/blacklist/peer counts.
type ListCounts struct {
	WhitelistCount    int `json:"whitelist_count"`
	BlacklistCount    int `json:"blacklist_count"`
	TrustedPeersCount int `json:"trusted_peers_count"`
}

// ActivityStats contains activity tracking statistics.
type ActivityStats struct {
	StreamsActive  int64 `json:"streams_active"`
	PubkeysIndexed int64 `json:"pubkeys_indexed"`
}

// DashboardHandler returns dashboard statistics.
func (s *Server) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Get relay stats
	total, _ := s.cache.GetStat(ctx, "relays:total")
	online, _ := s.cache.GetStat(ctx, "relays:online")
	degraded, _ := s.cache.GetStat(ctx, "relays:degraded")
	offline, _ := s.cache.GetStat(ctx, "relays:offline")

	relayStats := RelayStats{
		Total:    total,
		Online:   online,
		Degraded: degraded,
		Offline:  offline,
	}

	// Get discovery stats
	discoveryStats, _ := s.cache.GetAllStats(ctx)
	sources := make(map[string]int64)
	for key, val := range discoveryStats {
		if strings.HasPrefix(key, "discovery:") && key != "discovery:total" {
			sources[strings.TrimPrefix(key, "discovery:")] = val
		}
	}

	var lastRun discovery.LastFetchTimes
	if s.coordinator != nil {
		lastRun = s.coordinator.GetLastFetchTimes()
	}

	// Get list counts
	whitelist, _ := s.cache.GetWhitelist(ctx)
	blacklist, _ := s.cache.GetBlacklist(ctx)
	peers, _ := s.cache.GetTrustedPeers(ctx)

	// Get activity stats
	streams, _ := s.cache.GetActiveStreams(ctx)
	pubkeysIndexed, _ := s.cache.GetStat(ctx, "pubkeys:indexed")

	resp := DashboardResponse{
		Relays: relayStats,
		Discovery: DiscoveryInfo{
			Sources: sources,
			LastRun: lastRun,
		},
		Lists: ListCounts{
			WhitelistCount:    len(whitelist),
			BlacklistCount:    len(blacklist),
			TrustedPeersCount: len(peers),
		},
		Activity: ActivityStats{
			StreamsActive:  int64(len(streams)),
			PubkeysIndexed: pubkeysIndexed,
		},
	}

	// Add publisher stats if available
	if s.publisher != nil {
		resp.Publisher = &PublisherInfo{
			LastPublish:     s.publisher.GetLastPublish(),
			PublishCount:    s.publisher.GetPublishCount(),
			RelaysPublished: s.publisher.GetRelaysPublished(),
			PublicKey:       s.publisher.GetPublicKey(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RelaysHandler handles listing and adding relays.
func (s *Server) RelaysHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		relays := s.monitor.GetRelays()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"relays": relays,
			"total":  len(relays),
		})

	case http.MethodPost:
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}

		// Submit to discovery coordinator for proper processing
		if s.coordinator != nil {
			s.coordinator.SubmitRelay(ctx, req.URL)
		} else {
			// Fallback: add directly to monitor
			s.monitor.AddRelay(req.URL)
		}

		slog.Info("admin added relay", "url", req.URL)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "url": req.URL})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// RelayHandler handles operations on a specific relay.
func (s *Server) RelayHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin/relays/")
	url := "wss://" + path // Reconstruct URL (path loses the protocol)

	switch r.Method {
	case http.MethodDelete:
		s.monitor.RemoveRelay(url)
		slog.Info("admin removed relay", "url", url)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "url": url})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// WhitelistHandler handles listing and adding to the whitelist.
func (s *Server) WhitelistHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		whitelist, err := s.cache.GetWhitelist(ctx)
		if err != nil {
			http.Error(w, "Failed to get whitelist", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"whitelist": whitelist,
			"total":     len(whitelist),
		})

	case http.MethodPost:
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}

		if err := s.cache.AddToWhitelist(ctx, req.URL); err != nil {
			http.Error(w, "Failed to add to whitelist", http.StatusInternalServerError)
			return
		}

		// Also add to monitoring
		s.monitor.AddRelay(req.URL)

		slog.Info("admin added to whitelist", "url", req.URL)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "url": req.URL})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// WhitelistItemHandler handles operations on a specific whitelist item.
func (s *Server) WhitelistItemHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/admin/whitelist/")
	url := "wss://" + path

	switch r.Method {
	case http.MethodDelete:
		if err := s.cache.RemoveFromWhitelist(ctx, url); err != nil {
			http.Error(w, "Failed to remove from whitelist", http.StatusInternalServerError)
			return
		}
		slog.Info("admin removed from whitelist", "url", url)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "url": url})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// BlacklistHandler handles listing and adding to the blacklist.
func (s *Server) BlacklistHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		blacklist, err := s.cache.GetBlacklist(ctx)
		if err != nil {
			http.Error(w, "Failed to get blacklist", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"blacklist": blacklist,
			"total":     len(blacklist),
		})

	case http.MethodPost:
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}

		if err := s.cache.AddToBlacklist(ctx, req.URL); err != nil {
			http.Error(w, "Failed to add to blacklist", http.StatusInternalServerError)
			return
		}

		// Also remove from monitoring
		s.monitor.RemoveRelay(req.URL)

		slog.Info("admin added to blacklist", "url", req.URL)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "url": req.URL})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// BlacklistItemHandler handles operations on a specific blacklist item.
func (s *Server) BlacklistItemHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/admin/blacklist/")
	url := "wss://" + path

	switch r.Method {
	case http.MethodDelete:
		if err := s.cache.RemoveFromBlacklist(ctx, url); err != nil {
			http.Error(w, "Failed to remove from blacklist", http.StatusInternalServerError)
			return
		}
		slog.Info("admin removed from blacklist", "url", url)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "url": url})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// PeersHandler handles listing and adding trusted peers.
func (s *Server) PeersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		peers, err := s.cache.GetTrustedPeers(ctx)
		if err != nil {
			http.Error(w, "Failed to get trusted peers", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"peers": peers,
			"total": len(peers),
		})

	case http.MethodPost:
		var req struct {
			Pubkey string `json:"pubkey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Pubkey == "" {
			http.Error(w, "Pubkey is required", http.StatusBadRequest)
			return
		}

		if err := s.cache.AddTrustedPeer(ctx, req.Pubkey); err != nil {
			http.Error(w, "Failed to add trusted peer", http.StatusInternalServerError)
			return
		}

		slog.Info("admin added trusted peer", "pubkey", req.Pubkey[:16]+"...")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "pubkey": req.Pubkey})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// PeerHandler handles operations on a specific peer.
func (s *Server) PeerHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pubkey := strings.TrimPrefix(r.URL.Path, "/admin/peers/")

	switch r.Method {
	case http.MethodDelete:
		if err := s.cache.RemoveTrustedPeer(ctx, pubkey); err != nil {
			http.Error(w, "Failed to remove trusted peer", http.StatusInternalServerError)
			return
		}
		slog.Info("admin removed trusted peer", "pubkey", pubkey[:16]+"...")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "pubkey": pubkey})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
