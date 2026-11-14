GO ?= go
COMPOSE ?= $(shell command -v docker >/dev/null 2>&1 && echo docker compose || echo podman compose) -f deploy/compose/docker-compose.yml
DATABASE_URL ?= postgres://oms:oms@localhost:5432/oms?sslmode=disable

.PHONY: db-up db-down test migrate run build

db-up:
	$(COMPOSE) up -d postgres
	@until $(COMPOSE) exec -T postgres pg_isready -U oms -d oms >/dev/null 2>&1; do sleep 1; done
	@echo "postgres ready"

db-down:
	$(COMPOSE) down

test:
	TEST_DATABASE_URL='$(DATABASE_URL)' $(GO) test ./...

migrate:
	OMS_DATABASE_URL='$(DATABASE_URL)' $(GO) run ./cmd/oh-my-story migrate

run:
	OMS_DATABASE_URL='$(DATABASE_URL)' $(GO) run ./cmd/oh-my-story serve

build:
	CGO_ENABLED=0 $(GO) build -o bin/oh-my-story ./cmd/oh-my-story
