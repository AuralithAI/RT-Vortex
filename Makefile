# ==============================================================================
# RTVortex — Unified Build Controller
# ==============================================================================
#
# Builds both components into a shared rt_home/ directory:
#
#   rt_home/
#   ├── bin/
#   │   ├── rtvortex        ← C++ engine (gRPC server, indexing, retrieval)
#   │   └── RTVortexGo      ← Go API server (REST, webhooks, auth)
#   ├── config/             ← XML configuration files
#   ├── data/sql/           ← PostgreSQL setup scripts
#   ├── models/             ← ONNX model weights
#   └── temp/               ← Logs + ephemeral scratch (RT_TEMP)
#
# Usage:
#   make              — build everything (C++ engine + Go server)
#   make engine       — build only the C++ engine
#   make server       — build only the Go server
#   make run          — build and run both (engine + server)
#   make clean        — remove rt_home/
#   make help         — show all targets
#
# ==============================================================================

# ── Paths ────────────────────────────────────────────────────────────────────
ROOT_DIR     := $(shell pwd)
RT_HOME      := $(ROOT_DIR)/rt_home
ENGINE_DIR   := $(ROOT_DIR)/mono/engine
SERVER_DIR   := $(ROOT_DIR)/mono/server-go
CONFIG_DIR   := $(ROOT_DIR)/mono/config
BUILD_DIR    := $(ROOT_DIR)/build
CLI_DIR      := $(ROOT_DIR)/mono/cli
SDK_PY_DIR   := $(ROOT_DIR)/mono/sdks/python
SDK_NODE_DIR := $(ROOT_DIR)/mono/sdks/node
SDK_JAVA_DIR := $(ROOT_DIR)/mono/sdks/java
WEB_DIR      := $(ROOT_DIR)/mono/web
GO           := $(shell which go 2>/dev/null || echo "/usr/local/go/bin/go")
PYTHON       := $(shell which python3 2>/dev/null || echo "python3")
NODE         := $(shell which node 2>/dev/null || echo "node")
NPM          := $(shell which npm 2>/dev/null || echo "npm")
MVN          := $(shell which mvn 2>/dev/null || echo "mvn")
NPROC        := $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
MODELS_SRC   := $(ENGINE_DIR)/models
ONNX_MODEL   := all-MiniLM-L6-v2.onnx
ONNX_URL     := https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx

# ── Version ──────────────────────────────────────────────────────────────────
VERSION      := $(shell cat mono/VERSION 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo "0.0.0")
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: all engine server config models clean clean-all run run-engine run-server run-web \
        test test-engine test-server db-create db-init db-install \
        cli sdk-python sdk-node sdk-java sdks \
        test-cli test-sdk-python test-sdk-node test-sdk-java test-sdks test-all \
        build-cli build-sdk-python build-sdk-node build-sdk-java build-sdks \
        web build-web test-web lint-web \
        proto proto-go \
        version status help rt_home

# ==============================================================================
# Top-level targets
# ==============================================================================

all: rt_home engine server build-web config models ## Build everything
	@echo ""
	@echo "════════════════════════════════════════════════════════"
	@echo "  RTVortex build complete — $(VERSION)"
	@echo "  rt_home/bin/rtvortex     (C++ engine)"
	@echo "  rt_home/bin/RTVortexGo   (Go API server)"
	@echo "  rt_home/webApps/         (Next.js dashboard)"
	@echo "════════════════════════════════════════════════════════"

# ── rt_home directory structure ──────────────────────────────────────────────

rt_home:
	@mkdir -p $(RT_HOME)/bin $(RT_HOME)/lib $(RT_HOME)/config \
	          $(RT_HOME)/data/sql $(RT_HOME)/temp $(RT_HOME)/models \
	          $(RT_HOME)/webApps

# ==============================================================================
# C++ Engine
# ==============================================================================

engine: rt_home ## Build the C++ RTVortex engine
	@echo ""
	@echo "──── Building C++ Engine ────────────────────────────────"
	@mkdir -p $(BUILD_DIR)
	cd $(BUILD_DIR) && cmake $(ENGINE_DIR) \
		-DCMAKE_BUILD_TYPE=Release \
		-DAIPR_BUILD_SERVER=ON \
		-DAIPR_BUILD_TESTS=OFF \
		-DAIPR_BUILD_BENCH=OFF \
		-DAIPR_BUILD_DOCTOR=OFF
	cd $(BUILD_DIR) && $(MAKE) -j$(NPROC) aipr-engine-server
	@cp -f $(BUILD_DIR)/bin/rtvortex $(RT_HOME)/bin/
	@echo "  rt_home/bin/rtvortex ($$(du -h $(RT_HOME)/bin/rtvortex | cut -f1))"

# ==============================================================================
# Go API Server
# ==============================================================================

server: rt_home proto-go ## Build the Go RTVortexGo API server
	@echo ""
	@echo "──── Building Go API Server ─────────────────────────────"
	cd $(SERVER_DIR) && $(GO) build -trimpath \
		-ldflags "-s -w \
			-X main.version=$(VERSION) \
			-X main.commit=$(COMMIT) \
			-X main.buildDate=$(BUILD_DATE)" \
		-o $(RT_HOME)/bin/RTVortexGo \
		./cmd/rtvortex-server/
	@cp -f $(SERVER_DIR)/db/sql/*.sql $(RT_HOME)/data/sql/ 2>/dev/null || true
	@echo "  rt_home/bin/RTVortexGo ($$(du -h $(RT_HOME)/bin/RTVortexGo | cut -f1))"

# ==============================================================================
# Configuration
# ==============================================================================

config: rt_home ## Copy configuration files to rt_home
	@cp -n $(CONFIG_DIR)/rtserverprops.xml $(RT_HOME)/config/ 2>/dev/null || true
	@cp -n $(CONFIG_DIR)/vcsplatforms.xml  $(RT_HOME)/config/ 2>/dev/null || true
	@cp -n $(CONFIG_DIR)/*.yml $(RT_HOME)/config/ 2>/dev/null || true
	@mkdir -p $(RT_HOME)/config/certificates
	@cp -n $(CONFIG_DIR)/certificates/* $(RT_HOME)/config/certificates/ 2>/dev/null || true
	@mkdir -p $(RT_HOME)/config/model-providers
	@cp -n $(CONFIG_DIR)/model-providers/* $(RT_HOME)/config/model-providers/ 2>/dev/null || true
	@echo "  rt_home/config/ updated (xml, yml, certificates, model-providers)"

# ==============================================================================
# Models (ONNX embeddings)
# ==============================================================================

models: rt_home ## Download ONNX model and copy to rt_home/models
	@if [ ! -f "$(MODELS_SRC)/$(ONNX_MODEL)" ]; then \
		echo "Downloading ONNX embedding model (all-MiniLM-L6-v2, ~87 MB)..."; \
		mkdir -p $(MODELS_SRC); \
		curl -L --progress-bar -o "$(MODELS_SRC)/$(ONNX_MODEL)" "$(ONNX_URL)"; \
		curl -sL -o "$(MODELS_SRC)/tokenizer.json" \
			"https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json"; \
		curl -sL -o "$(MODELS_SRC)/vocab.txt" \
			"https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/vocab.txt"; \
		echo " ONNX model downloaded"; \
	fi
	@cp -f $(MODELS_SRC)/$(ONNX_MODEL) $(RT_HOME)/models/ 2>/dev/null || true
	@cp -f $(MODELS_SRC)/tokenizer.json $(RT_HOME)/models/ 2>/dev/null || true
	@cp -f $(MODELS_SRC)/vocab.txt $(RT_HOME)/models/ 2>/dev/null || true
	@echo " rt_home/models/ updated ($$(du -sh $(RT_HOME)/models/ | cut -f1))"

# ==============================================================================
# Proto Code Generation
# ==============================================================================
# C++ proto stubs are generated automatically by CMake when building the engine.
# This section handles Go proto stubs, using the protoc built by the engine's
# gRPC FetchContent (build/bin/protoc) so both sides use the same version.
# ==============================================================================

PROTOC_BIN   := $(BUILD_DIR)/bin/protoc
PROTO_SRC    := $(ROOT_DIR)/mono/proto
GO_PB_OUT    := $(SERVER_DIR)/internal/engine/pb
GO_PROTOC_GO := $(shell ls $(HOME)/go/bin/protoc-gen-go 2>/dev/null || echo "")

proto: proto-go ## Regenerate all proto stubs (C++ is automatic via CMake)

proto-go: ## Generate Go gRPC stubs from engine.proto
	@if [ ! -x "$(PROTOC_BIN)" ]; then \
		echo "ERROR: $(PROTOC_BIN) not found. Run 'make engine' first to build protoc."; \
		exit 1; \
	fi
	@if [ -z "$(GO_PROTOC_GO)" ]; then \
		echo "Installing protoc-gen-go and protoc-gen-go-grpc..."; \
		GOBIN=$(HOME)/go/bin $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
		GOBIN=$(HOME)/go/bin $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest; \
	fi
	@mkdir -p $(GO_PB_OUT)
	PATH="$(HOME)/go/bin:$(PATH)" $(PROTOC_BIN) \
		--proto_path=$(PROTO_SRC) \
		--go_out=$(GO_PB_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(GO_PB_OUT) --go-grpc_opt=paths=source_relative \
		$(PROTO_SRC)/engine.proto
	@echo "Go proto stubs generated in $(GO_PB_OUT)"

# ==============================================================================
# CLI & SDKs — Install, Build, Test
# ==============================================================================

# ── CLI ──────────────────────────────────────────────────────────────────────

cli: ## Install CLI in development mode
	@echo ""
	@echo "──── Installing CLI (dev mode) ─────────────────────────"
	cd $(CLI_DIR) && $(PYTHON) -m pip install -e ".[dev]" --quiet
	@echo "  rtvortex CLI installed ($(VERSION))"

build-cli: ## Build CLI distribution (sdist + wheel)
	@echo ""
	@echo "──── Building CLI distribution ─────────────────────────"
	cd $(CLI_DIR) && $(PYTHON) -m build
	@echo "  dist/ created"

test-cli: ## Run CLI tests with coverage
	@echo ""
	@echo "──── Testing CLI ───────────────────────────────────────"
	cd $(CLI_DIR) && $(PYTHON) -m pytest tests/ -v --tb=short \
		--cov=rtvortex_cli --cov-report=term-missing --cov-report=html:htmlcov

# ── Python SDK ───────────────────────────────────────────────────────────────

sdk-python: ## Install Python SDK in development mode
	@echo ""
	@echo "──── Installing Python SDK (dev mode) ──────────────────"
	cd $(SDK_PY_DIR) && $(PYTHON) -m pip install -e ".[dev]" --quiet
	@echo "  rtvortex-sdk installed ($(VERSION))"

build-sdk-python: ## Build Python SDK distribution
	@echo ""
	@echo "──── Building Python SDK distribution ──────────────────"
	cd $(SDK_PY_DIR) && $(PYTHON) -m build
	@echo "  dist/ created"

test-sdk-python: ## Run Python SDK tests with coverage
	@echo ""
	@echo "──── Testing Python SDK ────────────────────────────────"
	cd $(SDK_PY_DIR) && $(PYTHON) -m pytest tests/ -v --tb=short \
		--cov=rtvortex_sdk --cov-report=term-missing --cov-report=html:htmlcov

# ── Node.js SDK ──────────────────────────────────────────────────────────────

sdk-node: ## Install Node.js SDK dependencies
	@echo ""
	@echo "──── Installing Node.js SDK dependencies ───────────────"
	cd $(SDK_NODE_DIR) && $(NPM) ci
	@echo "  node_modules/ installed"

build-sdk-node: sdk-node ## Build Node.js SDK
	@echo ""
	@echo "──── Building Node.js SDK ──────────────────────────────"
	cd $(SDK_NODE_DIR) && $(NPM) run build
	@echo "  dist/ created"

test-sdk-node: sdk-node ## Run Node.js SDK tests
	@echo ""
	@echo "──── Testing Node.js SDK ───────────────────────────────"
	cd $(SDK_NODE_DIR) && $(NPM) run typecheck
	cd $(SDK_NODE_DIR) && $(NPM) test

# ── Java SDK ─────────────────────────────────────────────────────────────────

sdk-java: ## Compile Java SDK
	@echo ""
	@echo "──── Compiling Java SDK ────────────────────────────────"
	cd $(SDK_JAVA_DIR) && $(MVN) -B compile -Drevision=$(VERSION)
	@echo "  Java SDK compiled ($(VERSION))"

build-sdk-java: ## Package Java SDK jar
	@echo ""
	@echo "──── Building Java SDK jar ─────────────────────────────"
	cd $(SDK_JAVA_DIR) && $(MVN) -B package -DskipTests -Drevision=$(VERSION)
	@echo "  target/*.jar created"

test-sdk-java: ## Run Java SDK tests
	@echo ""
	@echo "──── Testing Java SDK ──────────────────────────────────"
	cd $(SDK_JAVA_DIR) && $(MVN) -B test -Drevision=$(VERSION)

# ── Aggregate targets ────────────────────────────────────────────────────────

sdks: sdk-python sdk-node sdk-java ## Install all SDKs
build-sdks: build-sdk-python build-sdk-node build-sdk-java ## Build all SDK distributables
test-sdks: test-sdk-python test-sdk-node test-sdk-java ## Test all SDKs
test-all: test test-cli test-sdks test-web ## Run all tests (engine + server + CLI + SDKs + web)

# ==============================================================================
# Web UI (Next.js)
# ==============================================================================

web: ## Install Web UI dependencies
	@echo ""
	@echo "──── Installing Web UI dependencies ────────────────────"
	cd $(WEB_DIR) && $(NPM) ci --legacy-peer-deps
	@echo "  node_modules/ installed"

build-web: rt_home web ## Build Web UI for production and deploy to rt_home/webApps
	@echo ""
	@echo "──── Building Web UI (production) ──────────────────────"
	cd $(WEB_DIR) && $(NPM) run build
	@echo "  .next/ built"
	@echo "──── Deploying Web UI to rt_home/webApps ───────────────"
	@rm -rf $(RT_HOME)/webApps/dashboard
	@mkdir -p $(RT_HOME)/webApps/dashboard
	@cp -r $(WEB_DIR)/.next/standalone/. $(RT_HOME)/webApps/dashboard/
	@cp -r $(WEB_DIR)/.next/static $(RT_HOME)/webApps/dashboard/.next/static
	@cp -r $(WEB_DIR)/public $(RT_HOME)/webApps/dashboard/public 2>/dev/null || true
	@echo "  rt_home/webApps/dashboard/ ($$(du -sh $(RT_HOME)/webApps/dashboard | cut -f1))"
	@echo "  Run: cd rt_home/webApps/dashboard && node server.js"

test-web: web ## Run Web UI tests
	@echo ""
	@echo "──── Testing Web UI ────────────────────────────────────"
	cd $(WEB_DIR) && npx vitest run

lint-web: web ## Lint Web UI
	@echo ""
	@echo "──── Linting Web UI ────────────────────────────────────"
	cd $(WEB_DIR) && $(NPM) run lint

# ==============================================================================
# Run
# ==============================================================================

run-engine: engine config ## Build and run the C++ engine
	@echo "Starting RTVortex C++ Engine..."
	RTVORTEX_HOME=$(RT_HOME) $(RT_HOME)/bin/rtvortex

run-server: server config ## Build and run the Go API server
	@echo "Starting RTVortexGo API Server..."
	RTVORTEX_HOME=$(RT_HOME) $(RT_HOME)/bin/RTVortexGo

run-web: build-web ## Build and run the Next.js dashboard
	@echo "Starting RTVortex Dashboard on port 3000..."
	cd $(RT_HOME)/webApps/dashboard && HOSTNAME=0.0.0.0 PORT=3000 $(NODE) server.js

run: all ## Build and run all (engine + server + web)
	@echo "Starting RTVortex C++ Engine (background)..."
	RTVORTEX_HOME=$(RT_HOME) $(RT_HOME)/bin/rtvortex &
	sleep 2
	@echo "Starting RTVortexGo API Server (background)..."
	RTVORTEX_HOME=$(RT_HOME) $(RT_HOME)/bin/RTVortexGo &
	sleep 1
	@echo "Starting RTVortex Dashboard (foreground, port 3000)..."
	cd $(RT_HOME)/webApps/dashboard && HOSTNAME=0.0.0.0 PORT=3000 $(NODE) server.js

# ==============================================================================
# Testing
# ==============================================================================

test: test-engine test-server ## Run engine + server tests

test-engine: ## Run C++ engine tests
	cd $(BUILD_DIR) && cmake $(ENGINE_DIR) -DAIPR_BUILD_TESTS=ON
	cd $(BUILD_DIR) && $(MAKE) -j$(NPROC) aipr-engine-tests
	cd $(BUILD_DIR) && ctest --output-on-failure

test-server: ## Run Go server tests
	cd $(SERVER_DIR) && $(GO) test -race -cover ./...

test-server-unit: ## Run Go server unit tests (tests/ dir only)
	cd $(SERVER_DIR) && $(GO) test -race -cover -v ./tests/...

# ==============================================================================
# Database
# ==============================================================================

db-create: rt_home config ## Create PostgreSQL role and database (run as postgres superuser)
	psql -U postgres -f $(RT_HOME)/data/sql/create_database.sql

db-init: rt_home config ## Initialize schema tables (run after db-create)
	psql -U rtvortex -d rtvortex -f $(RT_HOME)/data/sql/initData.sql

db-install: all db-create db-init ## Full setup: build + create DB + init schema

# ==============================================================================
# Clean
# ==============================================================================

clean: ## Remove rt_home/ (build output)
	rm -rf $(RT_HOME)
	@echo "rt_home/ removed"

clean-all: clean ## Remove everything (rt_home + C++ build cache)
	rm -rf $(BUILD_DIR)
	@echo "build/ removed"

# ==============================================================================
# Info
# ==============================================================================

version: ## Show version info
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "RT_HOME:    $(RT_HOME)"
	@echo "SDK/CLI:    $(VERSION)"

status: ## Show what's in rt_home
	@echo "rt_home contents:"
	@find $(RT_HOME) -type f 2>/dev/null | sort | sed 's|$(ROOT_DIR)/||' || echo "  (empty — run 'make' first)"

help: ## Show this help
	@echo "RTVortex Build System — $(VERSION)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
