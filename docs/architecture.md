# AI PR Reviewer - Architecture

## Quick Reference

```
External Clients (SDKs, CLI, Web)
         │
         │  gRPC (port 9090)          REST (port 8080)
         ▼                            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                   Java Server (aipr-server.jar)                     │
│                                                                     │
│  gRPC Server ◄── GrpcDataServiceImpl                                │
│  REST API    ◄── Controllers (5)                                    │
│  Background  ◄── BackgroundTaskScheduler (3 cron tasks)             │
│  DB Layer    ◄── Persister → PostgreSQL                             │
│  Cache       ◄── Redis (optional)                                   │
│                                                                     │
│  gRPC Client ──► EngineClient                                       │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ gRPC (port 50051)
                               ▼
                      ┌────────────────┐
                      │  C++ Engine    │
                      │  (aipr-engine) │
                      │  gRPC Server   │
                      └────────────────┘
```

**Port Summary:**
| Port | Service | Protocol | Access |
|------|---------|----------|--------|
| 8080 | Java REST API | HTTP | External |
| 9090 | Java gRPC API | gRPC | External |
| 50051 | C++ Engine | gRPC | Internal only |
| 5432 | PostgreSQL | TCP | Internal only |
| 6379 | Redis | TCP | Internal only |

## Overview

AI PR Reviewer is a multi-component system for automated code review powered by LLMs.
The system consists of two main runtime components:

1. **C++ Engine** - High-performance gRPC server for code indexing and retrieval
2. **Java Server** - REST/gRPC API for webhooks, orchestration, and LLM integration

```
┌─────────────────────────────────────────────────────────────────────┐
│                         External Clients                            │
│   (GitHub/GitLab Webhooks, CLI, SDKs, Web UI)                       │
└────────────────────────────┬────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        Java Server                                  │
│   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│   │  REST API   │  │  gRPC API   │  │  Webhooks   │                 │
│   │  (8080)     │  │  (9090)     │  │  Handler    │                 │
│   └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                 │
│          │                │                │                        │
│          └────────────────┼────────────────┘                        │
│                           │                                         │
│   ┌───────────────────────▼───────────────────────┐                 │
│   │              Review Orchestrator              │                 │
│   │  - Context building                           │                 │
│   │  - LLM prompt construction                    │                 │
│   │  - Response parsing                           │                 │
│   └───────────────────────┬───────────────────────┘                 │
│                           │                                         │
│   ┌───────────────────────▼───────────────────────┐                 │
│   │              Engine Client (gRPC)             │                 │
│   │  - IndexRepository, Search, BuildContext      │                 │
│   └───────────────────────┬───────────────────────┘                 │
└───────────────────────────┼─────────────────────────────────────────┘
                            │
                            │ gRPC (port 50051)
                            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       C++ Engine (gRPC Server)                      │
│   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│   │   Indexer   │  │  Retriever  │  │  Heuristics │                 │
│   │  - AST      │  │  - Lexical  │  │  - Security │                 │
│   │  - Chunking │  │  - Vector   │  │  - Perf     │                 │
│   │  - Symbols  │  │  - Graph    │  │  - Style    │                 │
│   └─────────────┘  └─────────────┘  └─────────────┘                 │
└─────────────────────────────────────────────────────────────────────┘
```

## Components

### C++ Engine (`mono/engine/`)

The engine is a **standalone gRPC server** that handles compute-intensive operations:

- **Code Indexing**: Parses repositories, extracts symbols, creates embeddings
- **Retrieval**: Hybrid search (lexical + vector + graph) for relevant context
- **Heuristics**: Fast rule-based checks that don't require LLM

**Key Files:**
- `src/server/main.cpp` - Server entry point, CLI parsing, signal handling
- `src/server/engine_service_impl.cpp` - gRPC service implementation
- `include/engine_api.h` - Core Engine API

**Communication:**
- Listens on port **50051** (configurable via `--port` or `ENGINE_PORT`)
- Protocol: gRPC (defined in `proto/engine.proto`)
- TLS optional (controlled by `ENGINE_TLS_ENABLED`)

**Startup:**
```bash
# Direct
./bin/aipr-engine --host 0.0.0.0 --port 50051 --config config/default.yml

# Via wrapper script
./bin/aipr-engine-start

# Windows service
aipr-engine.exe --service
```

### Java Server (`mono/server/`)

The server handles external API, webhooks, and LLM orchestration:

- **REST API** (port 8080): For web UI, manual triggers, and webhooks
- **gRPC API** (port 9090): For SDK clients (Java, Python, Node.js)
- **Webhook Handler**: GitHub, GitLab, Bitbucket integration
- **Review Orchestrator**: Coordinates engine + LLM for full reviews

#### REST API Endpoints (Port 8080)

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/reviews` | POST | Submit a PR for review |
| `/api/v1/reviews/{id}` | GET | Get review status/result |
| `/api/v1/webhooks/github` | POST | GitHub webhook receiver |
| `/api/v1/webhooks/gitlab` | POST | GitLab webhook receiver |
| `/api/v1/repos/{id}/index` | POST | Trigger reindex |
| `/api/v1/health` | GET | Health check |
| `/api/v1/config` | GET/PUT | Runtime configuration |

#### Why Two APIs?

- **REST (8080)**: HTTP-friendly for webhooks, web UI, simple integrations
- **gRPC (9090)**: Efficient for SDK clients, streaming support, type-safe

Both APIs are served by the same Java server and share the same orchestration logic.

**Communication with Engine:**
- Connects to engine via gRPC as a client
- Configuration in `rtserverprops.xml`:
  ```xml
  <engine host="${ENGINE_HOST:localhost}"
          port="${ENGINE_PORT:50051}"
          negotiation-type="${ENGINE_NEGOTIATION:TLS}"/>
  ```

#### Background Tasks (`mono/server/src/.../task/`)

The Java server includes a built-in cron scheduler for maintenance operations.
No external Quartz server required — tasks run inside the server process.

| Task | Cron | Purpose |
|------|------|---------|
| `session-cleanup` | Every 15 min | Evict expired sessions from cache/DB |
| `index-job-cleanup` | Every hour | Remove completed index jobs older than 7 days |
| `inactive-user-cleanup` | Daily 3 AM | Revoke sessions for users inactive >30 days |
| `llm-health-check` | Every 60s | Check LLM provider health, trigger failover |

**Key Classes:**
- `BackgroundTaskScheduler` - Discovers and schedules all `IBackgroundTask` beans
- `IBackgroundTask` - Interface for implementing new tasks
- `TaskResult` - Status enum: SUCCESS, PARTIAL, SKIPPED, FAILED
- `LLMHealthCheckBackgroundTask` - Provider health monitoring

Tasks execute with **superuser privileges** (no user session context).

### Proto Definitions (`mono/proto/`)

**Source of Truth:** All `.proto` files live in `mono/proto/`. This is the **only** location to edit.

```
mono/proto/                    ← EDIT HERE (source of truth)
├── api.proto                  │
├── engine.proto               │
└── session.proto              │
                               ▼
         ┌─────────────────────┴─────────────────────┐
         │                                           │
         ▼                                           ▼
    C++ Engine                              mono/proto-java/
    (CMake reads directly)                  (Gradle copies & generates)
                                                     │
                                                     ▼
                                            Java gRPC stubs
                                            (build/generated/)
```

**Important:** `mono/proto-java/src/main/proto/` contains **auto-copied** files. They are:
- Copied by Gradle `copyProtos` task before compilation
- Deleted on `./gradlew clean`
- Ignored by `.gitignore` — **never committed**

| Proto File | Purpose | Direction |
|------------|---------|-----------|
| `engine.proto` | Engine service | Java → C++ (Internal) |
| `api.proto` | External API | SDKs/CLI → Java (External) |
| `session.proto` | Auth/sessions | Shared types |

**Data Flow:**
```
┌──────────────────┐      api.proto          ┌──────────────────┐
│  External Client │ ◀────────────────────▶ │    Java Server   │
│  (SDK, CLI, UI)  │  ReviewRequest/Result   │    Port 9090     │
└──────────────────┘                         └────────┬─────────┘
                                                      │
                                             engine.proto
                                             IndexRequest
                                             SearchRequest
                                             BuildContextRequest
                                                      │
                                                      ▼
                                             ┌──────────────────┐
                                             │    C++ Engine    │
                                             │    Port 50051    │
                                             └──────────────────┘
```

**Key Distinction:**
- **`api.proto`**: High-level review operations (SubmitReview, GetStatus)
- **`engine.proto`**: Low-level compute operations (Index, Search, BuildContext)
- **`session.proto`**: Shared authentication/token types

## Database & Storage

| Store | Component | Purpose |
|-------|-----------|---------|
| PostgreSQL | **Java only** | Review history, user config, audit logs |
| Redis | **Java only** | Session cache, rate limiting, job queues |
| Cloud Storage | **C++ Engine** | Index data, embeddings (S3/GCS/Azure/OCI/MinIO) |
| Local files | C++ Engine | Index data (`.aipr/index/`) for local mode |

**Storage Configuration Flow:**
```
rtserverprops.xml (Single Source of Truth)
         │
         ▼
┌──────────────────┐     ConfigureStorage RPC       ┌──────────────────┐
│   Java Server    │ ─────────────────────────────▶│   C++ Engine     │
│   (reads XML)    │                                │ (builds backend) │
└──────────────────┘                                └──────────────────┘
```

The Java server reads storage configuration from `rtserverprops.xml` and pushes it to the C++ engine via the `ConfigureStorage` gRPC call at startup. This ensures both components use identical settings without environment variable duplication.

**Important:** The C++ Engine has **no database connections**. It's a pure compute service that:
- Reads repository files from disk or cloud storage
- Stores index data via the configured storage backend
- Responds to gRPC requests

## Deployment Models

### 1. Single Host (Development)

Both components on same machine:

```bash
# Terminal 1: Start engine
./bin/aipr-engine-start

# Terminal 2: Start server
./bin/aipr-server
```

### 2. Docker Compose

Uses separate containers with internal networking:

```bash
docker compose up -d
```

Services:
- `engine` - C++ gRPC server (internal, port 50051)
- `server` - Java server (exposed 8080, 9090)
- `postgres` - Database
- `redis` - Cache

### 3. Kubernetes / Helm

```bash
helm install aipr deploy/helm/aipr/
```

Creates separate pods for engine and server with proper service discovery.

### 4. Separate Hosts

Engine and server on different machines:

**Engine Host:**
```bash
ENGINE_HOST=0.0.0.0 ENGINE_PORT=50051 ./bin/aipr-engine
```

**Server Host:**
```bash
ENGINE_HOST=engine.internal.example.com ENGINE_PORT=50051 ./bin/aipr-server
```

## Configuration

### Configuration Files

| File | Used By | Purpose |
|------|---------|---------|
| `config/rtserverprops.xml` | **Both** (via gRPC) | Single source of truth: DB, Redis, LLM, Storage, Engine |
| `config/vcsplatforms.xml` | Java | Platform OAuth (GitHub, GitLab, Bitbucket, Azure DevOps) |
| `config/default.yml` | C++ Engine | Indexing, retrieval, review settings |
| `config/large-repo.yml` | Override | Profile for monorepos |
| `config/perf-focused.yml` | Override | Profile prioritizing performance checks |
| `config/strict-security.yml` | Override | Profile for security-sensitive repos |

### Configuration Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                      rtserverprops.xml                              │
│                   (Single Source of Truth)                          │
└────────────────────────────┬────────────────────────────────────────┘
                             │
         ┌───────────────────┴───────────────────┐
         │                                       │
         ▼                                       ▼
┌─────────────────────┐                ┌─────────────────────┐
│    Java Server      │    gRPC        │    C++ Engine       │
│                     │──────────────▶│                     │
│  - LLM config       │  Configure     │  - Storage backend  │
│  - DB/Redis         │  Storage()     │  - Index paths      │
│  - Engine client    │                │  - TLS settings     │
└─────────────────────┘                └─────────────────────┘
```

**How it works:**
1. Java server starts and loads `rtserverprops.xml`
2. Java connects to C++ engine via gRPC
3. Java calls `ConfigureStorage()` RPC, pushing storage config to engine
4. Engine builds appropriate `StorageBackend` (Local, S3, GCS, Azure, OCI, MinIO)
5. Both components now use identical storage settings

### default.yml Structure

```yaml
# Core indexing settings (read by C++ Engine)
indexing:
  max_file_size_kb: 1024
  exclude: ["**/node_modules/**", ...]
  languages: []  # auto-detect

# Retrieval settings (read by C++ Engine)
retrieval:
  top_k: 20
  lexical_weight: 0.3
  vector_weight: 0.7

# Review settings (read by Java Server)
review:
  max_comments: 20
  severity_threshold: "warning"
  enabled_checks: [security, performance, ...]

# Model settings (read by Java Server)
model:
  embeddings:
    provider: "openai"
    model: "text-embedding-3-small"
  llm:
    provider: "openai"
    model: "gpt-4o"

# Storage settings (read by C++ Engine)
storage:
  backend: "local"
  path: ".aipr/index"
```

**How config flows:**
1. Java Server loads `default.yml` at startup
2. When calling C++ Engine, relevant sections are passed via gRPC request config
3. C++ Engine can also load `default.yml` directly for standalone operation

### Engine Configuration

Command-line arguments:
- `--host` - Bind address (default: `0.0.0.0`)
- `--port` - Port (default: `50051`)
- `--config` - Config file path (default: `config/default.yml`)
- `--verbose` - Enable debug logging
- `--service` - Windows service mode

Environment variables:
- `ENGINE_HOST`, `ENGINE_PORT`, `ENGINE_CONFIG`
- `ENGINE_TLS_ENABLED`, `ENGINE_TLS_CERT`, `ENGINE_TLS_KEY`, `ENGINE_TLS_CA`

### Server Configuration

See `config/rtserverprops.xml` for full documentation.

Key settings:
```xml
<engine host="${ENGINE_HOST:localhost}"
        port="${ENGINE_PORT:50051}"
        negotiation-type="${ENGINE_NEGOTIATION:TLS}"/>
```

## CI/CD Pipelines

Independent pipelines for faster iteration:

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `engine.yml` | Changes to `mono/engine/**` | C++ build, test, package |
| `server.yml` | Changes to `mono/server/**` | Java build, test, coverage |
| `ci.yml` | All pushes | Integration tests with both |
| `release.yml` | Merge to main | Version bump, publish artifacts |

## Security

- **TLS/mTLS**: Both engine and server support TLS
- **Authentication**: JWT for REST API, client certs for gRPC
- **Network isolation**: Engine doesn't need public access

See `SECURITY.md` for vulnerability reporting.

## Phase 3 Features

### Brain-Inspired Memory Architecture (TMS)

The engine implements a **Triune Memory System (TMS)** inspired by cognitive architectures.
Total codebase: ~16,300 lines of C++17 across 35 source files and 17 headers.

```
┌─────────────────────────────────────────────────────────────────────┐
│                    TMSMemorySystem (Orchestrator)                   │
│                                                                     │
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐            │
│  │     LTM       │  │     STM       │  │     MTM       │            │
│  │ Long-Term     │  │ Short-Term    │  │ Meta-Task     │            │
│  │ Memory        │  │ Memory        │  │ Memory        │            │
│  │               │  │               │  │               │            │
│  │ FAISS Index   │  │ Session Ring  │  │ Pattern Graph │            │
│  │ Vector Search │  │ Buffer        │  │ Strategies    │            │
│  │ Binary Chunks │  │ Query Cache   │  │ Feedback Loop │            │
│  └───────┬───────┘  └───────┬───────┘  └───────┬───────┘            │
│          │                  │                  │                    │
│          └──────────────────┼──────────────────┘                    │
│                             │                                       │
│                 ┌───────────▼────────────┐                          │
│                 │ Cross-Memory Attention │                          │
│                 │                        │                          │
│                 │ - Multi-head attention │                          │
│                 │ - Source weighting     │                          │
│                 │ - Context-aware fusion │                          │
│                 └───────────┬────────────┘                          │
│                             │                                       │
│                 ┌───────────▼────────────┐                          │
│                 │  Compute Controller    │                          │
│                 │                        │                          │
│                 │ - FAST / BALANCED /    │                          │
│                 │   THOROUGH strategies  │                          │
│                 │ - Resource-aware       │                          │
│                 │   scheduling           │                          │
│                 └────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────────┘
```

#### Configuration & Hardcoded Values

**No hardcoded URLs or secrets in the C++ engine.** All external endpoints are configurable:

| Setting | Default Value | How to Override |
|---------|---------------|-----------------|
| Embedding endpoint | `https://api.openai.com/v1/embeddings` | Config file `embed_endpoint` or `EmbeddingConfig.api_endpoint` |
| Embedding API key | *(none)* | Env var: `OPENAI_API_KEY` (name configurable via `embed_api_key_env`) |
| ONNX model path | `models/all-MiniLM-L6-v2.onnx` | Config file `onnx_model_path` |
| Server bind address | `0.0.0.0` | CLI `--host` or env `ENGINE_HOST` |
| Server port | `50051` | CLI `--port` or env `ENGINE_PORT` |
| Storage path | `.aipr/index` | Config file `storage_path` |
| Cloud storage creds | *(none)* | Env vars: `AWS_*`, `AZURE_*`, `OCI_*`, `MINIO_*` |
| TLS certificates | *(disabled)* | Env vars: `ENGINE_TLS_CERT`, `ENGINE_TLS_KEY`, `ENGINE_TLS_CA` |
| Engine version | Auto from git tag | `CMakeLists.txt` reads git tag → `mono/VERSION` → fallback |

Cloud storage URLs (S3, GCS, Azure Blob, OCI) are **dynamically constructed** from the configured region/bucket — not hardcoded.

The only non-overridable URLs are standard infrastructure endpoints:
- `http://169.254.169.254` — AWS/GCP metadata service (link-local, immutable by spec)
- `https://sts.{region}.amazonaws.com` — AWS STS (constructed from region config)

#### Version Management

Version is **fully automated** — no manual edits required:

```
release.yml (merge to main)
    │
    ├── Bumps mono/VERSION (0.1.0 → 0.1.1)
    ├── Creates git tag v0.1.1
    └── Pushes tag
            │
            ▼
CMakeLists.txt (at cmake configure time)
    │
    ├── Priority 1: git describe --tags → v0.1.1 → VERSION 0.1.1
    ├── Priority 2: mono/VERSION file → 0.1.0 (local dev fallback)
    └── Priority 3: hardcoded 0.0.0
            │
            ▼
configure_file(version.h.in → build/generated/version.h)
    │
    ├── AIPR_VERSION      = "0.1.1"
    ├── AIPR_GIT_HASH     = "abc1234"
    ├── AIPR_GIT_DESCRIBE = "v0.1.1"
    ├── AIPR_BUILD_DATE   = "2026-03-03"
    └── AIPR_VERSION_FULL = "0.1.1 (abc1234)"
            │
            ▼
Used by: engine.cpp (getVersion(), runDiagnostics())
         main.cpp   (--version flag)
```

#### Build Dependencies

| Dependency | Purpose | How Resolved |
|------------|---------|--------------|
| **libcurl** | HTTP embedding API calls | System package (required) |
| **nlohmann/json** | JSON parsing | FetchContent (auto-downloaded) |
| **ONNX Runtime** | Local embedding inference | FetchContent (platform-specific binary) |
| **FAISS** | Vector similarity search | System or FetchContent (requires BLAS) |
| **BLAS** | Linear algebra for FAISS | MKL → OpenBLAS → Apple Accelerate (auto-detected) |
| **tree-sitter** | AST parsing (8 grammars) | FetchContent (built from amalgamated source) |
| **gRPC + Protobuf** | Server communication | System or FetchContent |
| **abseil** | gRPC dependency | FetchContent (if gRPC not system) |
| **Google Test** | Unit tests | FetchContent |

#### Cross-Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| Linux x86_64 | ✅ Primary | Full build, CI tested |
| Linux ARM64 | ✅ Supported | Cross-compile in CI |
| macOS x64 | ✅ Supported | Homebrew deps, Apple Accelerate BLAS |
| macOS ARM64 (M1+) | ✅ Supported | Native ARM64 runner in CI |
| Windows x64 | ✅ Supported | vcpkg deps, MSVC, Windows service support |

Platform-specific code is properly guarded with `#ifdef __linux__` / `__APPLE__` / `_WIN32` in:
- `engine.cpp` — system diagnostics (memory, disk, platform string)
- `tms_memory_system.cpp` — `/proc/meminfo` reading (Linux only, with fallback)
- `server/main.cpp` — signal handling, Windows service control
- `util/fs.cpp` — full cross-platform filesystem helpers

---

### Scaling: Multi-Instance Deployment

#### When to Scale Beyond a Single Engine

A single engine instance handles:
- ~50,000 chunks in LTM (FAISS) with sub-100ms query latency
- ~10 concurrent gRPC requests (pipelined through TMS forward pass)
- ~500MB-2GB RAM (depending on index size and embedding cache)

You need multiple instances when:
- Indexing 5+ large monorepos (>100K files each)
- Handling >50 concurrent review requests
- Requiring fault tolerance / zero-downtime deploys

#### Architecture: Multiple Engine Instances

```
                    ┌──────────────────────┐
                    │   Java Server(s)     │
                    │   (Stateless, N×)    │
                    └──────────┬───────────┘
                               │
                    ┌──────────▼───────────┐
                    │   gRPC Load Balancer │
                    │   (Envoy / NGINX /   │
                    │    K8s Service)       │
                    └──┬─────┬─────┬───────┘
                       │     │     │
              ┌────────┘     │     └────────┐
              ▼              ▼              ▼
      ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
      │ Engine #1    │ │ Engine #2    │ │ Engine #3    │
      │ repos: A,B   │ │ repos: C,D   │ │ repos: E,F   │
      │              │ │              │ │              │
      │ LTM (FAISS)  │ │ LTM (FAISS)  │ │ LTM (FAISS)  │
      │ STM (local)  │ │ STM (local)  │ │ STM (local)  │
      │ MTM (local)  │ │ MTM (local)  │ │ MTM (local)  │
      └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
             │                │                │
             └────────────────┼────────────────┘
                              │
                   ┌──────────▼───────────┐
                   │   Shared Storage     │
                   │   (S3 / GCS / Azure  │
                   │    / MinIO)          │
                   └──────────────────────┘
```

#### Storage Strategy: Local vs. Shared

| Setup | Storage | When to Use |
|-------|---------|-------------|
| **Single instance** | Local filesystem | Dev, small teams (<5 repos) |
| **2-4 instances** | Shared object storage (S3/GCS/MinIO) | Medium teams, each instance owns a repo partition |
| **5+ instances** | Shared object storage + optional Redis for coordination | Large orgs, auto-scaling |

**Why NOT a relational database for index data?**

The engine's index data is:
- **Large binary blobs** (FAISS indexes, embedding vectors) — poor fit for SQL
- **Read-heavy** with bulk writes during indexing — object storage excels here
- **Partitionable by repo** — no cross-repo joins needed

Object storage (S3/MinIO) is the right choice because:
- ✅ Handles binary blobs natively (FAISS index files, chunk binaries)
- ✅ Scales horizontally without connection pool limits
- ✅ Each engine instance can independently read/write its repo partition
- ✅ Already implemented (`cloud_storage.cpp` — S3/GCS/Azure/OCI/MinIO)
- ✅ No schema migrations, no ORM complexity

**PostgreSQL remains for the Java Server only** (review history, user config, audit logs — structured relational data).

#### Load Balancing

| Deployment | Load Balancer | Routing Strategy |
|------------|---------------|------------------|
| **Kubernetes** | K8s Service (ClusterIP) | Round-robin by default; use `repo_id` header for sticky routing |
| **Docker Compose** | Envoy sidecar or NGINX | Consistent hashing on `repo_id` for cache locality |
| **Cloud VMs** | Cloud LB (ALB/NLB/GLB) | gRPC-aware LB required (HTTP/2); use header-based routing |
| **Single host** | None needed | Direct connection on port 50051 |

**Recommended: Sticky routing by `repo_id`**

Each engine instance loads its repo's FAISS index into memory. Routing all requests for the same repo to the same instance avoids redundant index loading:

```yaml
# Envoy example: consistent hash on repo_id header
clusters:
  - name: aipr-engine
    lb_policy: RING_HASH
    ring_hash_lb_config:
      minimum_ring_size: 64
    transport_socket:
      name: envoy.transport_sockets.tls
```

```yaml
# Kubernetes: use a headless Service + client-side routing
apiVersion: v1
kind: Service
metadata:
  name: aipr-engine
spec:
  clusterIP: None  # Headless — lets Java client do sticky routing
  selector:
    app: aipr-engine
  ports:
    - port: 50051
      protocol: TCP
```

#### Compute Requirements

##### Per Engine Instance

| Resource | Minimum | Recommended | Large Monorepo |
|----------|---------|-------------|----------------|
| **CPU** | 2 cores | 4 cores | 8 cores |
| **RAM** | 1 GB | 4 GB | 8-16 GB |
| **Disk** | 5 GB | 20 GB | 50 GB |
| **GPU** | Not required | Not required | Optional (FAISS GPU) |

##### Resource Breakdown

| Component | CPU Impact | Memory Impact | Notes |
|-----------|-----------|---------------|-------|
| **FAISS Index** | Low (query) / High (build) | 500MB-4GB per repo | Scales linearly with chunk count; ~100 bytes per vector (384-dim) |
| **Tree-sitter Parsing** | High (indexing only) | ~50MB | 8 grammar libraries loaded; single-threaded per file |
| **Embedding (ONNX)** | High | ~200MB model | all-MiniLM-L6-v2; 4 threads; CPU-only is fine for <10K chunks |
| **Embedding (HTTP API)** | Minimal | Minimal | Network-bound; rate-limited by provider |
| **gRPC Server** | Low | ~50MB | Handles concurrent RPCs; uses thread pool |
| **Embedding Cache** | None | 100MB-1GB | LRU cache with binary disk persistence |
| **STM (Session)** | Minimal | ~10MB per active session | Ring buffer, auto-evicted |
| **MTM (Patterns)** | Minimal | ~10MB | Pattern graph + strategies |
| **Consolidation Thread** | Low (periodic) | Shared with LTM | Background: decay, cleanup, save |

##### Scaling Guidelines

| Workload | Instances | Instance Size | Storage |
|----------|-----------|---------------|---------|
| **Small team** (1-5 repos, <50K files total) | 1 | 2 CPU / 4 GB | Local filesystem |
| **Medium team** (5-20 repos, <500K files) | 2-3 | 4 CPU / 8 GB | S3 or MinIO |
| **Large org** (20-100 repos, monorepos) | 4-8 | 8 CPU / 16 GB | S3 with repo-partitioned routing |
| **Enterprise** (100+ repos, multi-region) | 8+ (per region) | 8 CPU / 16 GB | Regional S3 + CDN for model files |

##### Indexing vs. Query Performance

| Operation | Latency | CPU | Notes |
|-----------|---------|-----|-------|
| **Full repo index** (10K files) | 5-15 min | 4-8 cores saturated | Tree-sitter + embeddings; parallelizable |
| **Incremental update** (10 files) | 5-30 sec | 1-2 cores | Only re-chunks changed files |
| **Vector search** (top-20) | 5-50 ms | <1 core | FAISS IVF-PQ; near-constant time |
| **Build review context** | 50-200 ms | 1-2 cores | Search + heuristics + attention |
| **Heuristic checks** | 10-50 ms | <1 core | Regex-based, no ML |

##### Cloud Instance Recommendations

| Cloud | Instance Type | Specs | Monthly Cost (est.) |
|-------|--------------|-------|---------------------|
| **AWS** | `c6i.xlarge` | 4 vCPU / 8 GB | ~$125/mo |
| **GCP** | `c2-standard-4` | 4 vCPU / 16 GB | ~$140/mo |
| **Azure** | `Standard_F4s_v2` | 4 vCPU / 8 GB | ~$125/mo |
| **OCI** | `VM.Standard.E4.Flex` | 4 OCPU / 16 GB | ~$100/mo |

For indexing-heavy workloads (frequent repo changes), prefer compute-optimized instances.
For query-heavy workloads (many concurrent reviews), prefer memory-optimized instances for larger FAISS indexes.

### Azure DevOps Integration

Full support for Azure DevOps alongside GitHub/GitLab:
- **Authentication**: PAT and Azure AD OAuth (configurable URLs via XML)
- **Webhooks**: HMAC-SHA256 verification, `git.pullrequest.*` events
- **API**: PR diff, files, thread comments with iteration tracking
- **Configuration**: All URLs externalized (no hardcoded endpoints)

```xml
<azure-devops enabled="true"
              vssps-url="https://app.vssps.visualstudio.com"
              login-url="https://login.microsoftonline.com"
              webhook-secret="${AZURE_DEVOPS_WEBHOOK_SECRET}"/>
```

### LLM Provider Management

The `LLMProviderManager` reads all configuration from `rtserverprops.xml` (no `@Value` annotations):

```xml
<llm primary="openai" fallback="ollama" auto-discover-local="true">
    <openai api-key="${LLM_OPENAI_API_KEY}" 
            base-url="https://api.openai.com/v1"
            model="gpt-4-turbo-preview"/>
    <anthropic api-key="${LLM_ANTHROPIC_API_KEY}"
               models="claude-3-opus,claude-3-sonnet,claude-3-5-sonnet"/>
    <azure-openai endpoint="${LLM_AZURE_OPENAI_ENDPOINT}"
                  api-key="${LLM_AZURE_OPENAI_API_KEY}"
                  deployment="gpt-4"/>
    <ollama discovery-host="localhost" discovery-ports="11434,11435,8080"/>
    <custom base-url="${LLM_CUSTOM_BASE_URL}" api-key="${LLM_CUSTOM_API_KEY}"/>
</llm>
```

**Features:**
- **Health checks**: Via `LLMHealthCheckBackgroundTask` (IBackgroundTask framework)
- **Auto-discovery**: Detects Ollama at configured ports
- **Failover**: Automatic switch to fallback provider
- **REST API**: `GET /api/v1/llm/providers`, `POST /api/v1/llm/providers/switch`

### Ollama / Local LLM Support

LLMProviderManager with auto-discovery:
- Detects Ollama at configured ports (default: 11434, 11435, 8080)
- Health checks via `/api/tags`
- Automatic fallback to cloud providers
- REST endpoint: `GET /api/v1/llm/providers`

