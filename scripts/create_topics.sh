#!/usr/bin/env bash
set -euo pipefail

# Scripts must be idempotent and safe to re-run per AGENTS.md Section 6.
KAFKA_BROKER="${KAFKA_BROKERS:-localhost:9092}"
PARTITIONS="${PARTITIONS:-12}"
REPLICATION_FACTOR="${REPLICATION_FACTOR:-1}"

TOPICS=(
  "ride.requested"
  "ride.driver-assigned"
  "ride.accepted"
  "ride.rejected"
  "ride.started"
  "ride.completed"
  "ride.cancelled"
)

echo "Connecting to Kafka cluster at ${KAFKA_BROKER}..."

for topic in "${TOPICS[@]}"; do
  echo "Ensuring topic '${topic}' exists (partitions: ${PARTITIONS}, replication: ${REPLICATION_FACTOR})..."
  if command -v rpk >/dev/null 2>&1; then
    rpk topic create "${topic}" -p "${PARTITIONS}" -r "${REPLICATION_FACTOR}" --brokers "${KAFKA_BROKER}" 2>/dev/null || true
  elif command -v kafka-topics.sh >/dev/null 2>&1; then
    kafka-topics.sh --bootstrap-server "${KAFKA_BROKER}" \
      --create --if-not-exists \
      --topic "${topic}" \
      --partitions "${PARTITIONS}" \
      --replication-factor "${REPLICATION_FACTOR}"
  else
    echo "Notice: Neither rpk nor kafka-topics.sh found in PATH. Skipping local CLI creation."
    break
  fi
done

echo "Kafka topics ready."

