// Package main is the entry point for coldforge-discovery.
// Coldforge Discovery implements the Nostr Discovery Protocol (NDP):
// - Relay Discovery (Kind 30069): Monitor and catalog Nostr relays
// - Content Routing (Kind 30066): Index which relays have which pubkeys
// - Activity Discovery (Kind 30067): Track real-time user activities
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.com/coldforge/coldforge-discovery/internal/activity"
	"gitlab.com/coldforge/coldforge-discovery/internal/admin"
	"gitlab.com/coldforge/coldforge-discovery/internal/api"
	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
	"gitlab.com/coldforge/coldforge-discovery/internal/discovery"
	"gitlab.com/coldforge/coldforge-discovery/internal/inventory"
	"gitlab.com/coldforge/coldforge-discovery/internal/publisher"
	"gitlab.com/coldforge/coldforge-discovery/internal/relay"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Setup structured logging
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("starting coldforge-discovery",
		"version", "0.1.0",
		"port", cfg.Port,
	)

	// Initialize cache (Dragonfly/Redis)
	cacheClient, err := cache.New(cfg.CacheURL)
	if err != nil {
		slog.Error("failed to connect to cache", "error", err, "url", cfg.CacheURL)
		os.Exit(1)
	}
	defer cacheClient.Close()

	// Verify cache connection
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := cacheClient.Ping(pingCtx); err != nil {
		slog.Error("cache ping failed", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to cache", "url", cfg.CacheURL)

	// Initialize API server
	apiServer := api.New(cfg, cacheClient)

	// HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/metrics", apiServer.MetricsHandler)
	mux.HandleFunc("/api/v1/relays", apiServer.RelaysHandler)
	mux.HandleFunc("/api/v1/pubkey/", apiServer.PubkeyHandler)
	mux.HandleFunc("/api/v1/activity/", apiServer.ActivityHandler)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("HTTP server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	// Create context for background services
	bgCtx, bgCancel := context.WithCancel(context.Background())

	// Start relay monitoring goroutine
	relayMonitor := relay.NewMonitor(cfg, cacheClient)
	go func() {
		slog.Info("starting relay monitor")
		relayMonitor.Start(bgCtx)
	}()

	// Start discovery coordinator goroutine
	discoveryCoordinator := discovery.NewCoordinator(cfg, cacheClient, relayMonitor.DiscoveryChannel())
	go func() {
		slog.Info("starting discovery coordinator")
		discoveryCoordinator.Start(bgCtx)
	}()

	// Start inventory indexing goroutine
	inventoryIndexer := inventory.NewIndexer(cfg, cacheClient)
	go func() {
		slog.Info("starting inventory indexer")
		// Subscribe to seed relays for inventory events
		for _, relayURL := range cfg.SeedRelays {
			if err := inventoryIndexer.SubscribeToRelay(bgCtx, relayURL); err != nil {
				slog.Error("failed to subscribe to relay for inventory", "url", relayURL, "error", err)
			}
		}
		inventoryIndexer.Start(bgCtx)
	}()

	// Start activity tracking goroutine
	activityTracker := activity.NewTracker(cfg, cacheClient)
	go func() {
		slog.Info("starting activity tracker")
		// Subscribe to seed relays for activity events
		for _, relayURL := range cfg.SeedRelays {
			if err := activityTracker.SubscribeToRelay(bgCtx, relayURL); err != nil {
				slog.Error("failed to subscribe to relay for activity", "url", relayURL, "error", err)
			}
		}
		activityTracker.Start(bgCtx)
	}()

	// Start publisher goroutine (if enabled)
	var eventPublisher *publisher.Publisher
	if cfg.PublishEnabled {
		var err error
		eventPublisher, err = publisher.New(cfg, cacheClient)
		if err != nil {
			slog.Error("failed to initialize publisher", "error", err)
		} else {
			go func() {
				slog.Info("starting event publisher")
				eventPublisher.Start(bgCtx)
			}()
		}
	}

	// Initialize admin interface (if enabled)
	if cfg.AdminEnabled {
		adminServer := admin.NewServer(cfg, cacheClient, relayMonitor, discoveryCoordinator)
		if eventPublisher != nil {
			adminServer.SetPublisher(eventPublisher)
		}
		mux.HandleFunc("/admin/", adminServer.AuthMiddleware(adminServer.Handler))
		slog.Info("admin interface enabled", "path", "/admin/")
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")

	// Cancel background services first
	bgCancel()

	// Give background services time to clean up
	time.Sleep(500 * time.Millisecond)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("shutdown complete")
}
