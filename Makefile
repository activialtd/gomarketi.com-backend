# GoMarket Backend — top-level Makefile
# Always run from the repo root (gomarketi.com-backend/).

.PHONY: help docker-up docker-down docker-logs migrate gen tidy test \
        test-verbose test-cover gen-keys

SERVICES     := auth identity catalogue orders storefront
GO_SERVICES  := $(addprefix services/,$(SERVICES))

# ── Docker ─────────────────────────────────────────────────────────────────────

docker-up:
	@echo "→ Starting infra containers..."
	docker compose up -d

docker-down:
	@echo "→ Stopping infra containers..."
	docker compose down

docker-logs:
	docker compose logs -f

# ── Database ──────────────────────────────────────────────────────────────────

migrate:
	@echo "→ Running migrations..."
	go run ./scripts/migrate

# ── Code generation ───────────────────────────────────────────────────────────

gen:
	@bash scripts/gen/gen.sh

# ── Dependency management ─────────────────────────────────────────────────────

tidy:
	@go work sync
	@for dir in shared/pkg scripts/migrate $(GO_SERVICES); do \
		if [ -f $$dir/go.mod ]; then \
			echo "→ go mod tidy: $$dir"; \
			(cd $$dir && go mod tidy); \
		fi; \
	done

# ── Testing ───────────────────────────────────────────────────────────────────

test:
	go test ./...

test-verbose:
	go test -v -count=1 ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

# ── Build ─────────────────────────────────────────────────────────────────────

# Usage: make build-auth  |  make build-identity  etc.
build-%:
	@echo "→ Building $*..."
	@mkdir -p bin
	go build -o bin/$* ./services/$*/cmd/server

# ── Keys ──────────────────────────────────────────────────────────────────────

# Generate RSA-2048 key pair for JWT (RS256).
# Run once. Add the paths to .env. NEVER commit the key files.
gen-keys:
	@mkdir -p keys
	openssl genrsa -out keys/private.pem 2048
	openssl rsa -in keys/private.pem -pubout -out keys/public.pem
	@echo ""
	@echo "Keys written to keys/"
	@echo "Add to .env:"
	@echo "  JWT_PRIVATE_KEY_PATH=./keys/private.pem"
	@echo "  JWT_PUBLIC_KEY_PATH=./keys/public.pem"
	@echo ""
	@echo "WARNING: keys/ is gitignored. Back these up securely."

# ── Help ──────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  GoMarket Backend"
	@echo ""
	@echo "  docker-up       Start all infra containers"
	@echo "  docker-down     Stop all infra containers"
	@echo "  docker-logs     Follow container logs"
	@echo "  migrate         Run pending DB migrations  (needs DATABASE_URL)"
	@echo "  gen             Run sqlc generate for all services"
	@echo "  tidy            go work sync + go mod tidy for all modules"
	@echo "  test            Run all tests"
	@echo "  test-cover      Run tests and open HTML coverage report"
	@echo "  build-<svc>     Build service binary  (e.g. make build-auth)"
	@echo "  gen-keys        Generate RS256 key pair for JWT signing"
	@echo ""

.DEFAULT_GOAL := help
