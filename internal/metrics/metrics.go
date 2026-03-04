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

	// Lock metrics
	LockAcquired    *prometheus.CounterVec
	LockReleased    *prometheus.CounterVec
	LockWaitSeconds *prometheus.HistogramVec
	LockTimeout     *prometheus.CounterVec
	LockHeld        *prometheus.GaugeVec

	// Semaphore metrics
	SemaphoreAcquired    *prometheus.CounterVec
	SemaphoreReleased    *prometheus.CounterVec
	SemaphoreWaitSeconds *prometheus.HistogramVec
	SemaphoreTimeout     *prometheus.CounterVec
	SemaphoreAvailable   *prometheus.GaugeVec

	// Coordinate metrics
	CoordinateSignal      *prometheus.CounterVec
	CoordinateWait        *prometheus.CounterVec
	CoordinateWaitSeconds *prometheus.HistogramVec
	CoordinateTimeout     *prometheus.CounterVec
	CoordinatePreflightHit *prometheus.CounterVec
	CoordinateActiveWaits *prometheus.GaugeVec

	// Scheduler metrics
	ScheduledFlows   *prometheus.GaugeVec
	ScheduleExecuted *prometheus.CounterVec

	// Profile metrics
	ProfileActive    *prometheus.GaugeVec
	ProfileRequests  *prometheus.CounterVec
	ProfileErrors    *prometheus.CounterVec
	ProfileFallback  *prometheus.CounterVec
	ProfileLatency   *prometheus.HistogramVec

	// Runtime metrics
	UptimeSeconds *prometheus.GaugeVec
	GoRoutines    prometheus.Gauge

	// Service info
	ServiceInfo *prometheus.GaugeVec
}

// NewRegistry creates a new metrics registry with all Mycel metrics.
func NewRegistry(serviceName, version, mycelVersion string) *Registry {
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

		// Lock metrics
		LockAcquired: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_lock_acquired_total",
				Help: "Total number of locks acquired",
			},
			[]string{"key"},
		),
		LockReleased: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_lock_released_total",
				Help: "Total number of locks released",
			},
			[]string{"key"},
		),
		LockWaitSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_lock_wait_seconds",
				Help:    "Time spent waiting to acquire a lock",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"key"},
		),
		LockTimeout: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_lock_timeout_total",
				Help: "Total number of lock acquisition timeouts",
			},
			[]string{"key"},
		),
		LockHeld: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_lock_held",
				Help: "Current number of held locks",
			},
			[]string{"key"},
		),

		// Semaphore metrics
		SemaphoreAcquired: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_semaphore_acquired_total",
				Help: "Total number of semaphore permits acquired",
			},
			[]string{"key"},
		),
		SemaphoreReleased: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_semaphore_released_total",
				Help: "Total number of semaphore permits released",
			},
			[]string{"key"},
		),
		SemaphoreWaitSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_semaphore_wait_seconds",
				Help:    "Time spent waiting to acquire a semaphore permit",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"key"},
		),
		SemaphoreTimeout: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_semaphore_timeout_total",
				Help: "Total number of semaphore acquisition timeouts",
			},
			[]string{"key"},
		),
		SemaphoreAvailable: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_semaphore_available",
				Help: "Current number of available semaphore permits",
			},
			[]string{"key"},
		),

		// Coordinate metrics
		CoordinateSignal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_signal_total",
				Help: "Total number of signals emitted",
			},
			[]string{"signal"},
		),
		CoordinateWait: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_wait_total",
				Help: "Total number of waits started",
			},
			[]string{"signal"},
		),
		CoordinateWaitSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_coordinate_wait_seconds",
				Help:    "Time spent waiting for a signal",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"signal"},
		),
		CoordinateTimeout: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_timeout_total",
				Help: "Total number of coordinate wait timeouts",
			},
			[]string{"signal"},
		),
		CoordinatePreflightHit: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_preflight_hit_total",
				Help: "Total number of preflight check hits (already exists)",
			},
			[]string{"connector"},
		),
		CoordinateActiveWaits: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_coordinate_active_waits",
				Help: "Current number of active waits",
			},
			[]string{"signal"},
		),

		// Scheduler metrics
		ScheduledFlows: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_scheduled_flows",
				Help: "Current number of scheduled flows",
			},
			[]string{},
		),
		ScheduleExecuted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_schedule_executed_total",
				Help: "Total number of scheduled flow executions",
			},
			[]string{"flow", "status"},
		),

		// Profile metrics
		ProfileActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_connector_profile_active",
				Help: "Currently active profile for a connector (1=active)",
			},
			[]string{"connector", "profile"},
		),
		ProfileRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_connector_profile_requests_total",
				Help: "Total number of requests per profile",
			},
			[]string{"connector", "profile"},
		),
		ProfileErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_connector_profile_errors_total",
				Help: "Total number of errors per profile",
			},
			[]string{"connector", "profile", "error"},
		),
		ProfileFallback: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_connector_profile_fallback_total",
				Help: "Total number of fallback events between profiles",
			},
			[]string{"connector", "from", "to"},
		),
		ProfileLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_connector_profile_latency_seconds",
				Help:    "Latency per profile in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"connector", "profile"},
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
			[]string{"service", "version", "mycel_version"},
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
		r.LockAcquired,
		r.LockReleased,
		r.LockWaitSeconds,
		r.LockTimeout,
		r.LockHeld,
		r.SemaphoreAcquired,
		r.SemaphoreReleased,
		r.SemaphoreWaitSeconds,
		r.SemaphoreTimeout,
		r.SemaphoreAvailable,
		r.CoordinateSignal,
		r.CoordinateWait,
		r.CoordinateWaitSeconds,
		r.CoordinateTimeout,
		r.CoordinatePreflightHit,
		r.CoordinateActiveWaits,
		r.ScheduledFlows,
		r.ScheduleExecuted,
		r.ProfileActive,
		r.ProfileRequests,
		r.ProfileErrors,
		r.ProfileFallback,
		r.ProfileLatency,
		r.UptimeSeconds,
		r.GoRoutines,
		r.ServiceInfo,
	)

	// Set service info
	r.ServiceInfo.WithLabelValues(serviceName, version, mycelVersion).Set(1)

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

// RecordLockAcquired records a successful lock acquisition.
func (r *Registry) RecordLockAcquired(key string, waitDuration time.Duration) {
	r.LockAcquired.WithLabelValues(key).Inc()
	r.LockWaitSeconds.WithLabelValues(key).Observe(waitDuration.Seconds())
	r.LockHeld.WithLabelValues(key).Inc()
}

// RecordLockReleased records a lock release.
func (r *Registry) RecordLockReleased(key string) {
	r.LockReleased.WithLabelValues(key).Inc()
	r.LockHeld.WithLabelValues(key).Dec()
}

// RecordLockTimeout records a lock acquisition timeout.
func (r *Registry) RecordLockTimeout(key string, waitDuration time.Duration) {
	r.LockTimeout.WithLabelValues(key).Inc()
	r.LockWaitSeconds.WithLabelValues(key).Observe(waitDuration.Seconds())
}

// RecordSemaphoreAcquired records a successful semaphore permit acquisition.
func (r *Registry) RecordSemaphoreAcquired(key string, waitDuration time.Duration) {
	r.SemaphoreAcquired.WithLabelValues(key).Inc()
	r.SemaphoreWaitSeconds.WithLabelValues(key).Observe(waitDuration.Seconds())
}

// RecordSemaphoreReleased records a semaphore permit release.
func (r *Registry) RecordSemaphoreReleased(key string) {
	r.SemaphoreReleased.WithLabelValues(key).Inc()
}

// RecordSemaphoreTimeout records a semaphore acquisition timeout.
func (r *Registry) RecordSemaphoreTimeout(key string, waitDuration time.Duration) {
	r.SemaphoreTimeout.WithLabelValues(key).Inc()
	r.SemaphoreWaitSeconds.WithLabelValues(key).Observe(waitDuration.Seconds())
}

// SetSemaphoreAvailable sets the current available semaphore permits.
func (r *Registry) SetSemaphoreAvailable(key string, available int) {
	r.SemaphoreAvailable.WithLabelValues(key).Set(float64(available))
}

// RecordCoordinateSignal records a signal emission.
func (r *Registry) RecordCoordinateSignal(signal string) {
	r.CoordinateSignal.WithLabelValues(signal).Inc()
}

// RecordCoordinateWait records a wait initiation.
func (r *Registry) RecordCoordinateWait(signal string) {
	r.CoordinateWait.WithLabelValues(signal).Inc()
	r.CoordinateActiveWaits.WithLabelValues(signal).Inc()
}

// RecordCoordinateWaitComplete records a wait completion.
func (r *Registry) RecordCoordinateWaitComplete(signal string, waitDuration time.Duration, timedOut bool) {
	r.CoordinateWaitSeconds.WithLabelValues(signal).Observe(waitDuration.Seconds())
	r.CoordinateActiveWaits.WithLabelValues(signal).Dec()
	if timedOut {
		r.CoordinateTimeout.WithLabelValues(signal).Inc()
	}
}

// RecordCoordinatePreflightHit records a preflight check hit.
func (r *Registry) RecordCoordinatePreflightHit(connector string) {
	r.CoordinatePreflightHit.WithLabelValues(connector).Inc()
}

// SetScheduledFlows sets the current number of scheduled flows.
func (r *Registry) SetScheduledFlows(count int) {
	r.ScheduledFlows.WithLabelValues().Set(float64(count))
}

// RecordScheduleExecution records a scheduled flow execution.
func (r *Registry) RecordScheduleExecution(flow, status string) {
	r.ScheduleExecuted.WithLabelValues(flow, status).Inc()
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
		defaultRegistry = NewRegistry("mycel", "unknown", "unknown")
	})
	return defaultRegistry
}

// SetDefault sets the default metrics registry.
func SetDefault(r *Registry) {
	defaultRegistry = r
}
