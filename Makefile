GO ?= go
COMPOSE := docker compose -f deploy/compose/compose.yaml
TELEGRAM_COMPOSE := $(COMPOSE) -f deploy/compose/telegram-polling.yaml

.PHONY: build test test-race vet fmt check dev-up dev-up-telegram dev-down dev-logs demo-active-sessions demo-telegram-flow create-invite

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
	$(TELEGRAM_COMPOSE) --profile telegram up --build -d

dev-down:
	$(COMPOSE) --profile telegram down

dev-logs:
	$(COMPOSE) logs -f rw-bot rw-active-sessions

demo-active-sessions:
	$(COMPOSE) --profile tools run --rm --build rw-demo-active-sessions
	$(COMPOSE) exec -T sessions-db psql -U rw_sessions -d rw_sessions -c "SELECT status, count(*) FROM active_sessions GROUP BY status ORDER BY status;"
	$(COMPOSE) exec -T sessions-db psql -U rw_sessions -d rw_sessions -c "SELECT status, count(*) FROM activation_verifications GROUP BY status ORDER BY status;"

create-invite:
	$(COMPOSE) --profile tools run --rm --no-deps --build rw-create-invite

demo-telegram-flow:
	$(COMPOSE) --profile telegram up --build -d rw-fake-topup
	$(COMPOSE) --profile tools run --rm --no-deps --build rw-demo-telegram-flow
