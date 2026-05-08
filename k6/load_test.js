/**
 * Rate Limiter Labs — k6 load test
 *
 * Hits all 5 algorithm endpoints under three traffic patterns so you can
 * compare their behavior side-by-side in Grafana.
 *
 * Run locally (gateway must be up on :8080):
 *   k6 run k6/load_test.js
 *
 * Run against a remote gateway:
 *   k6 run -e TARGET_URL=http://host:8080 k6/load_test.js
 *
 * Thresholds:
 *   - 95th percentile latency < 200 ms
 *   - Error rate (non-200/429) < 1%
 */

import http from 'k6/http';
import { sleep, check } from 'k6';
import { Rate } from 'k6/metrics';

// Tell k6 that 429 is an *expected* status so it is NOT counted in
// http_req_failed. That metric then only captures real errors (5xx,
// timeouts, connection resets) — which is what we actually care about.
http.setResponseCallback(http.expectedStatuses(200, 429));

const BASE = __ENV.TARGET_URL || 'http://127.0.0.1:8080';

// All 5 algorithm endpoints exposed by the gateway.
const ENDPOINTS = [
  'token-bucket',
  'fixed-window',
  'leaky-bucket',
  'sliding-log',
  'sliding-counter',
];

// Custom metric: requests that are neither 200 nor 429 (unexpected errors).
const unexpectedErrors = new Rate('unexpected_errors');

export const options = {
  scenarios: {
    // Steady: well below limit — all algorithms should allow 100%.
    steady: {
      executor: 'constant-vus',
      vus: 5,
      duration: '30s',
      exec: 'steadyLoad',
      tags: { scenario: 'steady' },
    },

    // Burst: instant spike — TokenBucket/LeakyBucket (capacity=20) allow more
    // than window-based algorithms (limit=10). The gap is the key insight.
    burst: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '3s',  target: 50 },
        { duration: '7s',  target: 50 },
        { duration: '2s',  target: 0  },
      ],
      exec: 'burstLoad',
      startTime: '35s',
      tags: { scenario: 'burst' },
    },

    // Overload: sustained 2× limit — shows how each algorithm degrades gracefully.
    overload: {
      executor: 'constant-vus',
      vus: 20,
      duration: '30s',
      exec: 'overloadLoad',
      startTime: '50s',
      tags: { scenario: 'overload' },
    },
  },

  thresholds: {
    // Non-200/429 responses should be near zero.
    unexpected_errors: ['rate<0.01'],
    // p95 latency under 200 ms across all scenarios.
    http_req_duration: ['p(95)<200'],
  },
};

/** Hit every algorithm endpoint once and record results. */
function hitAll(tag) {
  for (const ep of ENDPOINTS) {
    const res = http.get(`${BASE}/${ep}`, {
      tags: { algorithm: ep, scenario: tag },
    });

    const ok = check(res, {
      'status 200 or 429': (r) => r.status === 200 || r.status === 429,
    });
    unexpectedErrors.add(!ok);
  }
}

export function steadyLoad()   { hitAll('steady');   sleep(0.2);  }
export function burstLoad()    { hitAll('burst');    sleep(0.02); }
export function overloadLoad() { hitAll('overload'); sleep(0.05); }
