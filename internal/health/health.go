// Package health provides health checking for background goroutines.
package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Status represents the health status of a worker or the overall system.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// WorkerStatus represents the health status of a single worker.
type WorkerStatus struct {
	Name       string    `json:"name"`
	Status     Status    `json:"status"`
	LastActive time.Time `json:"last_active,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// HealthResponse is the JSON response for the health endpoint.
type HealthResponse struct {
	Status  Status         `json:"status"`
	Workers []WorkerStatus `json:"workers"`
}

// CheckFunc returns the last active time for a worker.
// Return time.Time{} if the worker doesn't track last active time.
type CheckFunc func() time.Time

// Worker represents a background worker to monitor.
type Worker struct {
	Name             string
	Check            CheckFunc
	ExpectedInterval time.Duration
	GracePeriod      time.Duration // Extra time before marking unhealthy (default: ExpectedInterval)
}

// Registry tracks background workers for health checking.
type Registry struct {
	mu      sync.RWMutex
	workers map[string]*Worker
}

// NewRegistry creates a new health registry.
func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]*Worker),
	}
}

// Register adds a worker to the registry.
func (r *Registry) Register(w *Worker) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Copy to avoid modifying the caller's struct
	workerCopy := *w
	if workerCopy.GracePeriod == 0 {
		workerCopy.GracePeriod = workerCopy.ExpectedInterval
	}
	r.workers[workerCopy.Name] = &workerCopy
}

// Check returns the overall health status and individual worker statuses.
func (r *Registry) Check() (Status, []WorkerStatus) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	overall := StatusHealthy
	statuses := make([]WorkerStatus, 0, len(r.workers))
	now := time.Now()

	for _, w := range r.workers {
		status := WorkerStatus{
			Name: w.Name,
		}

		lastActive := w.Check()
		if lastActive.IsZero() {
			// Worker doesn't track last active or hasn't run yet
			// For first startup, give them time to initialize
			status.Status = StatusHealthy
			status.Message = "initializing"
		} else {
			status.LastActive = lastActive
			staleThreshold := w.ExpectedInterval + w.GracePeriod

			timeSinceActive := now.Sub(lastActive)
			if timeSinceActive > staleThreshold {
				status.Status = StatusUnhealthy
				status.Message = fmt.Sprintf("stale: no activity for %v (threshold: %v)",
					timeSinceActive.Round(time.Second), staleThreshold.Round(time.Second))
				overall = StatusUnhealthy
			} else if timeSinceActive > w.ExpectedInterval {
				status.Status = StatusDegraded
				status.Message = fmt.Sprintf("slow: last activity %v ago",
					timeSinceActive.Round(time.Second))
				if overall == StatusHealthy {
					overall = StatusDegraded
				}
			} else {
				status.Status = StatusHealthy
				status.Message = "ok"
			}
		}

		statuses = append(statuses, status)
	}

	// Sort workers by name for consistent output
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	return overall, statuses
}

// Handler returns an HTTP handler for the health endpoint.
func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		overall, workers := r.Check()

		// Set status code based on health
		switch overall {
		case StatusHealthy:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK) // Still return 200 for degraded
		case StatusUnhealthy:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		// Check if client wants JSON (Accept header or ?format=json)
		acceptJSON := req.Header.Get("Accept") == "application/json" ||
			req.URL.Query().Get("format") == "json"

		if acceptJSON {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(HealthResponse{
				Status:  overall,
				Workers: workers,
			}); err != nil {
				// Log but don't change status - headers already sent
				return
			}
		} else {
			// Simple text response for basic health checks
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(string(overall)))
		}
	}
}
