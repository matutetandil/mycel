import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// ---------------------------------------------------------------------------
// Adaptive Hardware Calibration
//
// Discovers the maximum sustainable VU count for the target hardware by
// stepping through increasing VU levels and measuring error rate + latency.
// Outputs calibration.json for use by subsequent benchmark scripts.
// ---------------------------------------------------------------------------

const BASE = __ENV.TARGET_URL || 'http://localhost:3000';
const headers = { 'Content-Type': 'application/json' };

// Custom metrics with step tags for per-step analysis
const errorRate = new Rate('error_rate');
const httpErrors = new Rate('http_errors');
const totalRequests = new Counter('total_requests');

// Per-step tracking (k6 tags enable per-group metric filtering in handleSummary)
const stepLatency = new Trend('step_latency', true);

// VU steps to test — each holds for 15 seconds
const STEPS = [10, 25, 50, 75, 100, 150, 200, 300];
const STEP_DURATION = '15s';

// Build ramping stages: each step ramps quickly (2s) then holds
function buildStages() {
  const stages = [];
  for (const vus of STEPS) {
    stages.push({ duration: '2s', target: vus });   // ramp to this level
    stages.push({ duration: STEP_DURATION, target: vus }); // hold
  }
  return stages;
}

// Build thresholds that register sub-metrics per step tag.
// k6 only exposes tagged sub-metrics in handleSummary if there is at least
// one threshold defined for them. We use a permissive threshold (always passes)
// just to ensure the data is available for analysis.
function buildThresholds() {
  const t = {};
  for (const vus of STEPS) {
    const tag = String(vus);
    t[`step_latency{step:${tag}}`] = ['p(95)<60000'];     // always passes
    t[`http_errors{step:${tag}}`] = ['rate<1.0'];          // always passes
    t[`http_reqs{step:${tag}}`] = ['count>=0'];            // always passes
  }
  return t;
}

export const options = {
  scenarios: {
    calibration: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: buildStages(),
      exec: 'calibrationRequest',
    },
  },

  // Permissive thresholds per step — needed to expose sub-metrics in handleSummary
  thresholds: buildThresholds(),

  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// ---------------------------------------------------------------------------
// Request function — mix of endpoint types to simulate real workload
// ---------------------------------------------------------------------------

let counter = 0;

export function calibrationRequest() {
  // Determine which VU step we are in based on current VU count
  const currentVUs = __VU;
  const stepTag = findStep(currentVUs);

  const r = Math.random();
  let res;

  if (r < 0.3) {
    // 30% — Echo (lightweight, tests basic throughput)
    res = http.get(`${BASE}/ping`, { tags: { step: stepTag } });
  } else if (r < 0.6) {
    // 30% — Transform (CPU-bound)
    const payload = JSON.stringify({
      name: 'Calibration User',
      email: 'CALIBRATE@Example.COM',
    });
    res = http.post(`${BASE}/process`, payload, { headers, tags: { step: stepTag } });
  } else if (r < 0.8) {
    // 20% — Create user (DB write)
    const i = __VU * 100000 + (counter++);
    const payload = JSON.stringify({
      name: `Cal User ${__VU}-${i}`,
      email: `CAL${i}@Example.COM`,
    });
    res = http.post(`${BASE}/users`, payload, { headers, tags: { step: stepTag } });
  } else {
    // 20% — Read users (DB read)
    res = http.get(`${BASE}/users`, { tags: { step: stepTag } });
  }

  totalRequests.add(1);
  stepLatency.add(res.timings.duration, { step: stepTag });

  const ok = res.status >= 200 && res.status < 300;
  httpErrors.add(!ok, { step: stepTag });
  errorRate.add(!check(res, {
    'status 2xx': (r) => r.status >= 200 && r.status < 300,
  }), { step: stepTag });
}

// Map current VU number to the closest step label
function findStep(vuCount) {
  for (let i = STEPS.length - 1; i >= 0; i--) {
    if (vuCount >= STEPS[i] * 0.8) return String(STEPS[i]);
  }
  return String(STEPS[0]);
}

// ---------------------------------------------------------------------------
// Summary — analyze results and find the sustainable limit
// ---------------------------------------------------------------------------

export function handleSummary(data) {
  const m = data.metrics;

  // Extract per-step metrics from tagged data
  const stepResults = [];

  for (const vus of STEPS) {
    const tag = String(vus);
    const stepData = extractStepMetrics(data, tag);
    stepResults.push({
      vus: vus,
      rps: stepData.rps,
      p95: stepData.p95,
      p99: stepData.p99,
      avg_latency: stepData.avg,
      error_rate: stepData.errorRate,
      requests: stepData.requests,
    });
  }

  // Find the highest VU level that meets our criteria:
  // - error rate < 1%
  // - p95 latency < 500ms
  let maxVUs = STEPS[0];  // minimum fallback
  let maxRPS = 0;
  let bottleneck = 'none';
  let bottleneckVUs = 0;

  for (const step of stepResults) {
    if (step.requests < 5) continue; // skip steps with insufficient data

    const passesErrors = step.error_rate < 0.01;
    const passesLatency = step.p95 < 500;

    if (passesErrors && passesLatency) {
      maxVUs = step.vus;
      maxRPS = step.rps;
    } else {
      // This is where it starts failing
      if (!bottleneckVUs) {
        bottleneckVUs = step.vus;
        if (!passesErrors && !passesLatency) {
          bottleneck = `error rate ${fmt(step.error_rate * 100, 1)}% AND p95 ${fmt(step.p95, 0)}ms at ${step.vus} VUs`;
        } else if (!passesErrors) {
          bottleneck = `error rate ${fmt(step.error_rate * 100, 1)}% at ${step.vus} VUs`;
        } else {
          bottleneck = `p95 latency ${fmt(step.p95, 0)}ms at ${step.vus} VUs`;
        }
      }
    }
  }

  if (!bottleneckVUs) {
    bottleneck = 'none found (all steps passed)';
  }

  const calibration = {
    timestamp: new Date().toISOString(),
    target: BASE,
    max_vus: maxVUs,
    max_rps: Math.round(maxRPS),
    bottleneck: bottleneck,
    steps: stepResults,
  };

  return {
    'results/calibration.json': JSON.stringify(calibration, null, 2),
    stdout: generateReport(calibration),
  };
}

// Extract metrics for a specific step tag from k6 summary data.
// k6 does not natively break down metrics by tag in handleSummary, so we
// use a heuristic based on overall metrics and the step progression.
// The step_latency trend with tags gives us per-step latency data.
function extractStepMetrics(data, stepTag) {
  const m = data.metrics;

  // k6 handleSummary provides aggregated metrics, not per-tag breakdowns.
  // We use the sub-metrics feature: metrics with tags are available as
  // metric_name{tag_name:tag_value} in the metrics object.
  const latencyKey = `step_latency{step:${stepTag}}`;
  const errKey = `http_errors{step:${stepTag}}`;
  const reqKey = `http_reqs{step:${stepTag}}`;

  const latencyData = m[latencyKey] ? m[latencyKey].values : null;
  const errData = m[errKey] ? m[errKey].values : null;
  const reqData = m[reqKey] ? m[reqKey].values : null;

  return {
    rps: reqData ? reqData.rate : 0,
    p95: latencyData ? (latencyData['p(95)'] || 0) : 0,
    p99: latencyData ? (latencyData['p(99)'] || 0) : 0,
    avg: latencyData ? (latencyData.avg || 0) : 0,
    errorRate: errData ? errData.rate : 0,
    requests: reqData ? reqData.count : 0,
  };
}

function fmt(val, decimals) {
  if (val === undefined || val === null) return 'N/A';
  return Number(val).toFixed(decimals);
}

function generateReport(cal) {
  const W = 62; // inner width of the box

  let report = `
\u2554${'═'.repeat(W)}\u2557
\u2551${center('HARDWARE CALIBRATION RESULTS', W)}\u2551
\u2560${'═'.repeat(W)}\u2563
\u2551${pad(`  Max sustainable VUs:  ${cal.max_vus}`, W)}\u2551
\u2551${pad(`  Max sustainable RPS:  ${cal.max_rps}`, W)}\u2551
\u2551${pad(`  Bottleneck:           ${cal.bottleneck}`, W)}\u2551
\u2560${'═'.repeat(W)}\u2563
\u2551${pad('  Step Results:', W)}\u2551
`;

  for (const step of cal.steps) {
    if (step.requests < 5) {
      report += `\u2551${pad(`    ${String(step.vus).padStart(3)} VUs:  -- (insufficient data)`, W)}\u2551\n`;
      continue;
    }

    const passed = step.error_rate < 0.01 && step.p95 < 500;
    const icon = passed ? '\u2705' : '\u274C';
    const line = `    ${String(step.vus).padStart(3)} VUs:  ${icon} ${String(Math.round(step.rps)).padStart(5)} RPS, p95=${fmt(step.p95, 0)}ms, errors=${fmt(step.error_rate * 100, 1)}%`;
    report += `\u2551${pad(line, W)}\u2551\n`;
  }

  report += `\u255A${'═'.repeat(W)}\u255D\n`;

  return report;
}

function pad(str, width) {
  // Account for multi-byte characters (emojis)
  const visLen = stripAnsi(str).length;
  if (visLen >= width) return str.substring(0, width);
  return str + ' '.repeat(width - visLen);
}

function center(str, width) {
  const visLen = str.length;
  if (visLen >= width) return str;
  const left = Math.floor((width - visLen) / 2);
  const right = width - visLen - left;
  return ' '.repeat(left) + str + ' '.repeat(right);
}

function stripAnsi(str) {
  // Simple strip for length calculation — no ANSI in k6 output, but handles emoji width
  return str;
}
