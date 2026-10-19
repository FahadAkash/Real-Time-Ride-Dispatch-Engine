.PHONY: up down obs-up obs-down obs-check run-all fmt lint vet test itest ctest load sec proto build docker migrate

SERVICES := gateway dispatch location notifier timeout-worker loadgen

up:
	docker compose -f deploy/docker/docker-compose.yml up -d

down:
	docker compose -f deploy/docker/docker-compose.yml down

obs-up:
	docker network inspect dispatch-net >/dev/null 2>&1 || docker network create dispatch-net
	docker compose -f deploy/observability/docker-compose.observability.yml up -d

obs-down:
	docker compose -f deploy/observability/docker-compose.observability.yml down

obs-check:
	docker run --rm -v "$(CURDIR)/deploy/observability/prometheus:/p" --entrypoint promtool prom/prometheus:v2.53.0 check config /p/prometheus.yml
	docker run --rm -v "$(CURDIR)/deploy/observability/prometheus:/p" --entrypoint promtool prom/prometheus:v2.53.0 check rules /p/alerts.yml
	docker run --rm -v "$(CURDIR)/deploy/observability/alertmanager:/a" --entrypoint amtool prom/alertmanager:v0.27.0 check-config /a/alertmanager.yml
	python3 -m json.tool deploy/observability/grafana/dashboards/dispatch-overview.json > /dev/null

run-all:
	@echo "Starting all services locally..."
	go run ./cmd/gateway &
	go run ./cmd/dispatch &
	go run ./cmd/location &
	go run ./cmd/notifier &
	go run ./cmd/timeout-worker

fmt:
	go fmt ./...
	which goimports >/dev/null 2>&1 && goimports -w . || true

lint:
	which golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || go vet ./...

vet:
	go vet ./...

test:
	go test -race -count=1 ./internal/...

itest:
	go test -v -race -timeout 300s ./test/integration/...

ctest:
	go test -v -race -count=20 -timeout 300s ./test/concurrency/...

load:
	k6 run test/load/dispatch.js

sec:
	which gosec >/dev/null 2>&1 && gosec -quiet ./... || true
	which govulncheck >/dev/null 2>&1 && govulncheck ./... || true
	which gitleaks >/dev/null 2>&1 && gitleaks detect --no-git -v || true
	which trivy >/dev/null 2>&1 && trivy fs --security-checks vuln,config . || true

proto:
	which buf >/dev/null 2>&1 && buf generate || true

build:
	@mkdir -p bin
	@for svc in $(SERVICES); do \
		echo "Building $$svc..."; \
		go build -trimpath -ldflags="-s -w" -o bin/$$svc ./cmd/$$svc; \
	done

docker:
	@for svc in $(SERVICES); do \
		echo "Building docker image for $$svc..."; \
		docker build -t ride-dispatch/$$svc:latest -f deploy/docker/Dockerfile.$$svc .; \
	done

migrate:
	go run scripts/create_indexes.go
	bash scripts/create_topics.sh

