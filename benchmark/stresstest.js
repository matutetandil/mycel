import http from 'k6/http';
import { check } from 'k6';
import { Rate, Counter } from 'k6/metrics';

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

// Scale stress scenarios proportionally to discovered limits.
// The point of stress testing is to go BEYOND the limit, so we scale
// the "normal" phases to MAX_VUS range and push storm/chaos phases
// to 2-3x beyond.
const WARMUP_VUS         = Math.max(10, Math.round(MAX_VUS * 0.1));
const ARRAY_MAX_VUS      = MAX_VUS;
const LARGE_MAX_VUS      = Math.round(MAX_VUS * 0.5);
const DB_STORM_MAX_VUS   = Math.round(MAX_VUS * 1.5);
const CONCURRENCY_MAX    = Math.round(MAX_VUS * 3);     // 3x — intentionally beyond limit
const CHAOS_VUS          = Math.round(MAX_VUS * 1.5);
const RECOVERY_VUS       = Math.max(5, Math.round(MAX_VUS * 0.05));

// ---------------------------------------------------------------------------
// Payloads
// ---------------------------------------------------------------------------

// Small payload (~100 bytes) -- same as standard benchmark
const smallPayload = JSON.stringify({
  name: 'John Doe',
  email: 'JOHN.DOE@Example.COM',
});

// Medium payload (~10KB) -- realistic API body
function buildMediumPayload() {
  const items = [];
  for (let i = 0; i < 100; i++) {
    items.push({
      name: `Product ${i}`,
      price: Math.round(Math.random() * 10000) / 100,
      category: ['electronics', 'books', 'clothing', 'food'][i % 4],
      sku: `SKU-${String(i).padStart(6, '0')}`,
    });
  }
  return JSON.stringify({ items });
}

// Large payload (~100KB) -- stress body parsing + serialization
function buildLargePayload() {
  const items = [];
  for (let i = 0; i < 1000; i++) {
    items.push({
      name: `Product ${i} with a longer description to increase payload size`,
      price: Math.round(Math.random() * 10000) / 100,
      category: ['electronics', 'books', 'clothing', 'food', 'sports'][i % 5],
      sku: `SKU-${String(i).padStart(6, '0')}`,
      tags: ['sale', 'new', 'featured', 'clearance'].slice(0, (i % 4) + 1),
      metadata: { weight: i * 0.5, color: ['red', 'blue', 'green'][i % 3] },
    });
  }
  return JSON.stringify({ items });
}

// Pre-build payloads (k6 init context)
const mediumPayload = buildMediumPayload();
const largePayload = buildLargePayload();

// ---------------------------------------------------------------------------
// Scenarios -- designed to find the breaking point (adaptive scaling)
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    // Phase 1: Warmup with heavy transforms (30s)
    warmup_heavy: {
      executor: 'constant-vus',
      vus: WARMUP_VUS,
      duration: '30s',
      startTime: '0s',
      exec: 'heavyTransform',
      tags: { phase: 'warmup_heavy' },
    },

    // Phase 2: Array processing ramp (1m) -- 100-item arrays with aggregation
    array_ramp: {
      executor: 'ramping-vus',
      startVUs: Math.max(5, Math.round(ARRAY_MAX_VUS * 0.1)),
      stages: [
        { duration: '20s', target: Math.round(ARRAY_MAX_VUS * 0.25) },
        { duration: '20s', target: Math.round(ARRAY_MAX_VUS * 0.5) },
        { duration: '20s', target: ARRAY_MAX_VUS },
      ],
      startTime: '30s',
      exec: 'arrayProcessing',
      tags: { phase: 'array_ramp' },
    },

    // Phase 3: Large payload echo (1m) -- 100KB bodies, test parsing/serialization
    large_payload: {
      executor: 'ramping-vus',
      startVUs: Math.max(5, Math.round(LARGE_MAX_VUS * 0.1)),
      stages: [
        { duration: '20s', target: Math.round(LARGE_MAX_VUS * 0.3) },
        { duration: '20s', target: Math.round(LARGE_MAX_VUS * 0.6) },
        { duration: '20s', target: LARGE_MAX_VUS },
      ],
      startTime: '90s',
      exec: 'largeEcho',
      tags: { phase: 'large_payload' },
    },

    // Phase 4: Database storm (1m) -- concurrent PostgreSQL CRUD under pressure
    db_storm: {
      executor: 'ramping-vus',
      startVUs: Math.max(5, Math.round(DB_STORM_MAX_VUS * 0.05)),
      stages: [
        { duration: '15s', target: Math.round(DB_STORM_MAX_VUS * 0.33) },
        { duration: '15s', target: Math.round(DB_STORM_MAX_VUS * 0.66) },
        { duration: '15s', target: DB_STORM_MAX_VUS },
        { duration: '15s', target: DB_STORM_MAX_VUS },
      ],
      startTime: '150s',
      exec: 'dbCrud',
      tags: { phase: 'db_storm' },
    },

    // Phase 5: Concurrency storm (1m) -- ramp to 3x MAX_VUS with heavy transforms
    concurrency_storm: {
      executor: 'ramping-vus',
      startVUs: Math.round(CONCURRENCY_MAX * 0.08),
      stages: [
        { duration: '15s', target: Math.round(CONCURRENCY_MAX * 0.25) },
        { duration: '15s', target: Math.round(CONCURRENCY_MAX * 0.5) },
        { duration: '15s', target: CONCURRENCY_MAX },
        { duration: '15s', target: CONCURRENCY_MAX },
      ],
      startTime: '210s',
      exec: 'heavyTransform',
      tags: { phase: 'concurrency_storm' },
    },

    // Phase 6: Everything at once (1m) -- mixed heavy workload including DB
    chaos: {
      executor: 'constant-vus',
      vus: CHAOS_VUS,
      duration: '60s',
      startTime: '270s',
      exec: 'chaosRequest',
      tags: { phase: 'chaos' },
    },

    // Phase 7: Recovery check (30s) -- light load after storm, check it recovers
    recovery: {
      executor: 'constant-vus',
      vus: RECOVERY_VUS,
      duration: '30s',
      startTime: '330s',
      exec: 'heavyTransform',
      tags: { phase: 'recovery' },
    },
  },

  thresholds: {
    http_req_duration: ['p(95)<2000'],  // relaxed -- stress test
    http_errors: ['rate<0.10'],          // allow up to 10% HTTP errors under extreme load
  },

  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// ---------------------------------------------------------------------------
// Request functions
// ---------------------------------------------------------------------------

// Each endpoint asserts one marker that could only be there if the flow ran.
// Status alone counted a 200 carrying an empty body — or a transform whose
// result never reached the JSON — as a success. Substring over JSON.parse on
// purpose: this runs on every request of a stress test.
function carriesTheAnswer(r) {
  return r.status === 200 && !!r.body && r.body.indexOf('"Adapter"') === -1;
}

// Heavy transforms -- 12 CEL expressions per request
export function heavyTransform() {
  const res = http.post(`${BASE}/heavy`, smallPayload, { headers });
  totalRequests.add(1);
  const ok = res.status === 200;
  httpErrors.add(!ok);
  errorRate.add(!check(res, {
    'heavy: status 200': (r) => r.status === 200,
    // The slug is the last of the twelve expressions to be built.
    'heavy: transforms ran': (r) => carriesTheAnswer(r) && r.body.indexOf('"slug":"') !== -1,
  }));
}

// Array processing -- 100 items with filter/sort/sum/pluck
export function arrayProcessing() {
  const res = http.post(`${BASE}/array`, mediumPayload, { headers });
  totalRequests.add(1);
  const ok = res.status === 200;
  httpErrors.add(!ok);
  errorRate.add(!check(res, {
    'array: status 200': (r) => r.status === 200,
    'array: aggregates ran': (r) => carriesTheAnswer(r) && r.body.indexOf('"count":100') !== -1,
  }));
}

// Large payload echo -- 100KB body, test parse + serialize overhead
export function largeEcho() {
  const res = http.post(`${BASE}/echo`, largePayload, { headers });
  totalRequests.add(1);
  const ok = res.status === 200;
  httpErrors.add(!ok);
  errorRate.add(!check(res, {
    'large echo: status 200': (r) => r.status === 200,
    // An echo has to give the body back, not an empty envelope.
    'large echo: body came back': (r) => carriesTheAnswer(r) && r.body.length > 100000,
  }));
}

// Database CRUD -- concurrent reads and writes
let dbCounter = 0;
export function dbCrud() {
  const r = Math.random();
  if (r < 0.5) {
    // Read
    const res = http.get(`${BASE}/users`);
    totalRequests.add(1);
    httpErrors.add(res.status !== 200);
    errorRate.add(!check(res, {
      'db read: status 200': (r) => r.status === 200,
      'db read: rows came back': (r) => carriesTheAnswer(r) && r.body.indexOf('"email"') !== -1,
    }));
  } else {
    // Write
    const i = __VU * 100000 + (dbCounter++);
    const payload = JSON.stringify({
      name: `Stress User ${__VU}-${i}`,
      email: `STRESS${i}@Example.COM`,
    });
    const res = http.post(`${BASE}/users`, payload, { headers });
    totalRequests.add(1);
    const ok = res.status >= 200 && res.status < 300;
    httpErrors.add(!ok);
    errorRate.add(!check(res, {
      'db write: status 2xx': (r) => r.status >= 200 && r.status < 300,
    }));
  }
}

// Chaos -- random mix of everything including DB
export function chaosRequest() {
  const r = Math.random();
  if (r < 0.25) return heavyTransform();
  if (r < 0.45) return arrayProcessing();
  if (r < 0.60) return largeEcho();
  if (r < 0.80) return dbCrud();
  // 20% -- medium payload with array processing
  const res = http.post(`${BASE}/array`, mediumPayload, { headers });
  totalRequests.add(1);
  httpErrors.add(res.status !== 200);
  errorRate.add(!check(res, {
    'chaos: status 200': (r) => r.status === 200,
    'chaos: aggregates ran': (r) => carriesTheAnswer(r) && r.body.indexOf('"count":100') !== -1,
  }));
}

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

export function handleSummary(data) {
  const m = data.metrics;
  const dur = m.http_req_duration ? m.http_req_duration.values : {};

  const summary = {
    test: 'stress',
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
\u2551              MYCEL STRESS TEST REPORT                       \u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  Target:          ${s.target.padEnd(40)}\u2551
\u2551  Timestamp:       ${s.timestamp.padEnd(40)}\u2551
\u2551  Calibrated VUs:  ${String(MAX_VUS).padEnd(40)}\u2551
\u2560${'═'.repeat(62)}\u2563
\u2551  PHASES (adaptive scaling)                                  \u2551
\u2551    1. Heavy transforms (12 CEL/req)     ${String(WARMUP_VUS + ' VUs').padEnd(20)}\u2551
\u2551    2. Array processing (100 items)      ramp to ${String(ARRAY_MAX_VUS + ' VUs').padEnd(11)}\u2551
\u2551    3. Large payload echo (100KB)        ramp to ${String(LARGE_MAX_VUS + ' VUs').padEnd(11)}\u2551
\u2551    4. Database storm (CRUD)             ramp to ${String(DB_STORM_MAX_VUS + ' VUs').padEnd(11)}\u2551
\u2551    5. Concurrency storm                 ramp to ${String(CONCURRENCY_MAX + ' VUs').padEnd(11)}\u2551
\u2551    6. Chaos (all above + DB)            ${String(CHAOS_VUS + ' VUs').padEnd(20)}\u2551
\u2551    7. Recovery check                    ${String(RECOVERY_VUS + ' VUs').padEnd(20)}\u2551
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
  if (httpErr > 0.10) return '💀 CRASHED — >10% HTTP errors';
  if (httpErr > 0.05) return '❌ UNSTABLE — >5% HTTP errors';
  if (httpErr > 0.01) return '⚠️  STRESSED — >1% HTTP errors';
  if (p99 > 5000) return '⚠️  DEGRADED — p99 > 5s';
  if (err < 0.01) return '🏆 UNBREAKABLE — survived stress test';
  return '✅ SURVIVED — with some check failures';
}
