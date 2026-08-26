import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('error_rate');
const reqDuration = new Trend('req_duration', true);
const totalRequests = new Counter('total_requests');

// Target URL from environment
const BASE = __ENV.TARGET_URL || 'http://localhost:3000';
const SCENARIO = __ENV.SCENARIO || 'all';
// SMOKE trades the six-minute phase plan for a single short run. It exists so
// the suite itself can be exercised — before this there was no way to answer
// "does the harness still work" in less than six minutes.
const SMOKE = __ENV.SMOKE === 'true' || __ENV.SMOKE === '1';
const SMOKE_VUS = parseInt(__ENV.SMOKE_VUS || '10', 10);
const SMOKE_DURATION = __ENV.SMOKE_DURATION || '30s';

// Payload for POST endpoints
const payload = JSON.stringify({
  name: 'John Doe',
  email: 'JOHN.DOE@Example.COM',
});

const headers = { 'Content-Type': 'application/json' };

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

const smokeScenarios = {
  smoke: {
    executor: 'constant-vus',
    vus: SMOKE_VUS,
    duration: SMOKE_DURATION,
    exec: 'processRequest',
    tags: { phase: 'smoke' },
  },
};

const fullScenarios = {
    // Phase 1: Warmup (30s)
    warmup: {
      executor: 'constant-vus',
      vus: 10,
      duration: '30s',
      startTime: '0s',
      exec: 'processRequest',
      tags: { phase: 'warmup' },
    },

    // Phase 2: Ramp up to find ceiling (2m)
    ramp_up: {
      executor: 'ramping-vus',
      startVUs: 10,
      stages: [
        { duration: '30s', target: 50 },
        { duration: '30s', target: 100 },
        { duration: '30s', target: 200 },
        { duration: '30s', target: 500 },
      ],
      startTime: '30s',
      exec: 'processRequest',
      tags: { phase: 'ramp_up' },
    },

    // Phase 3: Sustained max load (1m)
    sustained: {
      executor: 'constant-vus',
      vus: 500,
      duration: '60s',
      startTime: '150s',
      exec: 'processRequest',
      tags: { phase: 'sustained' },
    },

    // Phase 4: Spike test — sudden burst (30s)
    spike: {
      executor: 'ramping-vus',
      startVUs: 500,
      stages: [
        { duration: '5s', target: 1000 },
        { duration: '20s', target: 1000 },
        { duration: '5s', target: 50 },
      ],
      startTime: '210s',
      exec: 'processRequest',
      tags: { phase: 'spike' },
    },

    // Phase 5: Endurance — steady load for 2m to check for memory leaks
    endurance: {
      executor: 'constant-vus',
      vus: 100,
      duration: '120s',
      startTime: '240s',
      exec: 'processRequest',
      tags: { phase: 'endurance' },
    },
};

export const options = {
  scenarios: SMOKE ? smokeScenarios : fullScenarios,

  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    error_rate: ['rate<0.01'],
  },

  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// ---------------------------------------------------------------------------
// Request functions
// ---------------------------------------------------------------------------

export function processRequest() {
  const scenario = SCENARIO === 'all' ? randomScenario() : SCENARIO;

  let res;
  switch (scenario) {
    case 'ping':
      res = http.get(`${BASE}/ping`);
      break;
    case 'echo':
      res = http.post(`${BASE}/echo`, payload, { headers });
      break;
    case 'process':
    default:
      res = http.post(`${BASE}/process`, payload, { headers });
      break;
  }

  totalRequests.add(1);
  reqDuration.add(res.timings.duration);

  // Checking only the status counts a 200 carrying an empty body — or a
  // transform whose result never made it into the JSON — as a success, and
  // folds it into the throughput number. Each endpoint asserts one marker
  // that could only be there if the flow actually ran.
  const success = check(res, {
    'status is 200': (r) => r.status === 200,
    'latency < 500ms': (r) => r.timings.duration < 500,
    'body carries the answer': (r) => bodyIsRight(scenario, r),
  });

  errorRate.add(!success);
}

// bodyIsRight looks for one marker per endpoint. Substring over JSON.parse on
// purpose: this runs on every request of the load test.
function bodyIsRight(scenario, r) {
  if (r.status !== 200 || !r.body) return false;
  switch (scenario) {
    case 'ping':
      return r.body.charAt(0) === '{';
    case 'echo':
      // The echo flow returns the body it was given.
      return r.body.indexOf('"name":"John Doe"') !== -1;
    default:
      // The process flow lowercases the address and mints an id.
      return r.body.indexOf('"email":"john.doe@example.com"') !== -1 &&
             r.body.indexOf('"id":"') !== -1;
  }
}

function randomScenario() {
  const r = Math.random();
  if (r < 0.5) return 'process';  // 50% — transform (heaviest)
  if (r < 0.8) return 'echo';     // 30% — echo (medium)
  return 'ping';                   // 20% — ping (lightest)
}

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

export function handleSummary(data) {
  const summary = {
    timestamp: new Date().toISOString(),
    target: BASE,
    total_requests: data.metrics.total_requests ? data.metrics.total_requests.values.count : 0,
    duration_seconds: data.state ? data.state.testRunDurationMs / 1000 : 0,
    rps_avg: data.metrics.http_reqs ? data.metrics.http_reqs.values.rate : 0,
    latency: {
      avg: data.metrics.http_req_duration ? data.metrics.http_req_duration.values.avg : 0,
      min: data.metrics.http_req_duration ? data.metrics.http_req_duration.values.min : 0,
      med: data.metrics.http_req_duration ? data.metrics.http_req_duration.values.med : 0,
      p90: data.metrics.http_req_duration ? data.metrics.http_req_duration.values['p(90)'] : 0,
      p95: data.metrics.http_req_duration ? data.metrics.http_req_duration.values['p(95)'] : 0,
      p99: data.metrics.http_req_duration ? data.metrics.http_req_duration.values['p(99)'] : 0,
      max: data.metrics.http_req_duration ? data.metrics.http_req_duration.values.max : 0,
    },
    error_rate: data.metrics.error_rate ? data.metrics.error_rate.values.rate : 0,
    http_errors: data.metrics.http_req_failed ? data.metrics.http_req_failed.values.rate : 0,
  };

  return {
    'results/summary.json': JSON.stringify(summary, null, 2),
    stdout: generateTextReport(summary),
  };
}

function fmt(val, decimals) {
  if (val === undefined || val === null) return 'N/A';
  return Number(val).toFixed(decimals);
}

function generateTextReport(s) {
  return `
╔══════════════════════════════════════════════════════════════╗
║                  MYCEL BENCHMARK REPORT                     ║
╠══════════════════════════════════════════════════════════════╣
║  Target:          ${s.target.padEnd(40)}║
║  Timestamp:       ${s.timestamp.padEnd(40)}║
╠══════════════════════════════════════════════════════════════╣
║  THROUGHPUT                                                 ║
║    Total requests: ${String(s.total_requests || 0).padEnd(39)}║
║    Avg RPS:        ${String(fmt(s.rps_avg, 1)).padEnd(39)}║
║    Duration:       ${String(fmt(s.duration_seconds, 0) + 's').padEnd(39)}║
╠══════════════════════════════════════════════════════════════╣
║  LATENCY                                                    ║
║    Min:     ${String(fmt(s.latency.min, 2) + ' ms').padEnd(47)}║
║    Avg:     ${String(fmt(s.latency.avg, 2) + ' ms').padEnd(47)}║
║    Median:  ${String(fmt(s.latency.med, 2) + ' ms').padEnd(47)}║
║    p90:     ${String(fmt(s.latency.p90, 2) + ' ms').padEnd(47)}║
║    p95:     ${String(fmt(s.latency.p95, 2) + ' ms').padEnd(47)}║
║    p99:     ${String(fmt(s.latency.p99, 2) + ' ms').padEnd(47)}║
║    Max:     ${String(fmt(s.latency.max, 2) + ' ms').padEnd(47)}║
╠══════════════════════════════════════════════════════════════╣
║  RELIABILITY                                                ║
║    Error rate:  ${String(fmt((s.error_rate || 0) * 100, 3) + '%').padEnd(43)}║
║    HTTP errors: ${String(fmt((s.http_errors || 0) * 100, 3) + '%').padEnd(43)}║
╠══════════════════════════════════════════════════════════════╣
║  VERDICT: ${getVerdict(s).padEnd(48)}║
╚══════════════════════════════════════════════════════════════╝
`;
}

function getVerdict(s) {
  const err = s.error_rate || 0;
  const p99 = (s.latency && s.latency.p99) || 0;
  const p95 = (s.latency && s.latency.p95) || 0;
  const rps = s.rps_avg || 0;
  if (err > 0.05) return '❌ UNSTABLE — >5% error rate';
  if (p99 > 2000) return '⚠️  DEGRADED — p99 > 2s';
  if (p95 > 500) return '⚠️  ACCEPTABLE — p95 > 500ms';
  if (rps > 5000) return '🚀 EXCELLENT — >5k RPS';
  if (rps > 1000) return '✅ GOOD — >1k RPS';
  return '✅ OK';
}
