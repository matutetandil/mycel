package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// throughputWindow is the trailing window messages-per-second is measured over.
// One minute is long enough to ride out a quiet second on a low-volume consumer
// and short enough to still react within a scrape interval.
const throughputWindow = 60

// FlowStats tracks per-flow timing extremes and throughput over successful
// executions only.
//
// Most of this is already derivable from mycel_flow_duration_seconds:
// rate(_count) is the throughput and rate(_sum)/rate(_count) is the average.
// Two things are not. A histogram cannot give the exact fastest and slowest
// execution — only the buckets they fell into — and the existing histogram is
// not split by status, so none of it can be narrowed to messages that actually
// succeeded. A failure that returns in 2ms would otherwise flatter the average
// and take the "fastest" spot.
//
// These are exported as a Collector that computes on scrape, so there is no
// background goroutine and no staleness between the numbers and the counters
// they are derived from.
type FlowStats struct {
	mu    sync.Mutex
	flows map[string]*flowStat
	start time.Time

	fastest *prometheus.Desc
	slowest *prometheus.Desc
	average *prometheus.Desc
	rate    *prometheus.Desc
}

type flowStat struct {
	min   float64
	max   float64
	sum   float64
	count uint64

	// buckets counts executions per second over a trailing window, as a ring
	// indexed by unix second. Fixed size per flow, so a long-running service
	// does not accumulate memory the way a list of samples would.
	buckets [throughputWindow]uint32
	lastSec int64
}

// NewFlowStats creates the collector.
func NewFlowStats() *FlowStats {
	return &FlowStats{
		flows: make(map[string]*flowStat),
		start: time.Now(),
		fastest: prometheus.NewDesc(
			"mycel_flow_duration_fastest_seconds",
			"Fastest successful flow execution since start",
			[]string{"flow"}, nil,
		),
		slowest: prometheus.NewDesc(
			"mycel_flow_duration_slowest_seconds",
			"Slowest successful flow execution since start",
			[]string{"flow"}, nil,
		),
		average: prometheus.NewDesc(
			"mycel_flow_duration_average_seconds",
			"Mean duration of successful flow executions since start",
			[]string{"flow"}, nil,
		),
		rate: prometheus.NewDesc(
			"mycel_flow_messages_per_second",
			"Successful flow executions per second over the last minute",
			[]string{"flow"}, nil,
		),
	}
}

// Observe records one successful execution.
func (f *FlowStats) Observe(flow string, d time.Duration) {
	secs := d.Seconds()
	now := time.Now().Unix()

	f.mu.Lock()
	defer f.mu.Unlock()

	st, ok := f.flows[flow]
	if !ok {
		st = &flowStat{min: secs, max: secs, lastSec: now}
		f.flows[flow] = st
	}

	if secs < st.min {
		st.min = secs
	}
	if secs > st.max {
		st.max = secs
	}
	st.sum += secs
	st.count++

	st.advance(now)
	st.buckets[now%throughputWindow]++
}

// advance clears the ring slots for seconds that elapsed since the last write,
// so a flow that goes quiet decays to zero instead of reporting whatever it was
// doing a minute ago. Callers hold f.mu.
func (s *flowStat) advance(now int64) {
	if now <= s.lastSec {
		return
	}
	// More than a full window of silence clears everything; walking each slot
	// would be pointless work.
	if now-s.lastSec >= throughputWindow {
		s.buckets = [throughputWindow]uint32{}
		s.lastSec = now
		return
	}
	for sec := s.lastSec + 1; sec <= now; sec++ {
		s.buckets[sec%throughputWindow] = 0
	}
	s.lastSec = now
}

// Describe implements prometheus.Collector.
func (f *FlowStats) Describe(ch chan<- *prometheus.Desc) {
	ch <- f.fastest
	ch <- f.slowest
	ch <- f.average
	ch <- f.rate
}

// Collect implements prometheus.Collector.
func (f *FlowStats) Collect(ch chan<- prometheus.Metric) {
	now := time.Now().Unix()

	f.mu.Lock()
	defer f.mu.Unlock()

	// A window's worth of history does not exist yet in the first minute, so
	// divide by the time actually elapsed. Dividing by a full 60 would report
	// a third of the real rate to anyone watching a service come up.
	elapsed := time.Since(f.start).Seconds()
	divisor := float64(throughputWindow)
	if elapsed < divisor {
		divisor = elapsed
	}
	if divisor < 1 {
		divisor = 1
	}

	for name, st := range f.flows {
		if st.count == 0 {
			continue
		}

		st.advance(now)
		var recent uint64
		for _, n := range st.buckets {
			recent += uint64(n)
		}

		ch <- prometheus.MustNewConstMetric(f.fastest, prometheus.GaugeValue, st.min, name)
		ch <- prometheus.MustNewConstMetric(f.slowest, prometheus.GaugeValue, st.max, name)
		ch <- prometheus.MustNewConstMetric(f.average, prometheus.GaugeValue, st.sum/float64(st.count), name)
		ch <- prometheus.MustNewConstMetric(f.rate, prometheus.GaugeValue, float64(recent)/divisor, name)
	}
}
