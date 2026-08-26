import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('error_rate');
const httpErrors = new Rate('http_errors');
const totalRequests = new Counter('total_requests');

const BASE = __ENV.TARGET_URL || 'http://localhost:3000';
const headers = { 'Content-Type': 'application/json' };

// ---------------------------------------------------------------------------
// Adaptive VU scaling — reads MAX_VUS from calibration, defaults to 200
// ---------------------------------------------------------------------------

const MAX_VUS = parseInt(__ENV.MAX_VUS || '200', 10);
const IS_LOW_CAPACITY = MAX_VUS < 50;

// Scale VU counts proportionally to discovered hardware limits
const SEED_VUS       = Math.max(5, Math.min(20, Math.round(MAX_VUS * 0.2)));
const READ_MAX_VUS   = MAX_VUS;
const WRITE_MAX_VUS  = Math.round(MAX_VUS * 0.75);
const MIXED_MAX_VUS  = Math.round(MAX_VUS * 1.5);  // push beyond limit to find the edge
const ENDURANCE_VUS  = Math.round(MAX_VUS * 0.5);

// ---------------------------------------------------------------------------
// Scenarios — real CRUD with PostgreSQL (adaptive scaling)
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    // Phase 1: Seed the database (30s)
    seed: {
      executor: 'constant-vus',
      vus: SEED_VUS,
      duration: '30s',
      startTime: '0s',
      exec: 'createUser',
      tags: { phase: 'seed' },
    },

    // Phase 2: Read-heavy — 80% reads, 20% creates (1.5m, ramp to MAX_VUS)
    read_heavy: {
      executor: 'ramping-vus',
      startVUs: Math.max(5, Math.round(READ_MAX_VUS * 0.1)),
      stages: [
        { duration: '30s', target: Math.round(READ_MAX_VUS * 0.25) },
        { duration: '30s', target: Math.round(READ_MAX_VUS * 0.5) },
        { duration: '30s', target: READ_MAX_VUS },
      ],
      startTime: '30s',
      exec: 'readHeavy',
      tags: { phase: 'read_heavy' },
    },

    // Phase 3: Write-heavy — 70% creates, 30% reads (1m, ramp to WRITE_MAX_VUS)
    write_heavy: {
      executor: 'ramping-vus',
      startVUs: Math.max(5, Math.round(WRITE_MAX_VUS * 0.1)),
      stages: [
        { duration: '30s', target: Math.round(WRITE_MAX_VUS * 0.5) },
        { duration: '30s', target: WRITE_MAX_VUS },
      ],
      startTime: '120s',
      exec: 'writeHeavy',
      tags: { phase: 'write_heavy' },
    },

    // Phase 4: Mixed CRUD at peak concurrency (1m, ramp to MIXED_MAX_VUS)
    mixed_peak: {
      executor: 'ramping-vus',
      startVUs: Math.round(MIXED_MAX_VUS * 0.33),
      stages: [
        { duration: '20s', target: Math.round(MIXED_MAX_VUS * 0.66) },
        { duration: '20s', target: MIXED_MAX_VUS },
        { duration: '20s', target: Math.round(MIXED_MAX_VUS * 0.66) },
      ],
      startTime: '180s',
      exec: 'mixedCrud',
      tags: { phase: 'mixed_peak' },
    },

    // Phase 5: Endurance — steady load, check for leaks (1m)
    endurance: {
      executor: 'constant-vus',
      vus: ENDURANCE_VUS,
      duration: '60s',
      startTime: '240s',
      exec: 'mixedCrud',
      tags: { phase: 'endurance' },
    },
  },

  thresholds: IS_LOW_CAPACITY
    ? {
        // Lenient thresholds for low-capacity hardware
        http_req_duration: ['p(95)<1000', 'p(99)<2000'],
        error_rate: ['rate<0.03'],
      }
    : {
        // Standard thresholds
        http_req_duration: ['p(95)<500', 'p(99)<1000'],
        error_rate: ['rate<0.01'],
      },

  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// ---------------------------------------------------------------------------
// Request functions
// ---------------------------------------------------------------------------

let counter = 0;

export function createUser() {
  const i = __VU * 100000 + (counter++);
  const payload = JSON.stringify({
    name: `User ${__VU}-${i}`,
    email: `USER${i}@Example.COM`,
  });
  const res = http.post(`${BASE}/users`, payload, { headers });
  totalRequests.add(1);
  const ok = res.status >= 200 && res.status < 300;
  httpErrors.add(!ok);
  errorRate.add(!check(res, {
    'create: status 2xx': (r) => r.status >= 200 && r.status < 300,
    // A write that touched no row still answers 2xx.
    'create: a row was written': (r) => !!r.body && r.body.indexOf('"Adapter"') === -1 && r.body.length > 2,
  }));
}

export function readUsers() {
  const res = http.get(`${BASE}/users`);
  totalRequests.add(1);
  const ok = res.status === 200;
  httpErrors.add(!ok);
  errorRate.add(!check(res, {
    'read: status 200': (r) => r.status === 200,
    'read: rows came back': (r) => r.status === 200 && !!r.body && r.body.indexOf('"email"') !== -1,
  }));
}

export function readHeavy() {
  if (Math.random() < 0.8) {
    readUsers();
  } else {
    createUser();
  }
}

export function writeHeavy() {
  if (Math.random() < 0.3) {
    readUsers();
  } else {
    createUser();
  }
}

export function mixedCrud() {
  if (Math.random() < 0.5) {
    readUsers();
  } else {
    createUser();
  }
}

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

export function handleSummary(data) {
  const m = data.metrics;
  const dur = m.http_req_duration ? m.http_req_duration.values : {};

  const summary = {
    test: 'realistic',
    timestamp: new Date().toISOString(),
    target: BASE,
    max_vus_configured: MAX_VUS,
    total_requests: m.total_requests ? m.total_requests.values.count : 0,
    duration_seconds: data.state ? data.state.testRunDurationMs / 1000 : 0,
    rps_avg: m.http_reqs ? m.http_reqs.values.rate : 0,
    latency: {
      avg: dur.avg || 0,
      min: dur.min || 0,
      med: dur.med || 0,
      p90: dur['p(90)'] || 0,
      p95: dur['p(95)'] || 0,
      p99: dur['p(99)'] || 0,
      max: dur.max || 0,
    },
    error_rate: m.error_rate ? m.error_rate.values.rate : 0,
    http_errors: m.http_errors ? m.http_errors.values.rate : 0,
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
\u2554${'═'.repeat(62)}\u2557
\u2551           MYCEL REALISTIC BENCHMARK REPORT                  \u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  Target:          ${s.target.padEnd(40)}\u2551
\u2551  Timestamp:       ${s.timestamp.padEnd(40)}\u2551
\u2551  Calibrated VUs:  ${String(MAX_VUS).padEnd(40)}\u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  PHASES (adaptive scaling)                                  \u2551
\u2551    1. Seed database (creates only)      ${String(SEED_VUS + ' VUs').padEnd(20)}\u2551
\u2551    2. Read-heavy (80/20)               ramp to ${String(READ_MAX_VUS + ' VUs').padEnd(11)}\u2551
\u2551    3. Write-heavy (70/30)              ramp to ${String(WRITE_MAX_VUS + ' VUs').padEnd(11)}\u2551
\u2551    4. Mixed CRUD at peak               ramp to ${String(MIXED_MAX_VUS + ' VUs').padEnd(11)}\u2551
\u2551    5. Endurance (steady CRUD)          ${String(ENDURANCE_VUS + ' VUs').padEnd(20)}\u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  STORAGE: PostgreSQL (separate VPS, private network)        \u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  THROUGHPUT                                                 \u2551
\u2551    Total requests: ${String(s.total_requests || 0).padEnd(39)}\u2551
\u2551    Avg RPS:        ${String(fmt(s.rps_avg, 1)).padEnd(39)}\u2551
\u2551    Duration:       ${String(fmt(s.duration_seconds, 0) + 's').padEnd(39)}\u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  LATENCY                                                    \u2551
\u2551    Min:     ${String(fmt(s.latency.min, 2) + ' ms').padEnd(47)}\u2551
\u2551    Avg:     ${String(fmt(s.latency.avg, 2) + ' ms').padEnd(47)}\u2551
\u2551    Median:  ${String(fmt(s.latency.med, 2) + ' ms').padEnd(47)}\u2551
\u2551    p90:     ${String(fmt(s.latency.p90, 2) + ' ms').padEnd(47)}\u2551
\u2551    p95:     ${String(fmt(s.latency.p95, 2) + ' ms').padEnd(47)}\u2551
\u2551    p99:     ${String(fmt(s.latency.p99, 2) + ' ms').padEnd(47)}\u2551
\u2551    Max:     ${String(fmt(s.latency.max, 2) + ' ms').padEnd(47)}\u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  RELIABILITY                                                \u2551
\u2551    Error rate:  ${String(fmt((s.error_rate || 0) * 100, 3) + '%').padEnd(43)}\u2551
\u2551    HTTP errors: ${String(fmt((s.http_errors || 0) * 100, 3) + '%').padEnd(43)}\u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  VERDICT: ${getVerdict(s).padEnd(48)}\u2551
\u255A${'═'.repeat(62)}\u255D
`;
}

function getVerdict(s) {
  const httpErr = s.http_errors || 0;
  const err = s.error_rate || 0;
  const p99 = (s.latency && s.latency.p99) || 0;
  const rps = s.rps_avg || 0;
  if (httpErr > 0.05) return '❌ UNSTABLE — >5% HTTP errors';
  if (p99 > 2000) return '⚠️  DEGRADED — p99 > 2s';
  if (p99 > 500) return '⚠️  ACCEPTABLE — p95 > 500ms';
  if (rps > 3000) return '🚀 EXCELLENT — >3k RPS with DB';
  if (rps > 1000) return '✅ GOOD — >1k RPS with DB';
  if (rps > 500) return '✅ OK — >500 RPS with DB';
  return '✅ BASELINE';
}
