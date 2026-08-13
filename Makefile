GO ?= go
COMPOSE := docker compose -f deploy/compose/compose.yaml
TELEGRAM_COMPOSE := $(COMPOSE) -f deploy/compose/telegram-polling.yaml

.PHONY: build test test-race vet fmt check dev-up dev-up-telegram dev-down dev-logs

build:
	$(GO) build ./cmd/...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

check: fmt vet test build

dev-up:
	$(COMPOSE) up --build -d

dev-up-telegram:
	$(TELEGRAM_COMPOSE) up --build -d

dev-down:
	$(COMPOSE) down

dev-logs:
	$(COMPOSE) logs -f rw-bot rw-active-sessions
