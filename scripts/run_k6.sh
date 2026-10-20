#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

TEST_TYPE="${1:-spike}"
GATEWAY_URL="${GATEWAY_URL:-http://dispatch-gateway:8080}"
DURATION="${DURATION:-}"

echo "============================================================"
echo "                   K6 LOAD TEST LAUNCHER                    "
echo "============================================================"

# Generate valid RS256 JWT Token for k6
TOKEN=$(python3 -c "
import jwt, time
with open('${ROOT_DIR}/deploy/keys/jwt_rs256.key', 'r') as f:
    key = f.read()
payload = {
    'sub': 'rider-k6-suite',
    'role': 'rider',
    'iss': 'dispatch-auth',
    'aud': 'dispatch-engine',
    'exp': int(time.time()) + 86400 * 30
}
print(jwt.encode(payload, key, algorithm='RS256'))
")

SCRIPT_FILE="test/load/spike_test.js"
if [ "$TEST_TYPE" = "soak" ]; then
  SCRIPT_FILE="test/load/soak_test.js"
  echo "Running SOAK TEST (250 QPS, default: 30 minutes)..."
elif [ "$TEST_TYPE" = "spike" ]; then
  SCRIPT_FILE="test/load/spike_test.js"
  echo "Running SPIKE TEST (50 QPS -> 400 QPS sudden jump)..."
else
  SCRIPT_FILE="test/load/dispatch.js"
  echo "Running STANDARD LOAD TEST..."
fi

ENV_ARGS="-e GATEWAY_URL=${GATEWAY_URL} -e JWT_TOKEN=${TOKEN}"
if [ -n "$DURATION" ]; then
  ENV_ARGS="${ENV_ARGS} -e DURATION=${DURATION}"
fi

echo "Target Gateway: ${GATEWAY_URL}"
echo "Script:         ${SCRIPT_FILE}"
echo "Grafana Live:   http://192.168.0.113/grafana/"
echo "LAN Monitor:    http://192.168.0.113:8888/"
echo "============================================================"
echo ""

docker run --rm -i \
  --network server-infra_apps \
  -v "${ROOT_DIR}:/scripts" \
  ${ENV_ARGS} \
  grafana/k6:latest \
  run "/scripts/${SCRIPT_FILE}"

