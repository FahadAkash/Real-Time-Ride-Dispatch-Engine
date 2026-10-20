#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

CHOICE="${1:-}"

if [ -z "$CHOICE" ]; then
  echo "============================================================"
  echo "      FULL-STACK BENCHMARK: GATEWAY, REDIS, MONGO, KAFKA    "
  echo "============================================================"
  echo "Select a load test profile:"
  echo "  1) 40% Load   -  200 QPS | 400 Drivers  | 10s (Optimal)"
  echo "  2) 50% Load   -  250 QPS | 500 Drivers  | 10s (Peak)"
  echo "  3) 100% Load  -  500 QPS | 1000 Drivers | 10s (Max Stress)"
  echo "  4) Custom (Enter your own QPS and Duration)"
  echo "============================================================"
  read -p "Enter choice [1-4] (default: 2): " USER_INPUT
  CHOICE="${USER_INPUT:-2}"
fi

case "$CHOICE" in
  1|"40"|"40%")
    QPS=200
    DRIVERS=400
    DURATION="10s"
    LABEL="40% Load Test (200 QPS, 400 Drivers)"
    ;;
  2|"50"|"50%")
    QPS=250
    DRIVERS=500
    DURATION="10s"
    LABEL="50% Load Test (250 QPS, 500 Drivers)"
    ;;
  3|"100"|"100%")
    QPS=500
    DRIVERS=1000
    DURATION="10s"
    LABEL="100% Full Stress Test (500 QPS, 1000 Drivers)"
    ;;
  4|"custom")
    read -p "Enter Target QPS (e.g. 250): " QPS
    read -p "Enter Concurrent Drivers (e.g. 500): " DRIVERS
    read -p "Enter Duration (e.g. 10s, 30s): " DURATION
    LABEL="Custom Test (${QPS} QPS, ${DRIVERS} Drivers, ${DURATION})"
    ;;
  *)
    echo "Invalid option. Exiting."
    exit 1
    ;;
esac

echo ""
echo "📊 Real-time Grafana:      http://192.168.0.113/grafana/"
echo "📈 Real-time LAN Monitor:  http://192.168.0.113:8888/"
echo ""

python3 "${SCRIPT_DIR}/deep_infrastructure_benchmark.py" \
  --qps="${QPS}" \
  --drivers="${DRIVERS}" \
  --duration="${DURATION}" \
  --label="${LABEL}"
