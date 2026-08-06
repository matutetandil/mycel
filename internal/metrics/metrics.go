// Package metrics provides Prometheus metrics for Mycel services.
package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Default metrics registry
	defaultRegistry *Registry
	defaultMu       sync.Mutex
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

	// Message queue metrics
	MessagesUndispatched *prometheus.CounterVec

	// Flow metrics
	FlowExecutions *prometheus.CounterVec
	FlowDuration   *prometheus.HistogramVec
	FlowErrors     *prometheus.CounterVec

	FlowDrops *prometheus.CounterVec

	// FlowStats derives fastest/slowest/average/throughput over successful
	// executions only — see flowstats.go for why the histogram cannot.
	FlowStats *FlowStats

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
	CoordinateSignal       *prometheus.CounterVec
	CoordinateWait         *prometheus.CounterVec
	CoordinateWaitSeconds  *prometheus.HistogramVec
	CoordinateTimeout      *prometheus.CounterVec
	CoordinatePreflightHit *prometheus.CounterVec
	CoordinateActiveWaits  *prometheus.GaugeVec

	// Scheduler metrics
	ScheduledFlows   *prometheus.GaugeVec
	ScheduleExecuted *prometheus.CounterVec

	// Profile metrics
	ProfileActive   *prometheus.GaugeVec
	ProfileRequests *prometheus.CounterVec
	ProfileErrors   *prometheus.CounterVec
	ProfileFallback *prometheus.CounterVec
	ProfileLatency  *prometheus.HistogramVec

	// Runtime metrics
	UptimeSeconds *prometheus.GaugeVec
	GoRoutines    prometheus.Gauge

	// Service info
	ServiceInfo *prometheus.GaugeVec
}

// NewRegistry creates a new metrics registry with all Mycel metrics.
func NewRegistry(serviceName, version, mycelVersion, environment string) *Registry {
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

		// Message queue metrics.
		//
		// target is where the message arrived (queue, topic, channel) and key
		// is what was matched against the handler patterns — the routing key
		// on RabbitMQ, the topic on Kafka, the channel on Redis. key is a
		// label because "which key is being dropped" is the whole diagnostic
		// value here; cardinality is bounded in practice, since a consumer
		// only sees keys it is subscribed to.
		MessagesUndispatched: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_messages_undispatched_total",
				Help: "Messages delivered to a consumer that no flow handler matched (dropped)",
			},
			[]string{"connector", "driver", "target", "key"},
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
		// Deliberate drops, by the gate that declined the message. The
		// matching log line (reason + decided_by + detail) says why one
		// message was dropped; this says how many, and is what you graph
		// and alert on. reason is a closed set, so cardinality is bounded.
		FlowDrops: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_flow_drops_total",
				Help: "Messages a flow deliberately declined to process, by gate",
			},
			[]string{"flow", "reason"},
		),
		FlowStats: NewFlowStats(),

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
			[]string{"flow", "purpose"},
		),
		LockReleased: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_lock_released_total",
				Help: "Total number of locks released",
			},
			[]string{"flow", "purpose"},
		),
		LockWaitSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_lock_wait_seconds",
				Help:    "Time spent waiting to acquire a lock",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"flow", "purpose"},
		),
		LockTimeout: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_lock_timeout_total",
				Help: "Total number of lock acquisition timeouts",
			},
			[]string{"flow", "purpose"},
		),
		LockHeld: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_lock_held",
				Help: "Current number of held locks",
			},
			[]string{"flow", "purpose"},
		),

		// Semaphore metrics
		SemaphoreAcquired: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_semaphore_acquired_total",
				Help: "Total number of semaphore permits acquired",
			},
			[]string{"flow"},
		),
		SemaphoreReleased: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_semaphore_released_total",
				Help: "Total number of semaphore permits released",
			},
			[]string{"flow"},
		),
		SemaphoreWaitSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_semaphore_wait_seconds",
				Help:    "Time spent waiting to acquire a semaphore permit",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"flow"},
		),
		SemaphoreTimeout: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_semaphore_timeout_total",
				Help: "Total number of semaphore acquisition timeouts",
			},
			[]string{"flow"},
		),
		SemaphoreAvailable: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mycel_semaphore_available",
				Help: "Current number of available semaphore permits",
			},
			[]string{"flow"},
		),

		// Coordinate metrics
		CoordinateSignal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_signal_total",
				Help: "Total number of signals emitted",
			},
			[]string{"flow"},
		),
		CoordinateWait: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_wait_total",
				Help: "Total number of waits started",
			},
			[]string{"flow"},
		),
		CoordinateWaitSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mycel_coordinate_wait_seconds",
				Help:    "Time spent waiting for a signal",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"flow"},
		),
		CoordinateTimeout: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mycel_coordinate_timeout_total",
				Help: "Total number of coordinate wait timeouts",
			},
			[]string{"flow"},
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
			[]string{"flow"},
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
			[]string{"service", "version", "mycel_version", "environment"},
		),
	}

	// Register Go runtime and process collectors so the endpoint exposes
	// standard go_* and process_* series (memory, goroutines, GC, FDs, CPU).
	// Mycel uses a custom registry, which — unlike the global default — does
	// not include these collectors automatically.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// Register all metrics
	reg.MustRegister(
		r.RequestsTotal,
		r.RequestDuration,
		r.RequestsInFlight,
		r.ConnectorHealth,
		r.ConnectorOperations,
		r.ConnectorLatency,
		r.MessagesUndispatched,
		r.FlowDrops,
		r.FlowStats,
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
	r.ServiceInfo.WithLabelValues(serviceName, version, mycelVersion, environment).Set(1)

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

// RecordUndispatchedMessage records a message that reached a consumer but
// matched no flow handler, and was therefore dropped.
func (r *Registry) RecordUndispatchedMessage(connector, driver, target, key string) {
	r.MessagesUndispatched.WithLabelValues(connector, driver, target, key).Inc()
}

// Flow execution statuses.
const (
	FlowStatusSuccess = "success"
	FlowStatusError   = "error"
	// FlowStatusDropped is a message a gate declined to process — filtered,
	// deduplicated, superseded. Not an error: the flow did exactly what it was
	// configured to do. But not work either, which is why it is neither.
	FlowStatusDropped = "dropped"
)

// RecordFlowExecution records a flow execution.
func (r *Registry) RecordFlowExecution(flow, status string, duration time.Duration) {
	r.FlowExecutions.WithLabelValues(flow, status).Inc()
	r.FlowDuration.WithLabelValues(flow).Observe(duration.Seconds())

	// Fastest/slowest/average/throughput describe work actually done. A flow
	// that fails fast, or drops a message before the transform, would
	// otherwise own the "fastest" gauge permanently and pull the average
	// down — a consumer filtering out 90% of its input would look quick while
	// doing almost nothing.
	if status == FlowStatusSuccess {
		r.FlowStats.Observe(flow, duration)
	}
}

// RecordFlowDrop records a message declined by a gate. reason matches the
// `reason` field on the corresponding log line.
func (r *Registry) RecordFlowDrop(flow, reason string) {
	r.FlowDrops.WithLabelValues(flow, reason).Inc()
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

// Sync metrics are labelled by flow, not by the lock/semaphore/signal key.
// Those keys are evaluated per message — one per order, per SKU, per customer —
// so using them as a label would grow the time series set without bound and
// take Prometheus down with it. The flow is the bounded dimension, and it is
// also the one that answers the operational question: which flow is contending.

// RecordLockAcquired records a successful lock acquisition.
//
// purpose separates the two places a lock is taken: "flow" for the flow's own
// lock {} block, guarding a business key, and "dedupe" for the critical
// section around the duplicate check. Contention means different things in
// each — a hot business key versus duplicate deliveries piling up — so they
// need to be distinguishable without reading it out of the flow name.
func (r *Registry) RecordLockAcquired(flow, purpose string, waitDuration time.Duration) {
	r.LockAcquired.WithLabelValues(flow, purpose).Inc()
	r.LockWaitSeconds.WithLabelValues(flow, purpose).Observe(waitDuration.Seconds())
	r.LockHeld.WithLabelValues(flow, purpose).Inc()
}

// RecordLockReleased records a lock release.
func (r *Registry) RecordLockReleased(flow, purpose string) {
	r.LockReleased.WithLabelValues(flow, purpose).Inc()
	r.LockHeld.WithLabelValues(flow, purpose).Dec()
}

// RecordLockTimeout records a lock acquisition timeout.
func (r *Registry) RecordLockTimeout(flow, purpose string, waitDuration time.Duration) {
	r.LockTimeout.WithLabelValues(flow, purpose).Inc()
	r.LockWaitSeconds.WithLabelValues(flow, purpose).Observe(waitDuration.Seconds())
}

// RecordSemaphoreAcquired records a successful semaphore permit acquisition.
func (r *Registry) RecordSemaphoreAcquired(flow string, waitDuration time.Duration) {
	r.SemaphoreAcquired.WithLabelValues(flow).Inc()
	r.SemaphoreWaitSeconds.WithLabelValues(flow).Observe(waitDuration.Seconds())
}

// RecordSemaphoreReleased records a semaphore permit release.
func (r *Registry) RecordSemaphoreReleased(flow string) {
	r.SemaphoreReleased.WithLabelValues(flow).Inc()
}

// RecordSemaphoreTimeout records a semaphore acquisition timeout.
func (r *Registry) RecordSemaphoreTimeout(flow string, waitDuration time.Duration) {
	r.SemaphoreTimeout.WithLabelValues(flow).Inc()
	r.SemaphoreWaitSeconds.WithLabelValues(flow).Observe(waitDuration.Seconds())
}

// SetSemaphoreAvailable sets the current available semaphore permits.
func (r *Registry) SetSemaphoreAvailable(flow string, available int) {
	r.SemaphoreAvailable.WithLabelValues(flow).Set(float64(available))
}

// RecordCoordinateSignal records a signal emission.
func (r *Registry) RecordCoordinateSignal(flow string) {
	r.CoordinateSignal.WithLabelValues(flow).Inc()
}

// RecordCoordinateWait records a wait initiation.
func (r *Registry) RecordCoordinateWait(flow string) {
	r.CoordinateWait.WithLabelValues(flow).Inc()
	r.CoordinateActiveWaits.WithLabelValues(flow).Inc()
}

// RecordCoordinateWaitComplete records a wait completion.
func (r *Registry) RecordCoordinateWaitComplete(flow string, waitDuration time.Duration, timedOut bool) {
	r.CoordinateWaitSeconds.WithLabelValues(flow).Observe(waitDuration.Seconds())
	r.CoordinateActiveWaits.WithLabelValues(flow).Dec()
	if timedOut {
		r.CoordinateTimeout.WithLabelValues(flow).Inc()
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

// Default returns the default metrics registry, lazily creating a fallback one
// only if SetDefault was never called. Recorders (RecordFlowExecution, etc.)
// route through here, so it MUST return the same registry the runtime serves.
//
// Previously this used a sync.Once to lazy-init, which clobbered the registry
// SetDefault had assigned: the first Default() call fired the Once and replaced
// the runtime's registry with a throwaway "unknown" one that nothing serves —
// so flow/connector metrics were recorded but never exposed at /metrics.
func Default() *Registry {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRegistry == nil {
		defaultRegistry = NewRegistry("mycel", "unknown", "unknown", "unknown")
	}
	return defaultRegistry
}

// SetDefault sets the default metrics registry. The runtime calls this during
// startup so every recorder writes into the same registry the admin/REST
// endpoint serves.
func SetDefault(r *Registry) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRegistry = r
}
