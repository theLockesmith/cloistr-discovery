package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
	"gitlab.com/coldforge/coldforge-discovery/internal/discovery"
	"gitlab.com/coldforge/coldforge-discovery/internal/relay"
)

// mockPublisher is a mock publisher for testing.
type mockPublisher struct {
	publicKey       string
	lastPublish     time.Time
	publishCount    int64
	relaysPublished int64
}

func (m *mockPublisher) GetPublicKey() string {
	return m.publicKey
}

func (m *mockPublisher) GetLastPublish() time.Time {
	return m.lastPublish
}

func (m *mockPublisher) GetPublishCount() int64 {
	return m.publishCount
}

func (m *mockPublisher) GetRelaysPublished() int64 {
	return m.relaysPublished
}

// setupTestServer creates a test admin server with miniredis and real dependencies.
func setupTestServer(t *testing.T) (*Server, *cache.Client, *miniredis.Miniredis, *relay.Monitor, *discovery.Coordinator) {
	t.Helper()

	mr := miniredis.RunT(t)

	cacheClient, err := cache.New("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to create cache client: %v", err)
	}

	cfg := &config.Config{
		AdminAPIKey:      "test-api-key",
		AdminUsername:    "admin",
		AdminPassword:    "password123",
		AdminEnabled:     true,
		LogLevel:         "info",
		SeedRelays:       []string{"wss://relay1.example.com"},
		RelayCheckInterval: 300,
		NIP11Timeout:     10,
	}

	monitor := relay.NewMonitor(cfg, cacheClient)
	// Add some test relays
	monitor.AddRelay("wss://relay1.example.com")
	monitor.AddRelay("wss://relay2.example.com")

	output := make(chan string, 10)
	coordinator := discovery.NewCoordinator(cfg, cacheClient, output)

	server := NewServer(cfg, cacheClient, monitor, coordinator)

	return server, cacheClient, mr, monitor, coordinator
}

func TestNewServer(t *testing.T) {
	server, cacheClient, mr, monitor, coordinator := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	if server == nil {
		t.Fatal("NewServer() returned nil")
	}

	if server.cfg == nil {
		t.Error("NewServer() did not set config")
	}

	if server.cache != cacheClient {
		t.Error("NewServer() did not set cache properly")
	}

	if server.monitor != monitor {
		t.Error("NewServer() did not set monitor properly")
	}

	if server.coordinator != coordinator {
		t.Error("NewServer() did not set coordinator properly")
	}
}

func TestSetPublisher(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	pub := &mockPublisher{
		publicKey:       "testpubkey123",
		lastPublish:     time.Now(),
		publishCount:    5,
		relaysPublished: 100,
	}

	server.SetPublisher(pub)

	if server.publisher != pub {
		t.Error("SetPublisher() did not set publisher")
	}
}

func TestAuthMiddleware(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	tests := []struct {
		name           string
		apiKey         string
		apiKeyInQuery  bool
		basicAuthUser  string
		basicAuthPass  string
		wantStatusCode int
	}{
		{
			name:           "valid API key in header",
			apiKey:         "test-api-key",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "valid API key in query param",
			apiKey:         "test-api-key",
			apiKeyInQuery:  true,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "valid basic auth",
			basicAuthUser:  "admin",
			basicAuthPass:  "password123",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "invalid API key",
			apiKey:         "wrong-key",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "invalid basic auth username",
			basicAuthUser:  "wrong",
			basicAuthPass:  "password123",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "invalid basic auth password",
			basicAuthUser:  "admin",
			basicAuthPass:  "wrongpass",
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "no credentials",
			wantStatusCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/admin/dashboard"
			if tt.apiKeyInQuery && tt.apiKey != "" {
				url += "?api_key=" + tt.apiKey
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)

			if tt.apiKey != "" && !tt.apiKeyInQuery {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			if tt.basicAuthUser != "" {
				req.SetBasicAuth(tt.basicAuthUser, tt.basicAuthPass)
			}

			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("status code = %d, want %d", rec.Code, tt.wantStatusCode)
			}

			if tt.wantStatusCode == http.StatusUnauthorized {
				if rec.Header().Get("WWW-Authenticate") == "" {
					t.Error("missing WWW-Authenticate header for unauthorized response")
				}
			}
		})
	}
}

func TestDashboardHandler(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()

	// Set some stats
	cacheClient.SetStat(ctx, "relays:total", 100)
	cacheClient.SetStat(ctx, "relays:online", 80)
	cacheClient.SetStat(ctx, "relays:degraded", 10)
	cacheClient.SetStat(ctx, "relays:offline", 10)
	cacheClient.SetStat(ctx, "discovery:nip65", 50)
	cacheClient.SetStat(ctx, "discovery:nip66", 30)

	tests := []struct {
		name           string
		method         string
		wantStatusCode int
	}{
		{
			name:           "GET returns stats",
			method:         http.MethodGet,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "POST not allowed",
			method:         http.MethodPost,
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT not allowed",
			method:         http.MethodPut,
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "DELETE not allowed",
			method:         http.MethodDelete,
			wantStatusCode: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/admin/dashboard", nil)
			rec := httptest.NewRecorder()

			server.DashboardHandler(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("status code = %d, want %d", rec.Code, tt.wantStatusCode)
			}

			if tt.wantStatusCode == http.StatusOK {
				var resp DashboardResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}

				if resp.Relays.Total != 100 {
					t.Errorf("relays.total = %d, want 100", resp.Relays.Total)
				}
				if resp.Relays.Online != 80 {
					t.Errorf("relays.online = %d, want 80", resp.Relays.Online)
				}
			}
		})
	}
}

func TestDashboardHandler_WithPublisher(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	pub := &mockPublisher{
		publicKey:       "testpubkey123",
		lastPublish:     time.Now(),
		publishCount:    5,
		relaysPublished: 100,
	}
	server.SetPublisher(pub)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	rec := httptest.NewRecorder()

	server.DashboardHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp DashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Publisher == nil {
		t.Fatal("publisher should not be nil")
	}

	if resp.Publisher.PublicKey != "testpubkey123" {
		t.Errorf("publisher.public_key = %s, want testpubkey123", resp.Publisher.PublicKey)
	}

	if resp.Publisher.PublishCount != 5 {
		t.Errorf("publisher.publish_count = %d, want 5", resp.Publisher.PublishCount)
	}

	if resp.Publisher.RelaysPublished != 100 {
		t.Errorf("publisher.relays_published = %d, want 100", resp.Publisher.RelaysPublished)
	}
}

func TestRelaysHandler(t *testing.T) {
	server, cacheClient, mr, monitor, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	t.Run("GET returns relay list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/relays", nil)
		rec := httptest.NewRecorder()

		server.RelaysHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		relays, ok := resp["relays"].([]interface{})
		if !ok {
			t.Fatal("relays should be an array")
		}

		if len(relays) != 2 {
			t.Errorf("relays count = %d, want 2", len(relays))
		}
	})

	t.Run("POST adds relay directly without coordinator", func(t *testing.T) {
		// Create server without coordinator to test direct monitor addition
		serverNoCoord := NewServer(server.cfg, cacheClient, monitor, nil)

		body := `{"url": "wss://new-relay.example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/relays", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		initialCount := monitor.RelayCount()
		serverNoCoord.RelaysHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		// Check relay was added directly to monitor (no coordinator)
		if monitor.RelayCount() <= initialCount {
			t.Error("relay was not added to monitor")
		}
	})

	t.Run("POST with missing URL", func(t *testing.T) {
		body := `{"url": ""}`
		req := httptest.NewRequest(http.MethodPost, "/admin/relays", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.RelaysHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("POST with invalid JSON", func(t *testing.T) {
		body := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/admin/relays", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.RelaysHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("PUT not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/admin/relays", nil)
		rec := httptest.NewRecorder()

		server.RelaysHandler(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestWhitelistHandler(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	t.Run("GET returns empty whitelist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/whitelist", nil)
		rec := httptest.NewRecorder()

		server.WhitelistHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		total, ok := resp["total"].(float64)
		if !ok || total != 0 {
			t.Errorf("total = %v, want 0", resp["total"])
		}
	})

	t.Run("POST adds URL to whitelist", func(t *testing.T) {
		body := `{"url": "wss://whitelist-relay.example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/whitelist", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.WhitelistHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		// Verify it was added
		ctx := req.Context()
		whitelist, _ := cacheClient.GetWhitelist(ctx)
		found := false
		for _, url := range whitelist {
			if url == "wss://whitelist-relay.example.com" {
				found = true
				break
			}
		}
		if !found {
			t.Error("URL was not added to whitelist")
		}
	})

	t.Run("POST with missing URL", func(t *testing.T) {
		body := `{"url": ""}`
		req := httptest.NewRequest(http.MethodPost, "/admin/whitelist", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.WhitelistHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("POST with invalid JSON", func(t *testing.T) {
		body := `{invalid}`
		req := httptest.NewRequest(http.MethodPost, "/admin/whitelist", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.WhitelistHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestBlacklistHandler(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	t.Run("GET returns empty blacklist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/blacklist", nil)
		rec := httptest.NewRecorder()

		server.BlacklistHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		total, ok := resp["total"].(float64)
		if !ok || total != 0 {
			t.Errorf("total = %v, want 0", resp["total"])
		}
	})

	t.Run("POST adds URL to blacklist", func(t *testing.T) {
		body := `{"url": "wss://blacklist-relay.example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/blacklist", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.BlacklistHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		// Verify it was added
		ctx := req.Context()
		blacklist, _ := cacheClient.GetBlacklist(ctx)
		found := false
		for _, url := range blacklist {
			if url == "wss://blacklist-relay.example.com" {
				found = true
				break
			}
		}
		if !found {
			t.Error("URL was not added to blacklist")
		}
	})

	t.Run("POST with missing URL", func(t *testing.T) {
		body := `{"url": ""}`
		req := httptest.NewRequest(http.MethodPost, "/admin/blacklist", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.BlacklistHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestPeersHandler(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	t.Run("GET returns empty peers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/peers", nil)
		rec := httptest.NewRecorder()

		server.PeersHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		total, ok := resp["total"].(float64)
		if !ok || total != 0 {
			t.Errorf("total = %v, want 0", resp["total"])
		}
	})

	t.Run("POST adds pubkey", func(t *testing.T) {
		body := `{"pubkey": "abc123def456abc123def456abc123def456abc123def456abc123def456abc1"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/peers", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.PeersHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		// Verify it was added
		ctx := req.Context()
		peers, _ := cacheClient.GetTrustedPeers(ctx)
		found := false
		for _, pk := range peers {
			if pk == "abc123def456abc123def456abc123def456abc123def456abc123def456abc1" {
				found = true
				break
			}
		}
		if !found {
			t.Error("pubkey was not added to trusted peers")
		}
	})

	t.Run("POST with missing pubkey", func(t *testing.T) {
		body := `{"pubkey": ""}`
		req := httptest.NewRequest(http.MethodPost, "/admin/peers", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.PeersHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestHandler_Routing(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	tests := []struct {
		name           string
		path           string
		wantStatusCode int
	}{
		{
			name:           "root routes to dashboard",
			path:           "/admin",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "dashboard path",
			path:           "/admin/dashboard",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "relays path",
			path:           "/admin/relays",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "whitelist path",
			path:           "/admin/whitelist",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "blacklist path",
			path:           "/admin/blacklist",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "peers path",
			path:           "/admin/peers",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "unknown path",
			path:           "/admin/unknown",
			wantStatusCode: http.StatusNotFound,
		},
		{
			name:           "deeply nested unknown",
			path:           "/admin/some/deep/path",
			wantStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			server.Handler(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("status code = %d, want %d", rec.Code, tt.wantStatusCode)
			}
		})
	}
}

func TestRelayHandler_Delete(t *testing.T) {
	server, cacheClient, mr, monitor, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	// Ensure relay exists
	monitor.AddRelay("wss://to-delete.example.com")

	req := httptest.NewRequest(http.MethodDelete, "/admin/relays/to-delete.example.com", nil)
	rec := httptest.NewRecorder()

	server.RelayHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "removed" {
		t.Errorf("status = %s, want removed", resp["status"])
	}
}

func TestWhitelistItemHandler_Delete(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()

	// Add item to whitelist first
	cacheClient.AddToWhitelist(ctx, "wss://to-remove.example.com")

	req := httptest.NewRequest(http.MethodDelete, "/admin/whitelist/to-remove.example.com", nil)
	rec := httptest.NewRecorder()

	server.WhitelistItemHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "removed" {
		t.Errorf("status = %s, want removed", resp["status"])
	}
}

func TestBlacklistItemHandler_Delete(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()

	// Add item to blacklist first
	cacheClient.AddToBlacklist(ctx, "wss://to-remove.example.com")

	req := httptest.NewRequest(http.MethodDelete, "/admin/blacklist/to-remove.example.com", nil)
	rec := httptest.NewRecorder()

	server.BlacklistItemHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "removed" {
		t.Errorf("status = %s, want removed", resp["status"])
	}
}

func TestPeerHandler_Delete(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()

	pubkey := "abc123def456abc123def456abc123def456abc123def456abc123def456abc1"

	// Add peer first
	cacheClient.AddTrustedPeer(ctx, pubkey)

	req := httptest.NewRequest(http.MethodDelete, "/admin/peers/"+pubkey, nil)
	rec := httptest.NewRecorder()

	server.PeerHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "removed" {
		t.Errorf("status = %s, want removed", resp["status"])
	}
}

func TestHandlers_ContentType(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	handlers := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"Dashboard", server.DashboardHandler},
		{"Relays", server.RelaysHandler},
		{"Whitelist", server.WhitelistHandler},
		{"Blacklist", server.BlacklistHandler},
		{"Peers", server.PeersHandler},
	}

	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			h.handler(rec, req)

			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Content-Type = %s, want application/json", contentType)
			}
		})
	}
}

func TestRelaysHandler_POSTWithCoordinator(t *testing.T) {
	server, cacheClient, mr, _, _ := setupTestServer(t)
	defer mr.Close()
	defer cacheClient.Close()

	body := bytes.NewReader([]byte(`{"url": "wss://via-coordinator.example.com"}`))
	req := httptest.NewRequest(http.MethodPost, "/admin/relays", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.RelaysHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "added" {
		t.Errorf("status = %s, want added", resp["status"])
	}
}
