# ==============================================================================
# RTVortex API Server — Makefile
# ==============================================================================

BINARY      := RTVortexGo
MODULE      := github.com/AuralithAI/rtvortex-server
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO          := $(shell which go 2>/dev/null || echo "/usr/local/go/bin/go")
GOFLAGS     := -trimpath
LDFLAGS     := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

# ── Directories ──────────────────────────────────────────────────────────────
CMD_DIR     := ./cmd/rtvortex-server
RT_HOME     := ../../rt_home
PROTO_DIR   := ../proto
PROTO_OUT   := ./internal/engine/pb

.PHONY: all build run test lint clean proto deps docker help \
        db-create db-init db-drop db-status db-reset db-fix-owner db-install \
        db-migrate db-fix-migrate

all: deps build ## Build everything

# ── Build ────────────────────────────────────────────────────────────────────

build: ## Build the server binary
	@echo "Building $(BINARY) $(VERSION)..."
	@mkdir -p $(RT_HOME)/bin $(RT_HOME)/config $(RT_HOME)/data/sql $(RT_HOME)/data/migrations $(RT_HOME)/temp $(RT_HOME)/models
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RT_HOME)/bin/$(BINARY) $(CMD_DIR)
	@cp -f db/sql/*.sql $(RT_HOME)/data/sql/ 2>/dev/null || true
	@cp -f db/migrations/*.sql $(RT_HOME)/data/migrations/ 2>/dev/null || true
	@cp -n ../config/rtserverprops.xml $(RT_HOME)/config/ 2>/dev/null || true
	@echo "Built: $(RT_HOME)/bin/$(BINARY)"
	@ls -lh $(RT_HOME)/bin/$(BINARY)

build-linux: ## Cross-compile for Linux amd64
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RT_HOME)/bin/$(BINARY)-linux-amd64 $(CMD_DIR)

build-linux-arm: ## Cross-compile for Linux arm64
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RT_HOME)/bin/$(BINARY)-linux-arm64 $(CMD_DIR)

build-darwin: ## Cross-compile for macOS (arm64)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RT_HOME)/bin/$(BINARY)-darwin-arm64 $(CMD_DIR)

build-darwin-amd64: ## Cross-compile for macOS (amd64)
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RT_HOME)/bin/$(BINARY)-darwin-amd64 $(CMD_DIR)

build-windows: ## Cross-compile for Windows
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RT_HOME)/bin/$(BINARY)-windows-amd64.exe $(CMD_DIR)

build-all: build-linux build-linux-arm build-darwin build-darwin-amd64 build-windows ## Build for all platforms

# ── Run ──────────────────────────────────────────────────────────────────────

run: build ## Build and run the server
	RTVORTEX_HOME=$(abspath $(RT_HOME)) $(RT_HOME)/bin/$(BINARY)

dev: ## Run with hot reload (requires air: go install github.com/air-verse/air@latest)
	air -c .air.toml

# ── Dependencies ─────────────────────────────────────────────────────────────

deps: ## Download and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

# ── Testing ──────────────────────────────────────────────────────────────────

test: ## Run all tests
	$(GO) test -race -cover ./...

test-unit: ## Run unit tests (tests/ directory only)
	$(GO) test -race -cover -v ./tests/...

test-verbose: ## Run tests with verbose output
	$(GO) test -race -cover -v ./...

test-coverage: ## Generate coverage report
	@mkdir -p coverage
	$(GO) test -race -coverprofile=coverage/coverage.out ./...
	$(GO) tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report: coverage/coverage.html"

bench: ## Run benchmarks
	$(GO) test -bench=. -benchmem ./...

# ── Proto ────────────────────────────────────────────────────────────────────
# Uses the protoc built by the C++ engine's gRPC FetchContent (build/bin/protoc).
# Falls back to system protoc if the build protoc is not available.

PROTOC_BIN := $(shell ls ../../build/bin/protoc 2>/dev/null || echo "protoc")

proto: ## Generate Go code from proto files
	@mkdir -p $(PROTO_OUT)
	PATH="$(HOME)/go/bin:$(PATH)" $(PROTOC_BIN) --proto_path=$(PROTO_DIR) \
		--go_out=$(PROTO_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/engine.proto
	@echo "Proto generated in $(PROTO_OUT)"

# ── Lint ─────────────────────────────────────────────────────────────────────

lint: ## Run linters
	golangci-lint run ./...

fmt: ## Format code
	$(GO) fmt ./...
	goimports -w .

vet: ## Run go vet
	$(GO) vet ./...

# ── Docker ───────────────────────────────────────────────────────────────────

docker: ## Build Docker image
	docker build -t rtvortex-go:$(VERSION) -f Dockerfile .

docker-run: docker ## Run in Docker
	docker run --rm -p 8080:8080 rtvortex-go:$(VERSION)

# ── Database ─────────────────────────────────────────────────────────────────

db-create: ## Create the rtvortex database and role (run as postgres superuser)
	psql -U postgres -f db/sql/create_database.sql

db-init: ## Initialize schema tables (run after db-create)
	psql -U rtvortex -d rtvortex -f db/sql/initData.sql

db-status: ## Show current schema version
	psql -U rtvortex -d rtvortex -c "SELECT * FROM schema_info ORDER BY version;"

db-drop: ## Drop the rtvortex database (destructive!)
	psql -U postgres -c "DROP DATABASE IF EXISTS rtvortex;"

db-reset: db-drop db-create db-init ## Drop, recreate, and re-init the database

db-fix-owner: ## Transfer ownership of all DB objects to rtvortex
	psql -U postgres -d rtvortex -c " \
		DO \$$\$$ BEGIN \
			EXECUTE (SELECT string_agg('ALTER TABLE public.' || quote_ident(tablename) || ' OWNER TO rtvortex;', ' ') \
				FROM pg_tables WHERE schemaname = 'public' AND tableowner <> 'rtvortex'); \
			EXECUTE (SELECT coalesce(string_agg('ALTER SEQUENCE public.' || quote_ident(sequencename) || ' OWNER TO rtvortex;', ' '), '') \
				FROM pg_sequences WHERE schemaname = 'public' AND sequenceowner <> 'rtvortex'); \
		END \$$\$$; \
		ALTER SCHEMA public OWNER TO rtvortex; \
		ALTER DATABASE rtvortex OWNER TO rtvortex;"

db-install: db-create db-init ## Full database setup (create + init)

db-migrate: ## Apply all pending migrations via golang-migrate
	@echo "Applying migrations from db/migrations/ ..."
	@for f in db/migrations/*.up.sql; do \
		echo "  → $$f"; \
		psql -U rtvortex -d rtvortex -f "$$f" 2>&1 | grep -v 'already exists' || true; \
	done
	@echo "Done. Checking schema_migrations state:"
	@psql -U rtvortex -d rtvortex -c "SELECT * FROM schema_migrations;" 2>/dev/null || true

db-fix-migrate: ## Fix dirty schema_migrations state and re-apply all migrations
	@echo "Fixing schema_migrations dirty flag ..."
	psql -U postgres -d rtvortex -c " \
		DELETE FROM schema_migrations; \
		INSERT INTO schema_migrations (version, dirty) VALUES ( \
			(SELECT max(cast(left(s, 6) as int)) FROM ( \
				SELECT regexp_replace(f, '^0+', '') AS s FROM ( \
					SELECT split_part(pg_ls_dir, '_', 1) AS f \
					FROM pg_ls_dir('$(abspath db/migrations)') \
					WHERE pg_ls_dir LIKE '%.up.sql' \
				) sub \
			) sub2), false);"
	@echo "Re-applying all migrations ..."
	@for f in db/migrations/*.up.sql; do \
		echo "  → $$f"; \
		psql -U postgres -d rtvortex -f "$$f" 2>&1 | grep -v 'already exists' || true; \
	done
	@echo "Final state:"
	@psql -U rtvortex -d rtvortex -c "SELECT * FROM schema_migrations;"

# ── Clean ────────────────────────────────────────────────────────────────────

clean: ## Remove Go build artifacts from rt_home
	rm -f $(RT_HOME)/bin/$(BINARY) $(RT_HOME)/bin/$(BINARY)-*
	rm -rf coverage

# ── Help ─────────────────────────────────────────────────────────────────────

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
