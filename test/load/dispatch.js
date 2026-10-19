import http from 'k6/http';
import { check, sleep } from 'k6';

// 5,000 req/s dispatch scenario per AGENTS.md Section 1 & 13
export const options = {
  scenarios: {
    dispatch_high_throughput: {
      executor: 'ramping-arrival-rate',
      startRate: 500,
      timeUnit: '1s',
      preAllocatedVUs: 1000,
      maxVUs: 5000,
      stages: [
        { target: 2000, duration: '30s' }, // warm up
        { target: 5000, duration: '60s' }, // peak 5,000 req/s
        { target: 5000, duration: '60s' }, // sustain
        { target: 0, duration: '15s' },    // ramp down
      ],
    },
  },
  thresholds: {
    // SLO boundary: p95 latency < 2s end-to-end
    http_req_duration: ['p(95)<2000'],
    // Error rate must stay below 1%
    http_req_failed: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.GATEWAY_URL || 'http://localhost:8080';

// Simple UUID generator for k6
function uuidv4() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
    const r = (Math.random() * 16) | 0,
      v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export default function () {
  const riderID = `rider-${Math.floor(Math.random() * 50000)}`;
  const idemKey = uuidv4();

  // NYC bounding area
  const pickupLat = 40.7128 + (Math.random() * 0.05 - 0.025);
  const pickupLon = -74.0060 + (Math.random() * 0.05 - 0.025);

  const payload = JSON.stringify({
    riderId: riderID,
    pickup: {
      latitude: pickupLat,
      longitude: pickupLon,
    },
    dropoff: {
      latitude: 40.7580,
      longitude: -73.9855,
    },
    serviceType: 'STANDARD',
    region: 'downtown',
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idemKey,
    },
  };

  const res = http.post(`${BASE_URL}/api/ride/request`, payload, params);

  check(res, {
    'status is 200 or 201': (r) => r.status === 200 || r.status === 201,
  });
}

