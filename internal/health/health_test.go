package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.workers == nil {
		t.Fatal("workers map not initialized")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	w := &Worker{
		Name:             "test-worker",
		Check:            func() time.Time { return time.Now() },
		ExpectedInterval: 5 * time.Minute,
	}

	r.Register(w)

	if len(r.workers) != 1 {
		t.Errorf("expected 1 worker, got %d", len(r.workers))
	}

	// Original struct should NOT be modified (we copy internally)
	if w.GracePeriod != 0 {
		t.Errorf("expected original GracePeriod to remain 0, got %v", w.GracePeriod)
	}

	// Internal copy should have GracePeriod defaulted
	internal := r.workers["test-worker"]
	if internal.GracePeriod != 5*time.Minute {
		t.Errorf("expected internal GracePeriod to default to ExpectedInterval, got %v", internal.GracePeriod)
	}
}

func TestRegistry_Check_Healthy(t *testing.T) {
	r := NewRegistry()
	now := time.Now()

	r.Register(&Worker{
		Name:             "healthy-worker",
		Check:            func() time.Time { return now },
		ExpectedInterval: 5 * time.Minute,
	})

	status, workers := r.Check()

	if status != StatusHealthy {
		t.Errorf("expected StatusHealthy, got %v", status)
	}
	if len(workers) != 1 {
		t.Errorf("expected 1 worker status, got %d", len(workers))
	}
	if workers[0].Status != StatusHealthy {
		t.Errorf("expected worker StatusHealthy, got %v", workers[0].Status)
	}
}

func TestRegistry_Check_Initializing(t *testing.T) {
	r := NewRegistry()

	// Worker that hasn't run yet (returns zero time)
	r.Register(&Worker{
		Name:             "new-worker",
		Check:            func() time.Time { return time.Time{} },
		ExpectedInterval: 5 * time.Minute,
	})

	status, workers := r.Check()

	if status != StatusHealthy {
		t.Errorf("expected StatusHealthy for initializing worker, got %v", status)
	}
	if workers[0].Message != "initializing" {
		t.Errorf("expected message 'initializing', got %q", workers[0].Message)
	}
}

func TestRegistry_Check_Degraded(t *testing.T) {
	r := NewRegistry()

	// Worker that's past its expected interval but within grace period
	r.Register(&Worker{
		Name:             "slow-worker",
		Check:            func() time.Time { return time.Now().Add(-6 * time.Minute) },
		ExpectedInterval: 5 * time.Minute,
		GracePeriod:      5 * time.Minute,
	})

	status, workers := r.Check()

	if status != StatusDegraded {
		t.Errorf("expected StatusDegraded, got %v", status)
	}
	if workers[0].Status != StatusDegraded {
		t.Errorf("expected worker StatusDegraded, got %v", workers[0].Status)
	}
}

func TestRegistry_Check_Unhealthy(t *testing.T) {
	r := NewRegistry()

	// Worker that's past expected interval + grace period
	r.Register(&Worker{
		Name:             "stale-worker",
		Check:            func() time.Time { return time.Now().Add(-15 * time.Minute) },
		ExpectedInterval: 5 * time.Minute,
		GracePeriod:      5 * time.Minute,
	})

	status, workers := r.Check()

	if status != StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %v", status)
	}
	if workers[0].Status != StatusUnhealthy {
		t.Errorf("expected worker StatusUnhealthy, got %v", workers[0].Status)
	}
}

func TestRegistry_Check_MultipleWorkers(t *testing.T) {
	r := NewRegistry()
	now := time.Now()

	r.Register(&Worker{
		Name:             "healthy-worker",
		Check:            func() time.Time { return now },
		ExpectedInterval: 5 * time.Minute,
	})
	r.Register(&Worker{
		Name:             "stale-worker",
		Check:            func() time.Time { return now.Add(-20 * time.Minute) },
		ExpectedInterval: 5 * time.Minute,
	})

	status, workers := r.Check()

	// Overall status should be unhealthy if any worker is unhealthy
	if status != StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy (worst case), got %v", status)
	}
	if len(workers) != 2 {
		t.Errorf("expected 2 worker statuses, got %d", len(workers))
	}
}

func TestHandler_TextResponse(t *testing.T) {
	r := NewRegistry()
	now := time.Now()

	r.Register(&Worker{
		Name:             "test-worker",
		Check:            func() time.Time { return now },
		ExpectedInterval: 5 * time.Minute,
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	r.Handler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "healthy" {
		t.Errorf("expected body 'healthy', got %q", rec.Body.String())
	}
}

func TestHandler_JSONResponse(t *testing.T) {
	r := NewRegistry()
	now := time.Now()

	r.Register(&Worker{
		Name:             "test-worker",
		Check:            func() time.Time { return now },
		ExpectedInterval: 5 * time.Minute,
	})

	req := httptest.NewRequest(http.MethodGet, "/health?format=json", nil)
	rec := httptest.NewRecorder()

	r.Handler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if resp.Status != StatusHealthy {
		t.Errorf("expected status healthy, got %v", resp.Status)
	}
	if len(resp.Workers) != 1 {
		t.Errorf("expected 1 worker, got %d", len(resp.Workers))
	}
}

func TestHandler_UnhealthyStatus503(t *testing.T) {
	r := NewRegistry()

	r.Register(&Worker{
		Name:             "stale-worker",
		Check:            func() time.Time { return time.Now().Add(-20 * time.Minute) },
		ExpectedInterval: 5 * time.Minute,
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	r.Handler()(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}

func TestHandler_AcceptJSON(t *testing.T) {
	r := NewRegistry()

	r.Register(&Worker{
		Name:             "test-worker",
		Check:            func() time.Time { return time.Now() },
		ExpectedInterval: 5 * time.Minute,
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	r.Handler()(rec, req)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected JSON content type, got %q", rec.Header().Get("Content-Type"))
	}
}
