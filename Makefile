GO ?= go
COMPOSE ?= docker compose

.PHONY: run test test-integration lint migrate-up migrate-down generate docker-up docker-down

run:
	$(GO) build -o bin/server ./cmd/server
	./bin/server

test:
	$(GO) test -race ./...

test-integration:
	@test -n "$$TEST_DATABASE_URL" || { echo 'Set TEST_DATABASE_URL to a local PostgreSQL role with CREATEDB'; exit 1; }
	$(GO) test -race -tags=integration -count=1 ./...

lint:
	@test -z "$$(gofmt -l cmd internal migrations)" || { gofmt -l cmd internal migrations; exit 1; }
	$(GO) vet ./...

migrate-up:
	$(GO) run ./cmd/migrate up

migrate-down:
	$(GO) run ./cmd/migrate down

# No generators yet; this runs registered go:generate directives when introduced.
generate:
	$(GO) generate ./...

docker-up:
	$(COMPOSE) up -d --wait postgres
	$(COMPOSE) run --rm --no-deps --build backend /app/migrate up
	$(COMPOSE) up -d --wait backend

# Preserve the database volume. Destructive deletion is a separate manual action.
docker-down:
	$(COMPOSE) down
