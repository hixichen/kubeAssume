// Package metrics provides Prometheus metrics for the controller
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "kubeassume"
)

// Metrics holds all Prometheus metrics for the controller.
type Metrics struct {
	// SyncTotal counts total sync operations
	SyncTotal *prometheus.CounterVec
	// SyncDuration measures sync operation duration
	SyncDuration *prometheus.HistogramVec
	// RotationTotal counts rotation events
	RotationTotal *prometheus.CounterVec
	// ActiveKeys tracks the number of active keys
	ActiveKeys prometheus.Gauge
	// PublishErrorsTotal counts publish errors
	PublishErrorsTotal *prometheus.CounterVec
	// LastPublishTimestamp tracks last successful publish
	LastPublishTimestamp prometheus.Gauge
	// FetchErrorsTotal counts fetch errors
	FetchErrorsTotal prometheus.Counter
	// HealthStatus tracks overall health status
	HealthStatus *prometheus.GaugeVec
}

// New creates and registers all metrics.
func New() *Metrics {
	return &Metrics{
		SyncTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "sync_total",
				Help:      "Total number of sync operations",
			},
			[]string{"status"}, // success, error
		),
		SyncDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "sync_duration_seconds",
				Help:      "Duration of sync operations in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"phase"}, // fetch, publish
		),
		RotationTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "rotation_total",
				Help:      "Total number of key rotation events",
			},
			[]string{"type"}, // new_key, key_expired
		),
		ActiveKeys: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "active_keys",
				Help:      "Number of active keys in the JWKS",
			},
		),
		PublishErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "publish_errors_total",
				Help:      "Total number of publish errors",
			},
			[]string{"publisher"}, // s3, gcs, etc.
		),
		LastPublishTimestamp: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "last_publish_timestamp",
				Help:      "Unix timestamp of last successful publish",
			},
		),
		FetchErrorsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "fetch_errors_total",
				Help:      "Total number of OIDC fetch errors",
			},
		),
		HealthStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "health_status",
				Help:      "Health status of components (1=healthy, 0=unhealthy)",
			},
			[]string{"component"},
		),
	}
}

// RecordSync records a sync operation.
func (m *Metrics) RecordSync(status string) {
	m.SyncTotal.WithLabelValues(status).Inc()
}

// RecordSyncDuration records the duration of a sync phase.
func (m *Metrics) RecordSyncDuration(phase string, seconds float64) {
	m.SyncDuration.WithLabelValues(phase).Observe(seconds)
}

// RecordRotation records a rotation event.
func (m *Metrics) RecordRotation(eventType string) {
	m.RotationTotal.WithLabelValues(eventType).Inc()
}

// SetActiveKeys sets the number of active keys.
func (m *Metrics) SetActiveKeys(count int) {
	m.ActiveKeys.Set(float64(count))
}

// RecordPublishError records a publish error.
func (m *Metrics) RecordPublishError(publisher string) {
	m.PublishErrorsTotal.WithLabelValues(publisher).Inc()
}

// RecordPublish records a successful publish.
func (m *Metrics) RecordPublish(timestamp float64) {
	m.LastPublishTimestamp.Set(timestamp)
}

// RecordFetchError records a fetch error.
func (m *Metrics) RecordFetchError() {
	m.FetchErrorsTotal.Inc()
}

// SetHealthStatus sets the health status of a component.
func (m *Metrics) SetHealthStatus(component string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	m.HealthStatus.WithLabelValues(component).Set(value)
}

// Register registers all metrics with a prometheus registry.
// Use this for testing with custom registries.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.SyncTotal,
		m.SyncDuration,
		m.RotationTotal,
		m.ActiveKeys,
		m.PublishErrorsTotal,
		m.LastPublishTimestamp,
		m.FetchErrorsTotal,
		m.HealthStatus,
	}

	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}
