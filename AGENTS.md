# AGENTS.md — Real-Time Ride Dispatch Engine

Instructions for AI coding agents (and humans) working in this repository.
Read this file fully before changing anything. When this file conflicts with a
task prompt, stop and ask rather than guess.

---

## 1. Project overview

A high-throughput, geo-optimized ride dispatch backend written in **Go**.
It matches ride requests to the nearest available driver, guarantees a driver
is never double-assigned, survives restarts without losing requests, and
exposes metrics and alerts.

**Business goal:** cut rider wait time by >= 20% and reduce "no-driver" failures.

**Acceptance criteria (non-negotiable):**
1. Handle **5,000 ride requests/sec** (300,000/min) with **p95 dispatch latency < 2s**.
2. **At most one driver per ride request.** No duplicate assignment, ever.
3. **No lost requests** across service restarts (persistent state + persistent queues).
4. Expose key metrics (QPS, latency, errors) with alerts.

**Assumptions:** ~50,000 active drivers, location update every ~5s (~10,000
writes/sec), authenticated users with unique IDs, push delivery via FCM/APNs.

---

## 2. Architecture summary

```
Rider app ─┐                         ┌─> Notification svc ──> FCM / APNs ──> Driver app
           ├─> API Gateway ──> Dispatch svc ──┤
Driver app ┘        │                 │       └─> Kafka ──> Billing / Tracking / Loyalty
                    └─> Location svc ─┴─> Redis GEO (live drivers, locks)
                                      └─> MongoDB Atlas (durable ride records)
Timeout worker: expires unanswered offers, releases locks, re-matches.
```

| Component | Responsibility | Store |
|---|---|---|
| Gateway | AuthN, AuthZ, rate limit, idempotency check, routing | Redis (idempotency, rate limit) |
| Dispatch | Match, reserve, assign, ride state machine | Redis (locks), Mongo (rides), Kafka |
| Location | Ingest driver positions, shard by region | Redis GEO |
| Notifier | Push offers to drivers, retry with backoff | none |
| Timeout worker | Offer expiry, re-assignment | Redis, Mongo |

**Source of truth rule:** MongoDB is the durable source of truth for rides.
Redis holds *fast, disposable* state (live positions, locks). Anything in Redis
must be reconstructible or safe to lose.

---

## 3. Repository structure

```
ride-dispatch-engine/
├── cmd/                      # thin entrypoints: wire config + deps + server only
│   ├── gateway/main.go
│   ├── dispatch/main.go
│   ├── location/main.go
│   ├── notifier/main.go
│   ├── timeout-worker/main.go
│   └── loadgen/main.go       # simulated drivers + riders
├── internal/
│   ├── domain/               # pure types + errors. IMPORTS NOTHING from this repo.
│   ├── dispatch/             # service, matcher, reservation, statemachine
│   ├── location/             # service, geoindex, sharding
│   ├── notify/               # notifier interface, fcm.go, apns.go
│   ├── worker/               # timeout.go
│   ├── gateway/              # router, auth, authz, idempotency, ratelimit
│   ├── store/
│   │   ├── redis/            # client, geo, locks, idempotency
│   │   └── mongo/            # ride repo, driver repo, indexes
│   ├── events/               # producer, consumer, outbox, schema
│   └── platform/
│       ├── config/  logger/  metrics/  tracing/
│       ├── resilience/       # circuit breaker, retry + backoff
│       └── server/           # HTTP/gRPC bootstrap, graceful shutdown
├── api/
│   ├── openapi.yaml          # REST contract (source of truth for public API)
│   └── proto/                # gRPC contracts for internal calls
├── deploy/
│   ├── docker/               # Dockerfiles + docker-compose.yml (local stack)
│   ├── k8s/                  # deployments, HPA, NetworkPolicies, ServiceMonitor, configmaps
│   └── observability/        # Prometheus, Alertmanager, Grafana (see section 12.1)
│       ├── docker-compose.observability.yml
│       ├── prometheus/       # prometheus.yml, alerts.yml
│       ├── alertmanager/     # alertmanager.yml, secrets/ (git-ignored)
│       └── grafana/          # provisioning/{datasources,dashboards}, dashboards/*.json
├── test/
│   ├── integration/          # real Redis/Mongo/Kafka via testcontainers
│   ├── concurrency/          # double-booking race tests
│   └── load/                 # k6 scenarios (5k req/s)
├── scripts/                  # seed data, create indexes, create Kafka topics
├── docs/                     # architecture.md, ADRs, runbook.md
├── .github/workflows/ci.yml
├── Makefile
├── AGENTS.md
├── go.mod
└── README.md
```

### Dependency rules (enforced in review)

- `domain` imports **nothing** from this repo.
- `dispatch`, `location`, `worker` depend on **interfaces they define themselves**
  (`DriverIndex`, `RideRepo`, `Locker`, `Notifier`, `EventPublisher`).
  `store/*`, `events`, `notify` implement them.
- `store/*` and `events` never import `dispatch` or `gateway`.
- `cmd/*` is the only place that constructs concrete implementations.
- No package imports `cmd/*`. No circular imports. No global mutable state.
- Each service binary must be buildable and deployable independently.

---

## 4. Commands

Run these. Do not invent alternatives.

```bash
make up          # docker compose: redis, mongo, kafka, prometheus, grafana
make down        # stop local stack
make obs-up      # start Prometheus, Alertmanager, Grafana + exporters
make obs-down    # stop the observability stack
make obs-check   # promtool check config/rules + amtool check-config + JSON lint
make run-all     # run all services locally
make fmt         # gofmt + goimports
make lint        # golangci-lint run
make vet         # go vet ./...
make test        # unit tests: go test -race -count=1 ./...
make itest       # integration tests (testcontainers; needs Docker)
make ctest       # concurrency / double-booking tests, high -count
make load        # k6 run test/load/dispatch.js (5k req/s scenario)
make sec         # gosec + govulncheck + gitleaks + trivy
make proto       # regenerate gRPC code (buf generate)
make build       # build all binaries into ./bin
make docker      # build images
make migrate     # create Mongo indexes + Kafka topics (scripts/)
```

**Definition of done for every change:**
`make fmt lint vet test sec` all pass, plus `make itest` if you touched
`store/`, `events/`, or the dispatch flow, plus `make ctest` if you touched
reservation, locking, or the state machine.

---

## 5. Packages (Go dependencies)

Prefer the standard library first. Add a third-party dependency only when it
clearly earns its place. Pin exact versions in `go.mod`, run `go mod tidy`,
and check the latest stable release and its changelog before adding.

| Purpose | Package |
|---|---|
| HTTP router | `github.com/go-chi/chi/v5` |
| gRPC + protobuf | `google.golang.org/grpc`, `google.golang.org/protobuf` |
| Redis client | `github.com/redis/go-redis/v9` |
| Distributed lock (if needed beyond `SET NX`) | `github.com/go-redsync/redsync/v4` |
| MongoDB driver | `go.mongodb.org/mongo-driver` (official) |
| Kafka client | `github.com/segmentio/kafka-go` **or** `github.com/twmb/franz-go` (pick one, record in an ADR) |
| JWT | `github.com/golang-jwt/jwt/v5` |
| Config | `github.com/caarlos0/env/v11` (env-first, 12-factor) |
| Logging | stdlib `log/slog` (JSON handler) |
| Metrics | `github.com/prometheus/client_golang` |
| Tracing | `go.opentelemetry.io/otel` + OTLP exporter |
| Circuit breaker | `github.com/sony/gobreaker` |
| Retry/backoff | `github.com/cenkalti/backoff/v4` |
| Rate limiting | `golang.org/x/time/rate` (local) + Redis token bucket (global) |
| IDs | `github.com/google/uuid` |
| FCM push | `firebase.google.com/go/v4` |
| APNs push | `github.com/sideshow/apns2` |
| Validation | `github.com/go-playground/validator/v10` |
| Testing | `github.com/stretchr/testify`, `github.com/testcontainers/testcontainers-go`, `go.uber.org/mock` |
| Concurrency helpers | `golang.org/x/sync/errgroup`, `golang.org/x/sync/singleflight` |

**Dev tools (not in go.mod):** `golangci-lint`, `govulncheck`, `gosec`, `buf`,
`k6`, `gitleaks`, `trivy`, `air` (optional hot reload), `goimports`.

---

## 6. Scripts (`scripts/`)

| Script | Purpose |
|---|---|
| `scripts/create_indexes.go` | Create Mongo indexes (see section 9) |
| `scripts/create_topics.sh` | Create Kafka topics with partitions/retention |
| `scripts/seed_drivers.go` | Seed N fake drivers into Redis GEO for local/load tests |
| `scripts/gen_keys.sh` | Generate **local-dev-only** JWT signing keys (never reuse outside dev) |
| `scripts/check_env.sh` | Verify required env vars are set before boot |

Scripts must be idempotent (safe to re-run), must refuse to run against a
production endpoint without an explicit `--i-know-this-is-prod` flag, and must
never contain secrets.

---

## 7. Authentication

**Who calls us:** riders, drivers, admins/operators, and other services.

### End users (riders, drivers)
- The system assumes an upstream identity provider issues tokens. The gateway
  **validates** tokens; it does not manage passwords.
- Tokens: **JWT**, asymmetric signing (**RS256 or ES256**). Never HS256 with a
  shared secret across services. Never accept `alg: none`.
- Validate on every request: signature, `exp`, `nbf`, `iss`, `aud`, and a
  non-empty `sub`. Allow at most 30-60s clock skew.
- Access token lifetime: **<= 15 minutes**. Refresh handled by the identity provider.
- Public keys fetched from JWKS, cached, with rotation support (`kid`).
- Custom claims: `role` (`rider` | `driver` | `admin`) and `city`/`region` if used.
- Pin allowed algorithms explicitly in the parser.

### Service-to-service
- Internal calls (gateway -> dispatch/location, workers -> stores) use **mTLS**
  or short-lived signed service tokens with `role: service` and an `aud` per target.
- No service trusts a request just because it comes from inside the cluster.
- Internal endpoints (`/metrics`, `/debug/*`, gRPC) are **not** exposed through the public gateway.

### Infrastructure credentials
- Redis: `AUTH` with ACL users per service, TLS on.
- MongoDB: per-service DB user, least privilege (no `root`/`dbAdmin` at runtime), TLS.
- Kafka: SASL + TLS, per-service principals with topic-level ACLs.
- Credentials come from env/secret manager, rotated regularly.

---

## 8. Authorization

**Principle: identity comes from the verified token, never from the request body.**

- `riderID` and `driverID` are derived from the JWT `sub`. If the body also
  contains an ID, it must equal the token's `sub` or the request is rejected (403).
- Every handler performs **object-level authorization**: loading a ride by
  `rideID` is not enough; confirm the caller is that ride's rider or its assigned driver.
- Default deny. Routes without an explicit policy are rejected.

| Endpoint | Allowed role | Extra rule |
|---|---|---|
| `POST /api/ride/request` | rider | `riderID` = token `sub`; requires `Idempotency-Key` |
| `GET /api/ride/status` | rider, driver, admin | rider must own the ride; driver must be assigned to it |
| `POST /api/ride/response` | driver | `driverID` = token `sub`; ride must be assigned to this driver and in `PENDING_ACCEPT` |
| `POST /api/ride/cancel` | rider, driver (assigned), admin | valid only in cancellable states |
| `POST /api/driver/update-location` | driver | `driverID` = token `sub`; plausibility checks (section 10) |
| `GET /healthz`, `GET /readyz` | none (internal network) | no sensitive data |
| `GET /metrics` | internal scraper only | not routable from the public internet |
| `/admin/*` | admin | audit-logged |

Authorization logic lives in `internal/gateway/authz.go` and, for object-level
checks, inside `dispatch` (it owns ride state). Never skip it "because the
gateway already checked."

---

## 9. Data model

### Redis key schema (all keys namespaced, all with TTLs unless noted)
```
drivers:geo:{region}        GEO set of available drivers        (no TTL; members pruned by staleness job)
driver:{id}:status          hash {state, lastSeen, region}      TTL ~ 30s, refreshed by location updates
driver:{id}:lock            value = rideID, SET NX EX 15        reservation lock
ride:{id}:lock              short mutex for state transitions   TTL ~ 5s
idem:{riderID}:{uuid}       value = rideID                      TTL 24h
rl:{principal}:{route}      rate-limit counters                 TTL = window
```

### MongoDB
- `rides`: `{ rideId, riderId, driverId?, status, pickup, dropoff, serviceType, version, createdAt, updatedAt, history[] }`
- `drivers`: `{ driverId, rating, vehicle, region }`
- Indexes: unique `{rideId}`; unique `{riderId, idempotencyKey}`; `{riderId, createdAt:-1}`;
  `{status, updatedAt}`; `2dsphere` on `pickup` only if the Mongo geo fallback is used.
- Use optimistic concurrency via a `version` field on every ride update.

### Ride state machine
```
PENDING_ACCEPT -> ACCEPTED -> STARTED -> COMPLETED
PENDING_ACCEPT -> REASSIGNING -> PENDING_ACCEPT   (reject or timeout)
PENDING_ACCEPT | REASSIGNING -> NO_DRIVER_FOUND
(any state before STARTED) -> CANCELLED
```
All transitions go through `dispatch/statemachine.go`. Illegal transitions return
a typed error. **Never write `status` directly from anywhere else.**
Transitions are conditional updates (`status == expected AND version == expected`).

### Kafka topics
`ride.requested`, `ride.driver-assigned`, `ride.accepted`, `ride.rejected`,
`ride.started`, `ride.completed`, `ride.cancelled`.
- Message key = `rideID` (keeps per-ride ordering).
- Event envelope: `{eventId, type, rideId, occurredAt, schemaVersion, payload}`.
- Consumers must be idempotent (dedupe on `eventId`).
- Publish via the **outbox pattern**: write the ride + outbox record in one DB
  operation; a relay publishes to Kafka. No "write DB, then hope publish works."

---

## 10. Core flows (implement exactly this)

**Request ride**
1. Gateway: validate JWT, check role=rider, rate limit, require `Idempotency-Key`.
2. Idempotency: `SET idem:{riderID}:{uuid} <rideID> NX`. If it exists, return the existing ride.
3. Dispatch: `GEOSEARCH` nearest N drivers in the pickup region.
4. Rank (distance, then rating/acceptance rate). Filter out non-`available` drivers.
5. Reserve: `SET driver:{id}:lock <rideID> NX EX 15`. On failure, try the next candidate.
6. Persist ride as `PENDING_ACCEPT` (+ outbox event) **before** notifying anyone.
7. Notify driver (retry with backoff, circuit breaker). Return `{rideID, status}` to rider.

**Driver response**
- Accept: conditional update `PENDING_ACCEPT -> ACCEPTED` filtered on `driverId` and `version`. Publish `ride.accepted`.
- Reject / timeout: Timeout worker releases the lock, marks `REASSIGNING`, re-runs matching excluding previous drivers. Stop after a configured max attempts -> `NO_DRIVER_FOUND`.
- Release a driver lock **only if the lock value still equals this rideID** (compare-and-delete via Lua script).

**Location update**
- Validate coordinates and plausibility (see below), write `GEOADD`, refresh `driver:{id}:status`.
- Drop stale drivers (no update for > 30s) from the available set.

**Input validation (all endpoints)**
- `lat` in [-90, 90], `lon` in [-180, 180]; reject NaN/Inf; enforce a service-area bounding box.
- Reject absurd pickup/dropoff distances; cap request body size (e.g. 16 KB).
- Strict JSON decoding (`DisallowUnknownFields`). Validate enums (`serviceType`).
- Location plausibility: reject speeds above a sane threshold between consecutive
  updates (GPS spoofing / teleport), and rate-limit to ~1 update / 2s / driver.

---

## 11. Security requirements

### Transport and secrets
- TLS 1.2+ everywhere (public and internal). HSTS on public endpoints.
- **No secrets in code, config files, images, logs, or git history.** Use env vars
  injected from a secret manager (e.g. GCP Secret Manager / K8s secrets backed by KMS).
- `.env.example` documents variable names only. `.env` is git-ignored.
- `gitleaks` runs in CI and as a pre-commit hook.

### API hardening
- Rate limiting per principal and per IP at the gateway (stricter on `/ride/request`).
- Request timeouts, body size limits, header size limits, and server read/write/idle timeouts on every HTTP server.
- CORS: explicit allowlist, no wildcard with credentials.
- Return generic errors to clients; detailed errors only in logs. No stack traces in responses.
- Use `crypto/rand` for anything security-relevant, never `math/rand`.
- Constant-time comparison (`subtle.ConstantTimeCompare`) for secrets/tokens.

### Data protection and privacy
- Treat rider/driver location, phone numbers, and push tokens as **PII**.
- **Never log** tokens, JWTs, push tokens, full phone numbers, or precise coordinates
  at INFO level. Log IDs, not personal data. Mask where unavoidable.
- Encrypt data at rest (Mongo Atlas encryption, Redis persistence volumes).
- Retention: define TTLs for location history and logs; do not keep raw location trails forever.
- Least-privilege DB users; no shared credentials across services.

### Abuse and fraud
- Idempotency keys prevent duplicate rides from retries.
- Detect and throttle repeated request/cancel loops per rider.
- Detect GPS spoofing and impossible driver movement; flag for review.
- Audit-log admin actions and any manual ride/driver override.

### Supply chain and runtime
- CI runs: `govulncheck`, `gosec`, `trivy` (images + IaC), `gitleaks`. Fail on high/critical.
- Containers: multi-stage build, **distroless or scratch**, **non-root user**,
  read-only root filesystem, no shell, dropped capabilities.
- Kubernetes: `NetworkPolicy` default-deny with explicit allow rules; resource
  requests/limits; no privileged pods; separate service accounts per service.
- Pin base image digests. Review every new dependency (maintenance, license, CVEs).
- Never run `go get` for unfamiliar packages without checking them.

### Safe concurrency (security-adjacent)
- Reservation and state transitions must be atomic. Race conditions here are
  both a correctness bug and a double-booking/fraud risk.
- Always run `go test -race`. Treat any race detector report as a blocker.

---

## 12. Reliability, performance, observability

**Budgets**
- p95 dispatch latency < 2s end to end; target p50 well under 500ms.
- Redis geo query < 10ms p95; no unbounded scans (`KEYS *` is forbidden; use `SCAN` sparingly).
- Dispatch pods are **stateless** and horizontally scalable (HPA on CPU + Kafka lag).

**Resilience**
- Every external call: context with timeout, retry with exponential backoff + jitter
  (only for idempotent operations), and a circuit breaker.
- Graceful shutdown: stop accepting, drain in-flight, flush Kafka/outbox, close pools.
- If Redis is unavailable, degrade to Mongo `2dsphere` lookup (slower, still correct) or fail fast with a clear error. Never match without a working lock.

**Metrics (Prometheus)**
- `dispatch_requests_total{result}`, `dispatch_latency_seconds` (histogram),
  `dispatch_no_driver_total`, `dispatch_double_booking_total` (must stay 0),
  `dispatch_reassignments_total`, `driver_location_updates_total`,
  `active_drivers`, `kafka_consumer_lag`, `outbox_pending`, `push_failures_total`.
- Label cardinality: **never** label by rideID, riderID, or driverID.

**Alerts**
- p95 dispatch latency > 5s; error rate > 1%; `dispatch_double_booking_total > 0`;
  outbox backlog growing; Kafka lag growing; Redis/Mongo connection failures.

**Logging**
- Structured JSON via `slog`: `level, ts, service, requestId, rideId, step, outcome, latency_ms`.
- Propagate `requestId` and trace context across services.

**Tracing**
- OpenTelemetry spans: gateway -> dispatch -> redis/mongo -> notifier.

### 12.1 Prometheus and Grafana configuration

All config lives in `deploy/observability/`. Start it with `make obs-up`
(requires the shared Docker network: `docker network create dispatch-net`, once).

| Tool | Local URL | Notes |
|---|---|---|
| Prometheus | http://localhost:9090 | scrapes services every 15s, 15-day retention |
| Alertmanager | http://localhost:9093 | routes `critical` to on-call, `warning` to the alerts channel |
| Grafana | http://localhost:3000 | user `admin`, password from `GRAFANA_ADMIN_PASSWORD` in `.env` |

All ports bind to `127.0.0.1`. Never publish them publicly.

**Rules for agents**
- Every service serves `/metrics` on a **separate internal listener** (`METRICS_ADDR=:9100`), not on the public API port.
- Create metrics with `promauto` in `internal/platform/metrics`. Use the exact names from section 12.
- `dispatch_requests_total{result}` values are a fixed enum: `success | no_driver | error | duplicate`. Do not invent new values without updating alerts and dashboards.
- `dispatch_latency_seconds` buckets must include the SLO boundary: `0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10`.
- `kafka_consumer_lag` uses labels `{group, topic}`. Exporters add `redis_up`, `mongodb_up`, `kafka_brokers`.
- Add the standard Go collectors (`go_*`, `process_*`) so the goroutine and memory panels work.
- Allowed label keys: `service, env, result, route, method, status, topic, group`. **Never** use rideID, riderID, driverID, or any user-supplied value as a label.
- Adding or renaming a metric means updating `alerts.yml` and the dashboard JSON in the **same PR**.
- Dashboards are provisioned from JSON with `allowUiUpdates: false`. Edit in the UI, export JSON, commit it. UI-only changes are lost.
- Keep secrets out of these files. The Slack webhook lives in `alertmanager/secrets/slack_url` (git-ignored). Grafana password comes from `.env`.
- Run `make obs-check` before committing any change here. Pin image tags and bump them to the latest stable release deliberately.
- Alert thresholds in the SLO group come from the acceptance criteria (p95 < 2s warning, > 5s critical, error rate > 1%, double-booking = 0). Do not loosen them to silence an alert.

**`deploy/observability/prometheus/prometheus.yml`**
```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    env: local

rule_files:
  - /etc/prometheus/alerts.yml

alerting:
  alertmanagers:
    - static_configs:
        - targets: ["alertmanager:9093"]

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]
        labels: { service: prometheus }

  # App services expose /metrics on an INTERNAL port (METRICS_ADDR=:9100),
  # never on the public listener.
  - job_name: gateway
    static_configs:
      - targets: ["gateway:9100"]
        labels: { service: gateway }

  - job_name: dispatch
    static_configs:
      - targets: ["dispatch:9100"]
        labels: { service: dispatch }

  - job_name: location
    static_configs:
      - targets: ["location:9100"]
        labels: { service: location }

  - job_name: notifier
    static_configs:
      - targets: ["notifier:9100"]
        labels: { service: notifier }

  - job_name: timeout-worker
    static_configs:
      - targets: ["timeout-worker:9100"]
        labels: { service: timeout-worker }

  - job_name: redis
    static_configs:
      - targets: ["redis-exporter:9121"]
        labels: { service: redis }

  - job_name: mongodb
    static_configs:
      - targets: ["mongodb-exporter:9216"]
        labels: { service: mongodb }

  - job_name: kafka
    static_configs:
      - targets: ["kafka-exporter:9308"]
        labels: { service: kafka }
```

**`deploy/observability/prometheus/alerts.yml`**
```yaml
groups:
  - name: dispatch-slo
    rules:
      - alert: DispatchLatencyP95High
        expr: histogram_quantile(0.95, sum by (le) (rate(dispatch_latency_seconds_bucket[5m]))) > 5
        for: 5m
        labels: { severity: critical, service: dispatch }
        annotations:
          summary: "Dispatch p95 latency above 5s"
          description: "p95 is {{ $value | humanizeDuration }} (SLO target is < 2s)."

      - alert: DispatchLatencySLOBreach
        expr: histogram_quantile(0.95, sum by (le) (rate(dispatch_latency_seconds_bucket[5m]))) > 2
        for: 10m
        labels: { severity: warning, service: dispatch }
        annotations:
          summary: "Dispatch p95 latency above 2s SLO"
          description: "p95 is {{ $value | humanizeDuration }}."

      - alert: DispatchErrorRateHigh
        expr: |
          sum(rate(dispatch_requests_total{result="error"}[5m]))
            / sum(rate(dispatch_requests_total[5m])) > 0.01
        for: 5m
        labels: { severity: critical, service: dispatch }
        annotations:
          summary: "Dispatch error rate above 1%"
          description: "Error ratio is {{ $value | humanizePercentage }}."

      - alert: DispatchDoubleBooking
        expr: increase(dispatch_double_booking_total[5m]) > 0
        labels: { severity: critical, service: dispatch }
        annotations:
          summary: "A driver was double-booked"
          description: "dispatch_double_booking_total must always be 0. Page immediately."

      - alert: DispatchNoDriverRatioHigh
        expr: |
          sum(rate(dispatch_no_driver_total[10m]))
            / sum(rate(dispatch_requests_total[10m])) > 0.10
        for: 10m
        labels: { severity: warning, service: dispatch }
        annotations:
          summary: "More than 10% of requests find no driver"
          description: "No-driver ratio is {{ $value | humanizePercentage }}. Tune this to your real baseline."

      - alert: DispatchThroughputDropped
        expr: sum(rate(dispatch_requests_total[5m])) < 1 and sum(rate(dispatch_requests_total[5m] offset 1h)) > 50
        for: 5m
        labels: { severity: warning, service: dispatch }
        annotations:
          summary: "Dispatch traffic dropped sharply versus 1h ago"

  - name: dispatch-pipeline
    rules:
      - alert: OutboxBacklogGrowing
        expr: max(outbox_pending) > 1000
        for: 5m
        labels: { severity: critical, service: dispatch }
        annotations:
          summary: "Outbox backlog above 1000"
          description: "Events are not reaching Kafka. Pending: {{ $value }}."

      - alert: KafkaConsumerLagHigh
        expr: sum by (group) (kafka_consumer_lag) > 10000
        for: 5m
        labels: { severity: warning, service: kafka }
        annotations:
          summary: "Kafka consumer group {{ $labels.group }} is lagging"
          description: "Lag is {{ $value }} messages."

      - alert: PushFailuresHigh
        expr: sum(rate(push_failures_total[5m])) > 5
        for: 5m
        labels: { severity: warning, service: notifier }
        annotations:
          summary: "Push notification failures above 5/s"

      - alert: LocationIngestDropped
        expr: sum(rate(driver_location_updates_total[5m])) < 1000
        for: 5m
        labels: { severity: warning, service: location }
        annotations:
          summary: "Driver location updates far below the ~10k/s expected"
          description: "Tune the threshold to your real driver count."

  - name: infrastructure
    rules:
      - alert: ServiceDown
        expr: up{job=~"gateway|dispatch|location|notifier|timeout-worker"} == 0
        for: 1m
        labels: { severity: critical }
        annotations:
          summary: "{{ $labels.service }} is down"

      - alert: RedisDown
        expr: redis_up == 0
        for: 1m
        labels: { severity: critical, service: redis }
        annotations:
          summary: "Redis is unreachable (matching cannot reserve drivers)"

      - alert: MongoDown
        expr: mongodb_up == 0
        for: 1m
        labels: { severity: critical, service: mongodb }
        annotations:
          summary: "MongoDB is unreachable (ride records cannot be persisted)"

      - alert: KafkaDown
        expr: kafka_brokers < 1
        for: 2m
        labels: { severity: critical, service: kafka }
        annotations:
          summary: "No Kafka brokers visible"
```

**`deploy/observability/alertmanager/alertmanager.yml`**
```yaml
route:
  receiver: default
  group_by: [alertname, service]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 3h
  routes:
    - matchers: [severity="critical"]
      receiver: oncall
      repeat_interval: 1h

receivers:
  - name: default
    slack_configs:
      - api_url_file: /etc/alertmanager/secrets/slack_url
        channel: "#dispatch-alerts"
        send_resolved: true
  - name: oncall
    slack_configs:
      - api_url_file: /etc/alertmanager/secrets/slack_url
        channel: "#dispatch-oncall"
        send_resolved: true

inhibit_rules:
  - source_matchers: [alertname="ServiceDown"]
    target_matchers: [severity="warning"]
    equal: [service]
```

**`deploy/observability/grafana/provisioning/datasources/prometheus.yml`**
```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    uid: prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false
    jsonData:
      timeInterval: 15s
```

**`deploy/observability/grafana/provisioning/dashboards/dashboards.yml`**
```yaml
apiVersion: 1
providers:
  - name: dispatch
    orgId: 1
    folder: Ride Dispatch
    type: file
    disableDeletion: true
    allowUiUpdates: false
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
```

**`deploy/observability/docker-compose.observability.yml`**
```yaml
# Run alongside the main local stack: both must share the "dispatch-net" network.
#   docker network create dispatch-net   (once)
#   make obs-up
name: dispatch-observability

services:
  prometheus:
    image: prom/prometheus:v2.53.0
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --storage.tsdb.retention.time=15d
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./prometheus/alerts.yml:/etc/prometheus/alerts.yml:ro
      - prometheus-data:/prometheus
    ports: ["127.0.0.1:9090:9090"]
    networks: [dispatch-net]
    restart: unless-stopped

  alertmanager:
    image: prom/alertmanager:v0.27.0
    volumes:
      - ./alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro
      - ./alertmanager/secrets:/etc/alertmanager/secrets:ro
    ports: ["127.0.0.1:9093:9093"]
    networks: [dispatch-net]
    restart: unless-stopped

  grafana:
    image: grafana/grafana:11.1.0
    environment:
      GF_SECURITY_ADMIN_USER: admin
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_ADMIN_PASSWORD:?set GRAFANA_ADMIN_PASSWORD in .env}
      GF_USERS_ALLOW_SIGN_UP: "false"
      GF_AUTH_ANONYMOUS_ENABLED: "false"
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    ports: ["127.0.0.1:3000:3000"]
    depends_on: [prometheus]
    networks: [dispatch-net]
    restart: unless-stopped

  redis-exporter:
    image: oliver006/redis_exporter:v1.62.0
    environment:
      REDIS_ADDR: ${REDIS_EXPORTER_ADDR:-redis://redis:6379}
      REDIS_PASSWORD: ${REDIS_EXPORTER_PASSWORD:-}
    networks: [dispatch-net]
    restart: unless-stopped

  mongodb-exporter:
    image: percona/mongodb_exporter:0.40.0
    environment:
      MONGODB_URI: ${MONGODB_EXPORTER_URI:?set MONGODB_EXPORTER_URI in .env}
    command: ["--compatible-mode", "--collect-all"]
    networks: [dispatch-net]
    restart: unless-stopped

  kafka-exporter:
    image: danielqsj/kafka-exporter:v1.8.0
    command: ["--kafka.server=${KAFKA_EXPORTER_BROKER:-kafka:9092}"]
    networks: [dispatch-net]
    restart: unless-stopped

volumes:
  prometheus-data:
  grafana-data:

networks:
  dispatch-net:
    external: true
```

**Grafana dashboard:** `deploy/observability/grafana/dashboards/dispatch-overview.json` (uid `dispatch-overview`).

| Row | Panels |
|---|---|
| Headline | Requests/sec, p95 latency (orange > 2s, red > 5s), error rate (orange > 1%), double bookings 24h (red if >= 1) |
| Dispatch | Throughput by result, latency p50/p95/p99, no-driver and reassignments |
| Location | Location updates/sec, active drivers |
| Pipeline | Kafka consumer lag, outbox pending, push failures |
| Health | Services up, goroutines by service |

**Kubernetes:** with kube-prometheus-stack, use `deploy/k8s/servicemonitor.yaml`
instead of static targets. Each app Service exposes a port named `metrics` (9100)
and carries the label `app.kubernetes.io/part-of: ride-dispatch`. Convert the
groups in `alerts.yml` into a `PrometheusRule` resource, and keep Grafana
provisioning identical (mount the dashboard JSON via a ConfigMap).

**CI checks for observability** (`make obs-check`):
```bash
docker run --rm -v "$PWD/deploy/observability/prometheus:/p" --entrypoint promtool prom/prometheus:v2.53.0 check config /p/prometheus.yml
docker run --rm -v "$PWD/deploy/observability/prometheus:/p" --entrypoint promtool prom/prometheus:v2.53.0 check rules /p/alerts.yml
docker run --rm -v "$PWD/deploy/observability/alertmanager:/a" --entrypoint amtool prom/alertmanager:v0.27.0 check-config /a/alertmanager.yml
python3 -m json.tool deploy/observability/grafana/dashboards/dispatch-overview.json > /dev/null
```

---

## 13. Coding conventions

- Go formatted with `gofmt`/`goimports`; pass `golangci-lint` with the repo config.
- Always accept `context.Context` as the first parameter; honor cancellation.
- Wrap errors with `%w`; define typed/sentinel errors in `domain/errors.go`.
- Return errors, do not `panic` in request paths. No `log.Fatal` outside `main`.
- Small interfaces, defined where they are consumed.
- Constructors take dependencies explicitly (dependency injection); no `init()` side effects.
- Config via env only, loaded once in `main`, validated at startup (fail fast).
- Time: inject a clock interface so tests are deterministic. Use UTC everywhere.
- Money/ratings: avoid floats where exactness matters.
- Keep functions short; avoid goroutines without an owner, a `context`, and a shutdown path.
- Public API changes require updating `api/openapi.yaml` or `api/proto/` first.

### Testing requirements
- Unit tests for matcher, state machine, idempotency, authz rules (table-driven).
- **Concurrency test (mandatory):** N goroutines race for one driver; exactly one reservation wins. Run with `-race` and high `-count`.
- Integration tests against real Redis/Mongo/Kafka (testcontainers), not mocks, for `store/` and `events/`.
- Authorization tests: rider cannot read another rider's ride; driver cannot respond to a ride not assigned to them; token with wrong `aud`/`alg`/expired is rejected.
- Load test with k6 proves the 5,000 req/s / p95 < 2s target before release.
- Every bug fix ships with a regression test.

### Git and PRs
- Small, focused commits. Conventional commit messages (`feat:`, `fix:`, `test:`, `chore:`, `docs:`).
- One logical change per PR; include what changed, why, and how it was tested.
- Never force-push to `main`. Never commit generated secrets, `.env`, or build artifacts.
- Architecture-impacting decisions get an ADR in `docs/adr/`.

---

## 14. DO

- DO read this file and the relevant package before editing.
- DO derive caller identity from the verified JWT, and check object-level ownership.
- DO make reservation and state transitions atomic and conditional.
- DO persist the ride (and outbox event) before notifying a driver.
- DO use idempotency keys on ride creation and make Kafka consumers idempotent.
- DO put timeouts, retries with backoff, and circuit breakers on every external call.
- DO write the concurrency/double-booking test first when touching matching or locks.
- DO use interfaces at package boundaries and inject dependencies.
- DO keep `domain` pure and dependency-free.
- DO add metrics, structured logs, and trace spans to any new flow, and update `alerts.yml` and the Grafana dashboard JSON in the same PR.
- DO validate and bound all inputs (coordinates, sizes, enums, rates).
- DO run `make fmt lint vet test sec` before declaring done.
- DO update `api/`, docs, and this file when behavior or conventions change.
- DO ask when requirements are ambiguous, rather than guessing in security-sensitive code.

## 15. DO NOT

- DO NOT trust `riderID`/`driverID`/roles from the request body or query string.
- DO NOT skip authorization because "the gateway already checked."
- DO NOT write ride `status` outside the state machine, or update without a version/status condition.
- DO NOT use Redis as the only record of a ride. Mongo is the source of truth.
- DO NOT notify a driver before the ride is persisted.
- DO NOT release a lock without verifying you still own it (compare-and-delete).
- DO NOT use `KEYS *`, unbounded queries, or full-collection scans on hot paths.
- DO NOT log secrets, tokens, push tokens, phone numbers, or raw coordinates at INFO.
- DO NOT hardcode secrets, URLs, credentials, or keys. No secrets in git, ever.
- DO NOT use HS256 shared-secret JWTs, accept `alg: none`, or skip `aud`/`iss`/`exp` checks.
- DO NOT use `math/rand` for security-relevant values.
- DO NOT put high-cardinality IDs in metric labels.
- DO NOT expose Prometheus, Alertmanager, or Grafana publicly, enable anonymous Grafana access, or commit webhook URLs and passwords.
- DO NOT edit provisioned dashboards only in the Grafana UI; export the JSON and commit it.
- DO NOT add global mutable state, `init()` side effects, or goroutines with no shutdown path.
- DO NOT call external services without a `context` deadline.
- DO NOT retry non-idempotent operations blindly.
- DO NOT disable the race detector, skip failing tests, or weaken a security check to make a build pass.
- DO NOT add dependencies casually, or pull in a framework that replaces the architecture above.
- DO NOT expose `/metrics`, `/debug`, or internal gRPC publicly.
- DO NOT run containers as root or with privileged settings.
- DO NOT run destructive scripts against production, or commit `.env`, keys, dumps, or binaries.
- DO NOT rewrite large areas, rename packages, or change the architecture without an ADR and approval.

---

## 16. Environment variables (names only; values come from the secret manager)

```
APP_ENV=local|staging|prod
HTTP_ADDR=:8080
GRPC_ADDR=:9090
LOG_LEVEL=info
METRICS_ADDR=:9100          # internal-only /metrics listener, never public

JWT_ISSUER=
JWT_AUDIENCE=
JWKS_URL=
JWT_ALLOWED_ALGS=RS256,ES256

REDIS_ADDRS=
REDIS_USERNAME=
REDIS_PASSWORD=
REDIS_TLS=true

MONGO_URI=
MONGO_DB=dispatch

KAFKA_BROKERS=
KAFKA_SASL_USERNAME=
KAFKA_SASL_PASSWORD=
KAFKA_TLS=true

FCM_CREDENTIALS_FILE=
APNS_KEY_ID=
APNS_TEAM_ID=
APNS_KEY_FILE=

DISPATCH_CANDIDATES=10
DRIVER_LOCK_TTL=15s
OFFER_TIMEOUT=15s
MAX_REASSIGN_ATTEMPTS=3
LOCATION_STALE_AFTER=30s
RATE_LIMIT_RIDE_REQUEST_PER_MIN=
OTEL_EXPORTER_OTLP_ENDPOINT=
```

---

## 17. Suggested build order for agents

1. `domain` types, errors, and the ride state machine (+ unit tests).
2. `store/redis` geo, locks, idempotency (+ integration tests).
3. `dispatch` matcher + reservation + the **double-booking concurrency test**.
4. `dispatch/service.go` request flow, persistence, outbox.
5. Accept / reject / timeout flow and the timeout worker.
6. `location` service and region sharding.
7. `gateway`: JWT auth, authz, idempotency, rate limiting.
8. `events` (outbox relay, Kafka producer/consumers).
9. `notify` (FCM/APNs) with retry + circuit breaker.
10. Observability: metrics, logs, tracing, dashboards, alerts.
11. Docker/K8s manifests, CI pipeline, security scans.
12. k6 load test to prove 5,000 req/s and p95 < 2s.

---

## 18. When unsure

Stop and ask. Prefer the smaller, safer change. If a requirement here conflicts
with a task, flag the conflict instead of silently picking one. Security and
correctness of driver assignment outrank speed of delivery.
