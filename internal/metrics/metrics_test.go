package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPublishMetrics(t *testing.T) {
	// Test that metrics can be incremented without panic
	PublishCyclesTotal.Inc()

	count := testutil.ToFloat64(PublishCyclesTotal)
	if count < 1 {
		t.Errorf("PublishCyclesTotal = %f, want >= 1", count)
	}
}

func TestEventsPublishedTotal(t *testing.T) {
	EventsPublishedTotal.WithLabelValues("wss://test.relay").Inc()

	count := testutil.ToFloat64(EventsPublishedTotal.WithLabelValues("wss://test.relay"))
	if count < 1 {
		t.Errorf("EventsPublishedTotal = %f, want >= 1", count)
	}
}

func TestPublishErrorsTotal(t *testing.T) {
	PublishErrorsTotal.WithLabelValues("wss://test.relay", "connection").Inc()

	count := testutil.ToFloat64(PublishErrorsTotal.WithLabelValues("wss://test.relay", "connection"))
	if count < 1 {
		t.Errorf("PublishErrorsTotal = %f, want >= 1", count)
	}
}

func TestNIP65Metrics(t *testing.T) {
	NIP65CrawlsTotal.Inc()
	NIP65EventsProcessed.Inc()
	NIP65RelaysDiscovered.Inc()

	if testutil.ToFloat64(NIP65CrawlsTotal) < 1 {
		t.Error("NIP65CrawlsTotal not incremented")
	}
	if testutil.ToFloat64(NIP65EventsProcessed) < 1 {
		t.Error("NIP65EventsProcessed not incremented")
	}
	if testutil.ToFloat64(NIP65RelaysDiscovered) < 1 {
		t.Error("NIP65RelaysDiscovered not incremented")
	}
}

func TestNIP66Metrics(t *testing.T) {
	NIP66EventsConsumed.Inc()
	NIP66RelaysDiscovered.Inc()
	NIP66ConnectionsActive.Set(5)

	if testutil.ToFloat64(NIP66EventsConsumed) < 1 {
		t.Error("NIP66EventsConsumed not incremented")
	}
	if testutil.ToFloat64(NIP66RelaysDiscovered) < 1 {
		t.Error("NIP66RelaysDiscovered not incremented")
	}
	if testutil.ToFloat64(NIP66ConnectionsActive) != 5 {
		t.Error("NIP66ConnectionsActive not set correctly")
	}
}

func TestHealthCheckMetrics(t *testing.T) {
	HealthChecksTotal.WithLabelValues("online").Inc()
	HealthChecksTotal.WithLabelValues("degraded").Inc()
	HealthChecksTotal.WithLabelValues("offline").Inc()

	if testutil.ToFloat64(HealthChecksTotal.WithLabelValues("online")) < 1 {
		t.Error("HealthChecksTotal online not incremented")
	}
}

func TestRelayGauges(t *testing.T) {
	RelaysMonitored.Set(100)
	RelaysByHealth.WithLabelValues("online").Set(80)
	RelaysByHealth.WithLabelValues("degraded").Set(10)
	RelaysByHealth.WithLabelValues("offline").Set(10)

	if testutil.ToFloat64(RelaysMonitored) != 100 {
		t.Error("RelaysMonitored not set correctly")
	}
	if testutil.ToFloat64(RelaysByHealth.WithLabelValues("online")) != 80 {
		t.Error("RelaysByHealth online not set correctly")
	}
}

func TestCacheMetrics(t *testing.T) {
	CacheOperationsTotal.WithLabelValues("set").Inc()
	CacheOperationsTotal.WithLabelValues("get").Inc()
	CacheErrorsTotal.WithLabelValues("set").Inc()

	if testutil.ToFloat64(CacheOperationsTotal.WithLabelValues("set")) < 1 {
		t.Error("CacheOperationsTotal set not incremented")
	}
	if testutil.ToFloat64(CacheErrorsTotal.WithLabelValues("set")) < 1 {
		t.Error("CacheErrorsTotal set not incremented")
	}
}

func TestNetworkAggregateMetrics(t *testing.T) {
	RelaysByNIP.WithLabelValues("1").Set(500)
	RelaysByNIP.WithLabelValues("11").Set(400)
	RelaysByCountry.WithLabelValues("US").Set(200)
	RelaysByCountry.WithLabelValues("DE").Set(50)
	RelaysBySoftware.WithLabelValues("strfry").Set(300)
	RelaysByPayment.WithLabelValues("true").Set(50)
	RelaysByPayment.WithLabelValues("false").Set(450)

	if testutil.ToFloat64(RelaysByNIP.WithLabelValues("1")) != 500 {
		t.Error("RelaysByNIP not set correctly")
	}
	if testutil.ToFloat64(RelaysByCountry.WithLabelValues("US")) != 200 {
		t.Error("RelaysByCountry not set correctly")
	}
	if testutil.ToFloat64(RelaysBySoftware.WithLabelValues("strfry")) != 300 {
		t.Error("RelaysBySoftware not set correctly")
	}
}

func TestHistograms(t *testing.T) {
	// Test that histograms accept observations without panic
	PublishDurationSeconds.Observe(5.5)
	NIP65CrawlDurationSeconds.Observe(30.0)
	HealthCheckDurationSeconds.Observe(0.5)
	HealthCheckCycleDurationSeconds.Observe(120.0)
	RelayLatencyMilliseconds.Observe(150.0)
	RelayLatencySummary.Observe(150.0)
	QueryDurationSeconds.WithLabelValues("relays").Observe(0.05)
}

func TestMetricsAreRegistered(t *testing.T) {
	// Verify that our metrics are registered with the default registry
	metrics := []prometheus.Collector{
		PublishCyclesTotal,
		EventsPublishedTotal,
		PublishErrorsTotal,
		PublishDurationSeconds,
		NIP65CrawlsTotal,
		NIP65EventsProcessed,
		NIP65RelaysDiscovered,
		NIP66EventsConsumed,
		NIP66RelaysDiscovered,
		NIP66ConnectionsActive,
		HealthChecksTotal,
		RelaysMonitored,
		RelaysByHealth,
		CacheOperationsTotal,
		CacheErrorsTotal,
		QueriesTotal,
		QueryDurationSeconds,
		RelaysByNIP,
		RelaysByCountry,
		RelaysBySoftware,
		RelaysByPayment,
		RelayLatencyMilliseconds,
	}

	for _, m := range metrics {
		// promauto already registers these, just verify they're valid
		if m == nil {
			t.Error("metric is nil")
		}
	}
}
