// Package main is the entry point for coldforge-discovery.
// Coldforge Discovery implements the Nostr Discovery Protocol (NDP):
// - Relay Directory Entry (Kind 30072): Publish verified relay information
//
// Discovery gathers relay data through NIP-11 metadata fetches and health checks.
// This is the fallback mechanism when other relays don't publish Kind 30072 events.
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

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/admin"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/api"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/cache"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/config"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/discovery"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/health"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/publisher"
	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/relay"
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

	// Initialize health registry
	healthRegistry := health.NewRegistry()

	// HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthRegistry.Handler())
	mux.HandleFunc("/metrics", apiServer.MetricsHandler)
	mux.HandleFunc("/api/v1/relays", apiServer.RelaysHandler)

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

	// Register relay monitor with health registry
	healthRegistry.Register(&health.Worker{
		Name:             "relay-monitor",
		Check:            relayMonitor.LastCheck,
		ExpectedInterval: time.Duration(cfg.RelayCheckInterval) * time.Second,
	})

	// Start discovery coordinator goroutine
	discoveryCoordinator := discovery.NewCoordinator(cfg, cacheClient, relayMonitor.DiscoveryChannel())
	go func() {
		slog.Info("starting discovery coordinator")
		discoveryCoordinator.Start(bgCtx)
	}()

	// Register discovery sources with health registry
	if discoveryCoordinator.IsNIP65Enabled() {
		healthRegistry.Register(&health.Worker{
			Name:             "nip65-crawler",
			Check:            discoveryCoordinator.NIP65LastCrawl,
			ExpectedInterval: time.Duration(cfg.NIP65CrawlInterval) * time.Minute,
		})
	}
	if discoveryCoordinator.IsNIP66Enabled() {
		healthRegistry.Register(&health.Worker{
			Name:             "nip66-consumer",
			Check:            discoveryCoordinator.NIP66LastConsume,
			ExpectedInterval: 10 * time.Minute, // NIP-66 should receive events frequently
			GracePeriod:      20 * time.Minute, // Allow extra grace for sparse events
		})
	}

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

			// Register publisher with health registry
			healthRegistry.Register(&health.Worker{
				Name:             "publisher",
				Check:            eventPublisher.GetLastPublish,
				ExpectedInterval: time.Duration(cfg.PublishInterval) * time.Minute,
			})
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
