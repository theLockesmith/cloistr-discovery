// Package metrics provides Prometheus metrics for the discovery service.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Publisher metrics
var (
	PublishCyclesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_publish_cycles_total",
		Help: "Total number of publish cycles completed",
	})

	EventsPublishedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_events_published_total",
		Help: "Total number of Kind 30072 events published",
	}, []string{"relay"})

	PublishErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_publish_errors_total",
		Help: "Total number of publish errors",
	}, []string{"relay", "reason"})

	PublishDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_publish_duration_seconds",
		Help:    "Time taken to complete a publish cycle",
		Buckets: prometheus.ExponentialBuckets(1, 2, 8), // 1s to ~4min
	})
)

// NIP-65 crawler metrics
var (
	NIP65CrawlsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_nip65_crawls_total",
		Help: "Total number of NIP-65 crawl cycles completed",
	})

	NIP65EventsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_nip65_events_processed_total",
		Help: "Total number of NIP-65 events processed",
	})

	NIP65RelaysDiscovered = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_nip65_relays_discovered_total",
		Help: "Total number of relays discovered via NIP-65",
	})

	NIP65CrawlDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_nip65_crawl_duration_seconds",
		Help:    "Time taken to complete a NIP-65 crawl cycle",
		Buckets: prometheus.ExponentialBuckets(1, 2, 8),
	})
)

// NIP-66 consumer metrics
var (
	NIP66EventsConsumed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_nip66_events_consumed_total",
		Help: "Total number of NIP-66 events consumed",
	})

	NIP66RelaysDiscovered = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_nip66_relays_discovered_total",
		Help: "Total number of relays discovered via NIP-66",
	})

	NIP66ConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "discovery_nip66_connections_active",
		Help: "Number of active NIP-66 subscription connections",
	})
)

// Relay monitor / health check metrics
var (
	HealthChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_health_checks_total",
		Help: "Total number of relay health checks",
	}, []string{"status"}) // online, degraded, offline

	HealthCheckDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_health_check_duration_seconds",
		Help:    "Time taken for individual relay health checks",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 8), // 100ms to ~25s
	})

	HealthCheckCycleDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_health_check_cycle_duration_seconds",
		Help:    "Time taken to complete a full health check cycle",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
	})

	NIP11FetchErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_nip11_fetch_errors_total",
		Help: "Total number of NIP-11 fetch errors",
	}, []string{"reason"}) // timeout, connection, parse, http_error

	RelaysMonitored = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "discovery_relays_monitored",
		Help: "Number of relays being monitored",
	})

	RelaysByHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_relays_by_health",
		Help: "Number of relays by health status",
	}, []string{"status"}) // online, degraded, offline
)

// Cache operation metrics
var (
	CacheOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_cache_operations_total",
		Help: "Total number of cache operations",
	}, []string{"operation"}) // set, get, delete

	CacheErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_cache_errors_total",
		Help: "Total number of cache errors",
	}, []string{"operation"})
)

// API metrics (keeping existing ones but adding more)
var (
	QueriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_queries_total",
		Help: "Total number of discovery queries by type",
	}, []string{"type"})

	QueryDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "discovery_query_duration_seconds",
		Help:    "Time taken to handle API queries",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
	}, []string{"type"})
)
