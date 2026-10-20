import http from 'k6/http';
import { check, sleep } from 'k6';

// Soak Test Profile: 250 QPS sustained for 30 minutes
// Measures memory leaks, GC pauses, database connection stability, and latency creep.
export const options = {
  scenarios: {
    soak_scenario: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.QPS || 250),
      timeUnit: '1s',
      duration: __ENV.DURATION || '30m',
      preAllocatedVUs: 150,
      maxVUs: 800,
    },
  },
  thresholds: {
    // SLO: 95% of requests must complete under 500ms
    http_req_duration: ['p(95)<500'],
    // Maximum allowable failure rate < 1%
    http_req_failed: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.GATEWAY_URL || 'http://dispatch-gateway:8080';
const JWT_TOKEN = __ENV.JWT_TOKEN || '';

function uuidv4() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
    const r = (Math.random() * 16) | 0,
      v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export default function () {
  const riderID = `rider-${Math.floor(Math.random() * 100000)}`;
  const idemKey = uuidv4();

  // NYC downtown area
  const pickupLat = 40.7128 + (Math.random() * 0.05 - 0.025);
  const pickupLon = -74.0060 + (Math.random() * 0.05 - 0.025);

  const payload = JSON.stringify({
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

  const headers = {
    'Content-Type': 'application/json',
    'Idempotency-Key': idemKey,
  };

  if (JWT_TOKEN) {
    headers['Authorization'] = `Bearer ${JWT_TOKEN}`;
  }

  const res = http.post(`${BASE_URL}/api/ride/request`, payload, { headers: headers });

  check(res, {
    'status is 200 or 201': (r) => r.status === 200 || r.status === 201,
  });
}
