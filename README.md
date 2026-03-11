# RTVortex — AI-Powered Code Review Platform

<div align="center">

**Production-Grade AI-Powered Code Review System**

[![Go 1.24+](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://go.dev/)
[![C++17](https://img.shields.io/badge/C++-17-00599C.svg)](https://isocpp.org/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)
[![CI](https://github.com/AuralithAI/RT-AI-PR-Reviewer/actions/workflows/ci.yml/badge.svg)](https://github.com/AuralithAI/RT-AI-PR-Reviewer/actions/workflows/ci.yml)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED.svg)](https://hub.docker.com/)
[![gRPC](https://img.shields.io/badge/gRPC-TLS-4285F4.svg)](https://grpc.io/)
[![Prometheus](https://img.shields.io/badge/Prometheus-metrics-E6522C.svg)](https://prometheus.io/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.0-85EA2D.svg)](https://www.openapis.org/)

A platform-neutral PR review engine with connectors for GitHub, GitLab, Bitbucket, and Azure DevOps.

[Setup Guide](docs/setup.md) · [Architecture](docs/architecture.md) · [Go Server Architecture](docs/go-server-architecture.md)

</div>

---

## Overview

RTVortex is a two-component system that uses LLMs and static analysis to automatically review pull requests:

```
Clients (Webhooks, CLI, Web UI, SDKs)
         │
         ▼  REST / WebSocket (port 8080)
┌────────────────────────────┐
│  RTVortexGo API Server     │  Go 1.24, chi router, 32+ endpoints
│  (auth, orchestration,     │  OAuth2, JWT, rate limiting, audit
│   LLM, webhooks)           │  Prometheus, WebSocket, SSE streaming
└────────┬───────────────────┘
         │  gRPC (port 50051)
┌────────▼───────────────────┐
│  RTVortex C++ Engine       │  C++17, 16K+ lines
│  (indexing, retrieval,     │  FAISS, tree-sitter, ONNX
│   heuristics, TMS)         │  Hybrid search, AST parsing
└────────────────────────────┘
```

## Features

### Core Platform
- **Platform Agnostic**: GitHub, GitLab, Bitbucket, and Azure DevOps (cloud & self-hosted)
- **CI/CD Integration**: GitHub Actions, GitLab CI, Jenkins, Bitbucket Pipelines, Azure Pipelines
- **High Performance**: C++ engine for code indexing and analysis via gRPC
- **LLM Powered**: OpenAI, Anthropic, and Ollama with SSE streaming support
- **Multi-Cloud Storage**: Local, AWS S3, GCS, Azure Blob, OCI Object Storage, MinIO
- **Real-Time Updates**: WebSocket progress streaming for review status
- **Security**: AES-256-GCM token encryption, JWT auth, OAuth2 (6 providers), rate limiting
- **Self-Hostable**: Deploy on your infrastructure with full control
- **TLS/mTLS Support**: Secure gRPC communication with included dev certificates
- **API Documentation**: Full OpenAPI 3.0 spec at `/api/v1/docs/openapi.yaml`
- **Unified Configuration**: XML configs (`rtserverprops.xml` + `vcsplatforms.xml`) drive both components

### Ingestion Pipeline (TMS)
- **Batched Streaming**: Processes repositories in 5,000-file batches — indexes 130 GB / 9.5M-chunk repos at ~5 GB RSS
- **Parallel ONNX Embedding**: Multiple ONNX sessions run concurrently (auto-tunes to `cores/4` workers), saturating all available CPU cores
- **Hierarchical Chunking**: File-summary chunks with structural context (module path, imports, exports) for better retrieval
- **Knowledge Graph**: SQLite-backed IMPORTS / CALLS / CONTAINS edge graph extracted from parsed code
- **Memory Accounts**: Classifies chunks into dev / ops / security / history accounts for targeted retrieval
- **Confidence Gate**: Zero-LLM fast path — high-confidence retrievals skip the LLM round-trip entirely

### Observability
- **Prometheus Metrics**: 20+ counters, histograms, and gauges across both components
- **Real-Time Metrics Dashboard**: Live engine metrics (FAISS status, MiniLM readiness, embedding throughput, LLM avoidance rate) via SSE
- **Structured Logging**: JSON-structured logs with request tracing

### Repository Management
- **Web UI**: Index / reindex / reclone controls with branch selector and confirmation dialogs
- **Three Indexing Modes**: `index` (first-time clone + index), `reindex` (re-embed existing clone), `reclone` (fresh clone + reindex)
- **Branch Targeting**: Select any remote branch for indexing via `git ls-remote`

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.24+ | API server |
| CMake | 3.20+ | C++ engine build |
| g++ | 11+ | C++ compiler |
| PostgreSQL | 15+ | Database (runtime) |
| Redis | 7+ | Session cache, rate limiting (runtime) |
| Make | Any | Unified build controller |

## Quick Start

```bash
# Clone
git clone https://github.com/AuralithAI/RT-AI-PR-Reviewer.git
cd RT-AI-PR-Reviewer

# Build everything into rt_home/
make

# Configure
nano rt_home/config/rtserverprops.xml   # Database, LLM, engine settings
nano rt_home/config/vcsplatforms.xml    # GitHub/GitLab/Bitbucket/Azure OAuth

# Initialize database
make db-install

# Run both components
make run
```

The `make` command builds both the C++ engine and Go server into the `rt_home/` output directory:

```
rt_home/
├── bin/
│   ├── rtvortex          ← C++ engine (gRPC, indexing, retrieval)
│   └── RTVortexGo        ← Go API server (REST, webhooks, auth)
├── config/               ← XML configuration files + TLS certs
├── data/sql/             ← PostgreSQL schema scripts
├── models/               ← ONNX model weights
└── temp/                 ← Logs + ephemeral scratch
```

## Building the C++ Engine

The C++ engine is an independent CMake project.

### Prerequisites (Linux / Ubuntu)

```bash
sudo apt-get update
sudo apt-get install -y \
  build-essential cmake g++ \
  libcurl4-openssl-dev \
  libssl-dev \
  libomp-dev \
  libopenblas-dev \
  liblapack-dev \
  libgflags-dev
```

**FAISS** must be installed separately (not fetched by CMake):

```bash
git clone --depth 1 --branch v1.7.4 https://github.com/facebookresearch/faiss.git /tmp/faiss
cd /tmp/faiss && mkdir build && cd build
cmake .. \
  -DFAISS_ENABLE_GPU=OFF \
  -DFAISS_ENABLE_PYTHON=OFF \
  -DFAISS_ENABLE_C_API=ON \
  -DBUILD_TESTING=OFF \
  -DBUILD_SHARED_LIBS=ON \
  -DCMAKE_BUILD_TYPE=Release \
  -DBLA_VENDOR=OpenBLAS
cmake --build . --config Release --parallel $(nproc)
sudo cmake --install . --config Release
sudo ldconfig
```

### Prerequisites (macOS)

```bash
brew install libomp openblas faiss protobuf grpc abseil
```

### Building (Standalone)

```bash
# Configure (fetches gRPC, abseil, nlohmann/json, tree-sitter, ONNX Runtime automatically)
cmake -B build -S mono/engine \
  -DCMAKE_BUILD_TYPE=Release \
  -DAIPR_BUILD_TESTS=ON \
  -DAIPR_BUILD_SERVER=ON \
  -DAIPR_USE_FAISS=ON \
  -DAIPR_USE_TREE_SITTER=ON

# Build
cmake --build build --parallel $(nproc)
```

### CMake Options

| Option | Default | Description |
|--------|---------|-------------|
| `AIPR_BUILD_TESTS` | `ON` | Build unit tests (`aipr-engine-tests`) |
| `AIPR_BUILD_SERVER` | `ON` | Build the gRPC server (`rtvortex`) |
| `AIPR_BUILD_TOOLS` | `ON` | Build utility tools |
| `AIPR_USE_FAISS` | `ON` | Enable FAISS vector search |
| `AIPR_USE_TREE_SITTER` | `ON` | Enable tree-sitter AST parsing |
| `AIPR_USE_ONNX` | `ON` | Enable ONNX Runtime local embeddings |

## Building the Go Server

```bash
cd mono/server-go
go build -trimpath -o RTVortexGo ./cmd/rtvortex-server/
```

Or use the unified Makefile:

```bash
make server    # builds into rt_home/bin/RTVortexGo
```

### Go Dependencies

| Dependency | Purpose |
|------------|---------|
| `chi/v5` | HTTP router |
| `pgx/v5` | PostgreSQL driver (connection pooling) |
| `go-redis/v9` | Redis client |
| `grpc` | Engine communication |
| `golang-jwt/v5` | JWT authentication |
| `oauth2` | OAuth2 flows |
| `coder/websocket` | WebSocket support |
| `prometheus/client_golang` | Metrics export |

## Make Targets

| Target | Description |
|--------|-------------|
| `make` | Build everything (C++ engine + Go server + config + models) |
| `make engine` | Build only the C++ engine |
| `make server` | Build only the Go API server |
| `make run` | Build and run both components |
| `make run-engine` | Build and run only the engine |
| `make run-server` | Build and run only the Go server |
| `make test` | Run all tests (C++ + Go) |
| `make db-install` | Create database + initialize schema |
| `make clean` | Remove `rt_home/` |
| `make status` | Show what's in `rt_home/` |
| `make help` | Show all targets |

## Configuration

All configuration is centralized in two XML files. The Go server reads both files and pushes relevant settings (storage, LLM, etc.) to the C++ engine via gRPC at startup.

**`config/rtserverprops.xml`** — Server settings:
```xml
<!-- Database -->
<database host="localhost" port="5432" name="rtvortex"
          username="rtvortex" password="your_password"/>

<!-- LLM Providers (multiple supported with failover) -->
<llm primary="openai" fallback="ollama">
    <openai api-key="${LLM_OPENAI_API_KEY}" model="gpt-4-turbo-preview"/>
    <anthropic api-key="${LLM_ANTHROPIC_API_KEY}" model="claude-3-5-sonnet"/>
    <ollama base-url="http://localhost:11434" model="codellama"/>
</llm>

<!-- Storage (pushed to C++ engine via gRPC) -->
<storage type="s3">
    <s3 bucket="my-bucket" region="us-east-1"/>
</storage>
```

**`config/vcsplatforms.xml`** — Platform OAuth:
```xml
<github enabled="true" client-id="your_id" client-secret="your_secret"/>
<gitlab enabled="true" application-id="your_id" application-secret="your_secret"/>
<bitbucket enabled="true" client-id="your_id" client-secret="your_secret"/>
<azure-devops enabled="true" client-id="your_id" tenant-id="your_tenant"/>
```

## REST API Highlights

The Go server exposes 32+ endpoints at port 8080. Full OpenAPI spec at `/api/v1/docs/openapi.yaml`.

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/health` | GET | Health check |
| `/ready` | GET | Readiness probe |
| `/metrics` | GET | Prometheus metrics |
| `/api/v1/auth/login/{provider}` | GET | OAuth2 login (GitHub, GitLab, etc.) |
| `/api/v1/reviews` | POST | Submit a PR for review |
| `/api/v1/reviews/{id}` | GET | Get review result |
| `/api/v1/reviews/{id}/ws` | GET | WebSocket review progress |
| `/api/v1/repos/{id}/index` | POST | Trigger repository indexing |
| `/api/v1/repos/{id}/branches` | GET | List remote branches (`git ls-remote`) |
| `/api/v1/repos/{id}/reindex` | POST | Re-embed existing local clone |
| `/api/v1/repos/{id}/reclone` | POST | Fresh clone + reindex |
| `/api/v1/webhooks/github` | POST | GitHub webhook receiver |
| `/api/v1/webhooks/gitlab` | POST | GitLab webhook receiver |
| `/api/v1/webhooks/bitbucket` | POST | Bitbucket webhook receiver |
| `/api/v1/webhooks/azure-devops` | POST | Azure DevOps webhook receiver |
| `/api/v1/llm/providers` | GET | List LLM providers + health |
| `/api/v1/llm/stream` | POST | SSE streaming LLM completion |

## CI/CD Integration

### GitHub Actions

```yaml
- uses: AuralithAI/ai-pr-reviewer-action@v1
  with:
    api-url: https://your-server.com
    api-key: ${{ secrets.AIPR_API_KEY }}
```

### GitLab CI

```yaml
ai-pr-review:
  image: auralithai/aipr-cli:latest
  script:
    - aipr review --gitlab-mr $CI_MERGE_REQUEST_IID
```

### Jenkins

```groovy
sh 'docker run auralithai/aipr-cli review --pr ${CHANGE_ID}'
```

See [docs/setup.md](docs/setup.md) for complete integration guides.

## Documentation

| Document | Description |
|----------|-------------|
| [Setup Guide](docs/setup.md) | Prerequisites, building, configuration, TLS, distribution |
| [Architecture](docs/architecture.md) | System design, C++ engine, deployment models, scaling |
| [Go Server Architecture](docs/go-server-architecture.md) | Go server internals, packages, data flow, middleware |

## License

Apache 2.0 — See [LICENSE](LICENSE) for details.
