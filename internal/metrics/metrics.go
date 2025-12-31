// Package metrics provides Prometheus metrics for Mycel services.
package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Default metrics registry
	defaultRegistry *Registry
	initOnce        sync.Once
)

// Registry holds all Mycel metrics.
type Registry struct {
	reg *prometheus.Registry

	// Request metrics
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight *prometheus.GaugeVec

	// Connector metrics
	ConnectorHealth     *prometheus.GaugeVec
	ConnectorOperations *prometheus.CounterVec
	ConnectorLatency    *prometheus.HistogramVec

	// Flow metrics
	FlowExecutions   *prometheus.CounterVec
	FlowDuration     *prometheus.HistogramVec
	FlowErrors       *prometheus.CounterVec

	// Cache metrics
	CacheHits   *prometheus.CounterVec
	CacheMisses *prometheus.CounterVec
	CacheSize   *prometheus.GaugeVec

	// Runtime metrics
	UptimeSeconds *prometheus.GaugeVec
	GoRoutines    prometheus.Gauge

	// Service info
	ServiceInfo *prometheus.GaugeVec
}

// NewRegistry creates a new metrics registry with all Mycel metrics.
func NewRegistry(serviceName, version string) *Registry {
	reg := prometheus.NewRegistry()

	r := &Registry{
		reg: reg,

		// Request metrics
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_requests_total",
				Help: "Total number of HTTP requests processed",
			},
			[]string{"method", "path", "status"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		RequestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_requests_in_flight",
				Help: "Current number of requests being processed",
			},
			[]string{"method", "path"},
		),

		// Connector metrics
		ConnectorHealth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_connector_health",
				Help: "Connector health status (1=healthy, 0=unhealthy)",
			},
			[]string{"connector", "type"},
		),
		ConnectorOperations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_connector_operations_total",
				Help: "Total number of connector operations",
			},
			[]string{"connector", "type", "operation", "status"},
		),
		ConnectorLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_connector_latency_seconds",
				Help:    "Connector operation latency in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"connector", "type", "operation"},
		),

		// Flow metrics
		FlowExecutions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_flow_executions_total",
				Help: "Total number of flow executions",
			},
			[]string{"flow", "status"},
		),
		FlowDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_flow_duration_seconds",
				Help:    "Flow execution duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"flow"},
		),
		FlowErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_flow_errors_total",
				Help: "Total number of flow execution errors",
			},
			[]string{"flow", "error_type"},
		),

		// Cache metrics
		CacheHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_cache_hits_total",
				Help: "Total number of cache hits",
			},
			[]string{"cache"},
		),
		CacheMisses: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_cache_misses_total",
				Help: "Total number of cache misses",
			},
			[]string{"cache"},
		),
		CacheSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_cache_size",
				Help: "Current number of items in cache",
			},
			[]string{"cache"},
		),

		// Runtime metrics
		UptimeSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_uptime_seconds",
				Help: "Service uptime in seconds",
			},
			[]string{},
		),
		GoRoutines: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "mycel_goroutines",
				Help: "Current number of goroutines",
			},
		),

		// Service info
		ServiceInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_service_info",
				Help: "Service information",
			},
			[]string{"service", "version"},
		),
	}

	// Register all metrics
	reg.MustRegister(
		r.RequestsTotal,
		r.RequestDuration,
		r.RequestsInFlight,
		r.ConnectorHealth,
		r.ConnectorOperations,
		r.ConnectorLatency,
		r.FlowExecutions,
		r.FlowDuration,
		r.FlowErrors,
		r.CacheHits,
		r.CacheMisses,
		r.CacheSize,
		r.UptimeSeconds,
		r.GoRoutines,
		r.ServiceInfo,
	)

	// Set service info
	r.ServiceInfo.WithLabelValues(serviceName, version).Set(1)

	return r
}

// Handler returns an HTTP handler for the /metrics endpoint.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordRequest records a request with method, path, and status.
func (r *Registry) RecordRequest(method, path, status string, duration time.Duration) {
	r.RequestsTotal.WithLabelValues(method, path, status).Inc()
	r.RequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// IncRequestsInFlight increments the in-flight requests counter.
func (r *Registry) IncRequestsInFlight(method, path string) {
	r.RequestsInFlight.WithLabelValues(method, path).Inc()
}

// DecRequestsInFlight decrements the in-flight requests counter.
func (r *Registry) DecRequestsInFlight(method, path string) {
	r.RequestsInFlight.WithLabelValues(method, path).Dec()
}

// SetConnectorHealth sets the health status of a connector.
func (r *Registry) SetConnectorHealth(name, connType string, healthy bool) {
	val := 0.0
	if healthy {
		val = 1.0
	}
	r.ConnectorHealth.WithLabelValues(name, connType).Set(val)
}

// RecordConnectorOperation records a connector operation.
func (r *Registry) RecordConnectorOperation(connector, connType, operation, status string, duration time.Duration) {
	r.ConnectorOperations.WithLabelValues(connector, connType, operation, status).Inc()
	r.ConnectorLatency.WithLabelValues(connector, connType, operation).Observe(duration.Seconds())
}

// RecordFlowExecution records a flow execution.
func (r *Registry) RecordFlowExecution(flow, status string, duration time.Duration) {
	r.FlowExecutions.WithLabelValues(flow, status).Inc()
	r.FlowDuration.WithLabelValues(flow).Observe(duration.Seconds())
}

// RecordFlowError records a flow error.
func (r *Registry) RecordFlowError(flow, errorType string) {
	r.FlowErrors.WithLabelValues(flow, errorType).Inc()
}

// RecordCacheHit records a cache hit.
func (r *Registry) RecordCacheHit(cache string) {
	r.CacheHits.WithLabelValues(cache).Inc()
}

// RecordCacheMiss records a cache miss.
func (r *Registry) RecordCacheMiss(cache string) {
	r.CacheMisses.WithLabelValues(cache).Inc()
}

// SetCacheSize sets the current cache size.
func (r *Registry) SetCacheSize(cache string, size int64) {
	r.CacheSize.WithLabelValues(cache).Set(float64(size))
}

// SetUptime sets the current uptime in seconds.
func (r *Registry) SetUptime(seconds float64) {
	r.UptimeSeconds.WithLabelValues().Set(seconds)
}

// SetGoRoutines sets the current number of goroutines.
func (r *Registry) SetGoRoutines(count int) {
	r.GoRoutines.Set(float64(count))
}

// Default returns the default metrics registry.
// Creates one with default settings if not already initialized.
func Default() *Registry {
	initOnce.Do(func() {
		defaultRegistry = NewRegistry("mycel", "unknown")
	})
	return defaultRegistry
}

// SetDefault sets the default metrics registry.
func SetDefault(r *Registry) {
	defaultRegistry = r
}
