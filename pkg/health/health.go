// Package health provides health check functionality for the controller
package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Status represents the health status.
type Status string

const (
	// StatusHealthy indicates the component is healthy.
	StatusHealthy Status = "healthy"
	// StatusDegraded indicates the component is degraded but functioning.
	StatusDegraded Status = "degraded"
	// StatusUnhealthy indicates the component is unhealthy.
	StatusUnhealthy Status = "unhealthy"
)

// Check represents a single health check.
type Check struct {
	Name    string    `json:"name"`
	Status  Status    `json:"status"`
	Message string    `json:"message,omitempty"`
	LastRun time.Time `json:"lastRun"`
}

// Result represents the overall health check result.
type Result struct {
	Status  string            `json:"status"`
	Checks  map[string]*Check `json:"checks"`
	Version string            `json:"version,omitempty"`
}

// Checker defines a function that performs a health check.
type Checker func(ctx context.Context) error

// Health manages health checks for the controller.
type Health struct {
	checkers map[string]Checker
	results  map[string]*Check
	mu       sync.RWMutex
	logger   *slog.Logger
}

// New creates a new Health instance.
func New(logger *slog.Logger) *Health {
	return &Health{
		checkers: make(map[string]Checker),
		results:  make(map[string]*Check),
		logger:   logger,
	}
}

// Register registers a health check.
func (h *Health) Register(name string, checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[name] = checker
	h.results[name] = &Check{
		Name:   name,
		Status: StatusUnhealthy,
	}
}

// RunAll runs all registered health checks.
func (h *Health) RunAll(ctx context.Context) *Result {
	h.mu.RLock()
	checkers := make(map[string]Checker)
	for name, checker := range h.checkers {
		checkers[name] = checker
	}
	h.mu.RUnlock()

	results := make(map[string]*Check)
	now := time.Now()

	for name, checker := range checkers {
		err := checker(ctx)
		check := &Check{
			Name:    name,
			LastRun: now,
		}
		if err != nil {
			check.Status = StatusUnhealthy
			check.Message = err.Error()
			h.logger.Error("Health check failed", "name", name, "error", err)
		} else {
			check.Status = StatusHealthy
			check.Message = "OK"
		}
		results[name] = check
	}

	// Update results
	h.mu.Lock()
	for name, check := range results {
		h.results[name] = check
	}
	h.mu.Unlock()

	// Compute overall status
	status := computeOverallStatus(results)

	return &Result{
		Status: string(status),
		Checks: results,
	}
}

// Run runs a specific health check by name.
func (h *Health) Run(ctx context.Context, name string) (*Check, error) {
	h.mu.RLock()
	checker, exists := h.checkers[name]
	h.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("health check not found: %s", name)
	}

	// Run checker
	err := checker(ctx)

	// Update result
	now := time.Now()
	check := &Check{
		Name:    name,
		LastRun: now,
	}

	if err != nil {
		check.Status = StatusUnhealthy
		check.Message = err.Error()
	} else {
		check.Status = StatusHealthy
		check.Message = "OK"
	}

	h.mu.Lock()
	h.results[name] = check
	h.mu.Unlock()

	return check, nil
}

// GetResult returns the current health result without running checks.
func (h *Health) GetResult() *Result {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := &Result{
		Status: string(StatusHealthy),
		Checks: make(map[string]*Check),
	}

	for name, check := range h.results {
		result.Checks[name] = check
		if check.Status == StatusUnhealthy {
			result.Status = string(StatusUnhealthy)
		} else if check.Status == StatusDegraded && result.Status != string(StatusUnhealthy) {
			result.Status = string(StatusDegraded)
		}
	}

	return result
}

// IsHealthy returns true if all checks are healthy.
func (h *Health) IsHealthy() bool {
	return h.GetResult().Status == string(StatusHealthy)
}

// LivenessCheck returns a checker for Kubernetes liveness probe.
func (h *Health) LivenessCheck() Checker {
	return func(ctx context.Context) error {
		// Check critical components are running
		// For now, just check if we can access the health manager
		return nil
	}
}

// ReadinessCheck returns a checker for Kubernetes readiness probe.
func (h *Health) ReadinessCheck() Checker {
	return func(ctx context.Context) error {
		// Check all components are ready to serve
		result := h.GetResult()
		if result.Status == string(StatusUnhealthy) {
			return fmt.Errorf("health check failed")
		}
		return nil
	}
}

// computeOverallStatus computes the overall status from individual checks.
func computeOverallStatus(checks map[string]*Check) Status {
	status := StatusHealthy
	for _, check := range checks {
		if check.Status == StatusUnhealthy {
			return StatusUnhealthy
		}
		if check.Status == StatusDegraded {
			status = StatusDegraded
		}
	}
	return status
}
