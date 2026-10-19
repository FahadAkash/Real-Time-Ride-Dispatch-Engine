#!/usr/bin/env bash
set -euo pipefail

# Verifies that required environment variables are set before service boot per AGENTS.md Section 6.
REQUIRED_VARS=(
  "APP_ENV"
  "HTTP_ADDR"
  "METRICS_ADDR"
  "REDIS_ADDRS"
  "MONGO_URI"
  "KAFKA_BROKERS"
)

MISSING=0
for var in "${REQUIRED_VARS[@]}"; do
  if [ -z "${!var:-}" ]; then
    echo "ERROR: Required environment variable '${var}' is unset or empty."
    MISSING=1
  fi
done

if [ "${MISSING}" -eq 1 ]; then
  echo "Boot verification failed: missing environment configuration."
  exit 1
fi

echo "Environment verification passed."

