# ADR 001: Selection of Kafka Client (`twmb/franz-go`)

## Context
The Real-Time Ride Dispatch Engine must handle up to **5,000 ride requests/sec** (300,000 requests/minute) with p95 dispatch latency under 2.0 seconds. Every state mutation in the ride lifecycle (`ride.requested`, `ride.driver-assigned`, `ride.accepted`, etc.) generates an event published to Kafka via the transactional outbox pattern. This requires a Kafka client capable of tens of thousands of messages per second with minimal CPU and garbage collection overhead.

Per Section 5 of `AGENTS.md`, the two evaluated candidates were:
1. `github.com/segmentio/kafka-go`
2. `github.com/twmb/franz-go`

## Decision
We select **`github.com/twmb/franz-go`** as the standard Kafka client for both producers and consumers across all services.

## Rationale
1. **Performance and Allocation Profile**:
   `franz-go` is specifically engineered for zero allocations in common producing/consuming loops. Under high load (5,000+ msg/s), Go GC pauses can severely degrade p95 latency. `franz-go` drastically reduces GC pressure compared to alternative libraries.
2. **Feature Completeness and Reliability**:
   - Native support for modern Kafka protocol features (KIPs), record batching, snappy/zstd compression, and transactional producers.
   - Robust built-in support for SASL/SCRAM, SASL/PLAIN, and TLS 1.3 without cgo dependencies.
   - Comprehensive metrics and tracing hooks that integrate directly with OpenTelemetry and Prometheus.
3. **Partition-Key Ordering**:
   `franz-go` supports deterministic partitioning using the Murmur2 partitioner, ensuring all events with `Key = rideID` are strictly ordered within the same partition.

## Consequences
- All event producers and consumers in `internal/events` will utilize `github.com/twmb/franz-go/pkg/kgo`.
- Code stays pure Go with no requirement for `librdkafka` C libraries or CGO, simplifying multi-stage distroless Docker builds.

