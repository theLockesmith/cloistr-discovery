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

// NIP-66 publisher metrics
var (
	NIP66PublishCyclesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "discovery_nip66_publish_cycles_total",
		Help: "Total number of NIP-66 publish cycles completed",
	})

	NIP66EventsPublished = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_nip66_events_published_total",
		Help: "Total number of NIP-66 events published",
	}, []string{"kind", "relay"}) // kind: "10166" or "30166"

	NIP66PublishErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_nip66_publish_errors_total",
		Help: "Total number of NIP-66 publish errors",
	}, []string{"kind", "relay", "reason"})

	NIP66PublishDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_nip66_publish_duration_seconds",
		Help:    "Time taken to complete a NIP-66 publish cycle",
		Buckets: prometheus.ExponentialBuckets(1, 2, 8),
	})

	NIP66RelaysPublished = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "discovery_nip66_relays_published",
		Help: "Number of relays published in last NIP-66 cycle",
	})
)

// NIP-65 user relay list lookup metrics
var (
	NIP65UserLookupTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_nip65_user_lookup_total",
		Help: "Total number of NIP-65 user relay list lookups",
	}, []string{"cache_hit"}) // "true" or "false"

	NIP65UserLookupDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_nip65_user_lookup_duration_seconds",
		Help:    "Time taken to lookup user NIP-65 relay list",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 8), // 100ms to ~25s
	})

	NIP65UserLookupErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "discovery_nip65_user_lookup_errors_total",
		Help: "Total number of NIP-65 user lookup errors",
	}, []string{"reason"}) // "timeout", "no_events", "invalid_pubkey", "fetch_error"
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

// Relay network aggregate metrics
var (
	RelaysByNIP = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_nip",
		Help: "Number of relays supporting each NIP",
	}, []string{"nip"})

	RelaysByCountry = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_country",
		Help: "Number of relays by country code",
	}, []string{"country"})

	RelaysByContentPolicy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_content_policy",
		Help: "Number of relays by content policy",
	}, []string{"policy"}) // anything, sfw, nsfw-allowed, nsfw-only

	RelaysByModeration = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_moderation",
		Help: "Number of relays by moderation level",
	}, []string{"level"}) // unmoderated, light, active, strict

	RelaysBySoftware = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_software",
		Help: "Number of relays by software type",
	}, []string{"software"})

	RelaysByPayment = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_payment",
		Help: "Number of relays by payment requirement",
	}, []string{"required"}) // true, false

	RelaysByAuth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "discovery_network_relays_by_auth",
		Help: "Number of relays by auth requirement",
	}, []string{"required"}) // true, false

	RelayLatencyMilliseconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "discovery_network_relay_latency_milliseconds",
		Help:    "Distribution of relay response latencies in milliseconds",
		Buckets: prometheus.ExponentialBuckets(50, 2, 10), // 50ms to ~50s
	})

	RelayLatencySummary = promauto.NewSummary(prometheus.SummaryOpts{
		Name:       "discovery_network_relay_latency_summary_milliseconds",
		Help:       "Summary of relay response latencies with quantiles",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
)
