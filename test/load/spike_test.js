import http from 'k6/http';
import { check, sleep } from 'k6';

// Spike Test Profile: Sudden burst from 50 QPS to 400 QPS
// Evaluates queue buildup, latency recovery, and graceful degradation under sudden surges.
export const options = {
  scenarios: {
    spike_scenario: {
      executor: 'ramping-arrival-rate',
      startRate: 50,
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 1200,
      stages: [
        { target: 50, duration: '20s' },   // 1. Normal traffic baseline at 50 QPS
        { target: 400, duration: '5s' },   // 2. Sudden steep spike jump to 400 QPS
        { target: 400, duration: '30s' },  // 3. Sustained shock period at 400 QPS
        { target: 50, duration: '10s' },   // 4. Fast drop down to 50 QPS
        { target: 50, duration: '25s' },   // 5. Recovery validation phase at 50 QPS
      ],
    },
  },
  thresholds: {
    // SLO boundary during recovery
    http_req_duration: ['p(90)<2500'],
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
