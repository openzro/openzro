# openZro · top-level Makefile
#
# `make help` lists every target with a short description.
#
# Conventions:
#   - Targets are namespaced with `.` (e.g. test.go) so tab-completion is
#     deterministic. Short top-level aliases (test, build, lint, fmt) call
#     into the namespaced ones.
#   - Anything that touches the network or starts a service is gated
#     behind a target name that says so explicitly (dev.up, ha.up).
#   - The dashboard targets shell out to npm; everything else is plain Go.

.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

GO            ?= go
GOFLAGS       ?=
GOFMT         ?= gofmt
GOLANGCI_LINT ?= golangci-lint
NPM           ?= npm
DOCKER        ?= docker
DOCKER_COMPOSE ?= docker compose

DASHBOARD_DIR := dashboard

# Coverage threshold for `make coverage` to be happy.
COVERAGE_MIN ?= 60

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk 'BEGIN{FS=":.*?## "; printf "\nUsage: make \033[36m<target>\033[0m\n\nTargets:\n"} \
	     /^[a-zA-Z0-9_.-]+:.*?## / {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

.PHONY: build build.go build.dashboard
build: build.go ## Build everything (Go core; dashboard requires `make build.dashboard`)

build.go: ## go build ./... — verifies the whole module compiles
	$(GO) build $(GOFLAGS) ./...

build.dashboard: ## Production build of the Next.js dashboard
	cd $(DASHBOARD_DIR) && $(NPM) ci && $(NPM) run build

build.management: ## Build the management server binary
	$(GO) build $(GOFLAGS) -o management/management ./management

dev.dashboard: dev.idp.up dev.management.up ## Full dev stack: Zitadel + management + dashboard. http://localhost:3000
	cd $(DASHBOARD_DIR) && $(NPM) install && $(NPM) run dev

dev.dashboard.turbo: dev.idp.up dev.management.up ## Same as dev.dashboard but with Turbopack
	cd $(DASHBOARD_DIR) && $(NPM) install && $(NPM) run turbo

dev.dashboard.bare: ## Run dashboard without IdP/management (use when pointing at external services)
	cd $(DASHBOARD_DIR) && $(NPM) install && $(NPM) run dev

lint.dashboard: ## ESLint over the dashboard sources
	cd $(DASHBOARD_DIR) && $(NPM) install && $(NPM) run lint

# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------

.PHONY: test test.go test.dashboard test.short
test: test.go ## Run Go tests; dashboard tests via test.dashboard

test.go: ## go test ./... with race detector and atomic coverage mode
	$(GO) test $(GOFLAGS) -race -count=1 -covermode=atomic -coverprofile=coverage.out ./...

test.short: ## go test -short (skips integration tests that need real services)
	$(GO) test $(GOFLAGS) -short -count=1 ./...

test.dashboard: ## Cypress E2E + lint pass for the dashboard
	cd $(DASHBOARD_DIR) && $(NPM) ci && $(NPM) run lint && $(NPM) run cypress:open

coverage: test.go ## Print coverage and fail if below COVERAGE_MIN%
	@$(GO) tool cover -func=coverage.out | tail -1 | awk '{print "coverage:", $$3}'
	@$(GO) tool cover -func=coverage.out | tail -1 | awk -v min=$(COVERAGE_MIN) \
	    '{cov=$$3; gsub(/%/, "", cov); if (cov+0 < min) {printf "FAIL: coverage %s%% below threshold %d%%\n", cov, min; exit 1} else {printf "OK: coverage %s%% >= %d%%\n", cov, min}}'

# ---------------------------------------------------------------------------
# Lint / format / static analysis
# ---------------------------------------------------------------------------

.PHONY: lint fmt vet tidy fmt.check
lint: ## golangci-lint run ./... (requires golangci-lint installed)
	$(GOLANGCI_LINT) run ./...

fmt: ## Format Go sources in place (gofmt + goimports if available)
	$(GOFMT) -s -w .
	@command -v goimports >/dev/null && goimports -w . || true

fmt.check: ## Fail if any Go file is not gofmt-clean (use in CI)
	@diff_out=$$($(GOFMT) -l .); \
	if [ -n "$$diff_out" ]; then \
	  echo "gofmt issues in:"; echo "$$diff_out"; exit 1; \
	fi

vet: ## go vet ./...
	$(GO) vet ./...

tidy: ## go mod tidy
	$(GO) mod tidy

# ---------------------------------------------------------------------------
# Local dev: dependencies for HA mode (Postgres + Valkey + NATS)
# ---------------------------------------------------------------------------

.PHONY: dev.deps.up dev.deps.down dev.deps.logs
dev.deps.up: ## Start local Postgres + Valkey + NATS containers for HA testing
	$(DOCKER_COMPOSE) -f deploy/dev-deps.compose.yml up -d
	@echo ""
	@echo "Local HA dependencies are up. Useful env vars:"
	@echo "  export OPENZRO_STORE_ENGINE=postgres"
	@echo "  export OPENZRO_STORE_ENGINE_POSTGRES_DSN=postgres://openzro:openzro@localhost:5432/openzro?sslmode=disable"
	@echo "  export OPENZRO_REDIS_URL=valkey://localhost:6379/0"
	@echo "  # — or —"
	@echo "  export OPENZRO_NATS_URL=nats://localhost:4222"
	@echo ""

dev.deps.down: ## Stop and remove local Postgres + Valkey + NATS containers
	$(DOCKER_COMPOSE) -f deploy/dev-deps.compose.yml down -v

dev.deps.logs: ## Tail logs from the local dev dependencies
	$(DOCKER_COMPOSE) -f deploy/dev-deps.compose.yml logs -f --tail=100

# ---------------------------------------------------------------------------
# Local dev: IdP (Zitadel) for dashboard auth
# ---------------------------------------------------------------------------

.PHONY: dev.idp.up dev.idp.down dev.idp.logs dev.idp.reset
dev.idp.up: ## Bring up local Zitadel and provision an OIDC app for the dashboard
	$(DOCKER_COMPOSE) -f deploy/dev-idp.compose.yml up -d
	@bash deploy/dev-idp/provision.sh

dev.idp.down: ## Stop the local Zitadel (keeps volume so subsequent ups are fast)
	$(DOCKER_COMPOSE) -f deploy/dev-idp.compose.yml down

dev.idp.logs: ## Tail Zitadel logs
	$(DOCKER_COMPOSE) -f deploy/dev-idp.compose.yml logs -f --tail=100

dev.idp.reset: ## Wipe the IdP volume and force a fresh provisioning on next dev.idp.up
	$(DOCKER_COMPOSE) -f deploy/dev-idp.compose.yml down -v
	rm -rf deploy/dev-idp/machinekey/*

# ---------------------------------------------------------------------------
# Local dev: management server (foreground binary, sqlite store, IdP=none)
# ---------------------------------------------------------------------------

MGMT_BIN     := management/management
MGMT_PIDFILE := /tmp/openzro-mgmt.pid
MGMT_LOGFILE := /tmp/openzro-mgmt.log
MGMT_DATADIR := /tmp/openzro-mgmt-data
MGMT_CONFIG  := deploy/dev-mgmt/management.json

.PHONY: dev.management.up dev.management.down dev.management.logs dev.management.status
dev.management.up: build.management ## Start the management server in the background (HTTP :33071)
	@if [ ! -f $(MGMT_CONFIG) ]; then \
	  echo "ERROR: $(MGMT_CONFIG) missing. Run 'make dev.idp.up' first."; exit 1; \
	fi
	@if [ -f $(MGMT_PIDFILE) ] && kill -0 $$(cat $(MGMT_PIDFILE)) 2>/dev/null; then \
	  echo "management already running (pid $$(cat $(MGMT_PIDFILE)))"; \
	else \
	  mkdir -p $(MGMT_DATADIR); \
	  ./$(MGMT_BIN) management \
	    --config $(MGMT_CONFIG) \
	    --datadir $(MGMT_DATADIR) \
	    --port 33071 \
	    --log-file $(MGMT_LOGFILE) \
	    --log-level info \
	    --disable-anonymous-metrics \
	    --disable-geolite-update \
	    >>$(MGMT_LOGFILE) 2>&1 & \
	  echo $$! > $(MGMT_PIDFILE); \
	  sleep 1; \
	  if ! kill -0 $$(cat $(MGMT_PIDFILE)) 2>/dev/null; then \
	    echo "management failed to start. Last 20 log lines:"; tail -20 $(MGMT_LOGFILE); exit 1; \
	  fi; \
	  echo "management started (pid $$(cat $(MGMT_PIDFILE))). Logs: $(MGMT_LOGFILE)"; \
	fi

dev.management.down: ## Stop the management server
	@if [ -f $(MGMT_PIDFILE) ]; then \
	  kill $$(cat $(MGMT_PIDFILE)) 2>/dev/null && echo "management stopped" || echo "management was not running"; \
	  rm -f $(MGMT_PIDFILE); \
	else \
	  echo "no management PID file"; \
	fi

dev.management.logs: ## Tail management server logs
	@tail -f $(MGMT_LOGFILE)

dev.management.status: ## Check whether management is running
	@if [ -f $(MGMT_PIDFILE) ] && kill -0 $$(cat $(MGMT_PIDFILE)) 2>/dev/null; then \
	  echo "running (pid $$(cat $(MGMT_PIDFILE)))"; \
	else \
	  echo "stopped"; \
	fi

# ---------------------------------------------------------------------------
# Stop everything
# ---------------------------------------------------------------------------

.PHONY: stop dev.down
stop: dev.down ## Alias for dev.down — stop every container started by `make dev.*`

dev.down: dev.management.down ## Stop management + dev IdP + dev deps + HA cluster (whichever are running)
	-$(DOCKER_COMPOSE) -f deploy/dev-idp.compose.yml down
	-$(DOCKER_COMPOSE) -f deploy/dev-deps.compose.yml down
	-$(DOCKER_COMPOSE) -f deploy/ha-local.compose.yml down 2>/dev/null || true

# ---------------------------------------------------------------------------
# Local dev: HA cluster (2 management + 2 signal + deps)
# ---------------------------------------------------------------------------

.PHONY: ha.up ha.down ha.logs
ha.up: ## Start a 2-node HA cluster locally for smoke testing
	$(DOCKER_COMPOSE) -f deploy/ha-local.compose.yml up -d --build
	@echo ""
	@echo "HA cluster up: management at :33071/:33072, signal at :10000/:10001"
	@echo ""

ha.down: ## Tear the local HA cluster down (keeps volumes)
	$(DOCKER_COMPOSE) -f deploy/ha-local.compose.yml down

ha.logs: ## Tail logs from the HA cluster
	$(DOCKER_COMPOSE) -f deploy/ha-local.compose.yml logs -f --tail=100

# ---------------------------------------------------------------------------
# Clean
# ---------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artefacts, coverage output, and the dashboard's .next dir
	rm -f coverage.out
	rm -f management/management signal/signal relay/relay client/client
	rm -rf $(DASHBOARD_DIR)/.next $(DASHBOARD_DIR)/out
	rm -f $(DASHBOARD_DIR)/.local-config.json
	rm -f deploy/dev-idp/machinekey/zitadel-admin-sa.token
	rm -f deploy/dev-idp/machinekey/provisioned.json
