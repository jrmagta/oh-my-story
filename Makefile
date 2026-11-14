GO ?= go
COMPOSE ?= $(shell command -v docker >/dev/null 2>&1 && echo docker compose || echo podman compose) -f deploy/compose/docker-compose.yml
DATABASE_URL ?= postgres://oms:oms@localhost:5432/oms?sslmode=disable

E2E_DATABASE_URL ?= postgres://oms:oms@localhost:5432/oms_e2e?sslmode=disable

.PHONY: up down ps db-up db-down test migrate run build web e2e

# Full dev stack: postgres, migrate, app, keycloak, mailpit.
up:
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

ps:
	$(COMPOSE) ps

db-up:
	$(COMPOSE) up -d postgres
	@until $(COMPOSE) exec -T postgres pg_isready -U oms -d oms >/dev/null 2>&1; do sleep 1; done
	@echo "postgres ready"

db-down:
	$(COMPOSE) down

# Needs the stack from `make up`: Postgres, plus Keycloak and Mailpit for the SSO and SMTP tests.
test:
	TEST_DATABASE_URL='$(DATABASE_URL)' \
	TEST_OIDC_ISSUER=http://localhost:8180/realms/oms \
	TEST_SMTP_ADDR=localhost:1025 TEST_MAILPIT_URL=http://localhost:8025 \
	$(GO) test ./...

migrate:
	OMS_DATABASE_URL='$(DATABASE_URL)' $(GO) run ./cmd/oh-my-story migrate

run:
	OMS_DATABASE_URL='$(DATABASE_URL)' $(GO) run ./cmd/oh-my-story serve

web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch $@

# The web app, built into web/dist for the binary to embed.
web: web/node_modules
	cd web && npm run build

build: web
	CGO_ENABLED=0 $(GO) build -o bin/oh-my-story ./cmd/oh-my-story

# Browser tests: the built binary on localhost:18080 with a fresh database,
# against the compose Keycloak and Mailpit (make up).
e2e: build
	$(COMPOSE) exec -T postgres psql -q -U oms -d oms -c 'DROP DATABASE IF EXISTS oms_e2e WITH (FORCE)' -c 'CREATE DATABASE oms_e2e'
	OMS_DATABASE_URL='$(E2E_DATABASE_URL)' bin/oh-my-story migrate
	cd web && E2E_DATABASE_URL='$(E2E_DATABASE_URL)' npx playwright test
