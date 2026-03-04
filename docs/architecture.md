# RTVortex — System Architecture

## Quick Reference

```
External Clients (Webhooks, CLI, Web UI, SDKs)
         │
         │  REST / WebSocket (port 8080)
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                 Go API Server (RTVortexGo)                          │
│                                                                     │
│  REST API     ◄── chi router (32+ endpoints)                        │
│  WebSocket    ◄── Real-time review progress (coder/websocket)       │
│  Webhooks     ◄── GitHub, GitLab, Bitbucket, Azure DevOps           │
│  Auth         ◄── JWT + OAuth2 (6 providers) + AES-256-GCM          │
│  Metrics      ◄── Prometheus (16 counters/histograms/gauges)        │
│  Background   ◄── Scheduler (cleanup, health checks, indexing)      │
│  DB Layer     ◄── pgx/v5 → PostgreSQL                               │
│  Cache        ◄── go-redis/v9 → Redis                               │
│                                                                     │
│  gRPC Client  ──► engine.Pool → engine.Client                       │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ gRPC (port 50051)
                               ▼
                      ┌────────────────┐
                      │  C++ Engine    │
                      │  (rtvortex)    │
                      │  gRPC Server   │
                      └────────────────┘
```

**Port Summary:**

| Port | Service | Protocol | Access |
|------|---------|----------|--------|
| 8080 | Go REST API + WebSocket | HTTP/WS | External |
| 50051 | C++ Engine | gRPC | Internal only |
| 5432 | PostgreSQL | TCP | Internal only |
| 6379 | Redis | TCP | Internal only |

## Overview

RTVortex is a two-component system for automated code review powered by LLMs.

1. **C++ Engine (`rtvortex`)** — High-performance gRPC server for code indexing, retrieval, and heuristic analysis
2. **Go API Server (`RTVortexGo`)** — REST/WebSocket API for webhooks, authentication, orchestration, and LLM integration

```
┌─────────────────────────────────────────────────────────────────────┐
│                         External Clients                            │
│   (GitHub/GitLab/Bitbucket/Azure DevOps Webhooks, CLI, SDKs, UI)    │
└────────────────────────────┬────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        Go API Server                                │
│   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│   │  REST API   │  │  WebSocket  │  │  Webhooks   │                 │
│   │  (8080)     │  │  Progress   │  │  Handler    │                 │
│   └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                 │
│          │                │                │                        │
│          └────────────────┼────────────────┘                        │
│                           │                                         │
│   ┌───────────────────────▼───────────────────────┐                 │
│   │          Review Pipeline (12 steps)           │                 │
│   │  1. Validate → 2. Fetch diff → 3. Parse       │                 │
│   │  4. Skip patterns → 5. Chunk → 6. Index       │                 │
│   │  7. Build context → 8. Prompt → 9. LLM call   │                 │
│   │  10. Parse response → 11. Post comments       │                 │
│   │  12. Record review                            │                 │
│   └───────────────────────┬───────────────────────┘                 │
│                           │                                         │
│   ┌───────────────────────▼───────────────────────┐                 │
│   │           Engine Client (gRPC Pool)           │                 │
│   │  IndexRepository, Search, BuildContext        │                 │
│   └───────────────────────┬───────────────────────┘                 │
└───────────────────────────┼─────────────────────────────────────────┘
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
│   ┌───────────────────────────────────────────────┐                 │
│   │        TMS (Triune Memory System)             │                 │
│   │  LTM (FAISS) + STM (Session) + MTM (Patterns) │                 │
│   └───────────────────────────────────────────────┘                 │
└─────────────────────────────────────────────────────────────────────┘
```

## Components

### Go API Server (`mono/server-go/`)

The Go server handles external API, webhooks, authentication, and LLM orchestration.
Written in Go 1.24 using `chi/v5` for routing. Single statically-compiled binary (~20MB).

**Key responsibilities:**
- REST API (32+ endpoints) for users, reviews, repos, orgs, webhooks, LLM, admin
- OAuth2 authentication (GitHub, GitLab, Google, Microsoft, Bitbucket, LinkedIn)
- JWT token management with Redis-backed sessions
- 12-step review pipeline coordinating VCS → Engine → LLM → VCS
- WebSocket real-time review progress streaming
- SSE streaming for LLM completions
- Prometheus metrics (16 metrics across 7 subsystems)
- AES-256-GCM encryption for OAuth tokens at rest
- Redis-backed sliding window rate limiting (per category)
- Async audit logging for security events
- Background job scheduler (session cleanup, LLM health, index cleanup)
- Webhook receivers for all 4 VCS platforms with signature verification

See [Go Server Architecture](go-server-architecture.md) for detailed package layout and internals.

**REST API endpoints (port 8080):**

| Group | Endpoint | Method | Purpose |
|-------|----------|--------|---------|
| Health | `/health` | GET | Liveness probe |
| Health | `/ready` | GET | Readiness probe (checks DB + Redis + Engine) |
| Health | `/version` | GET | Version info |
| Metrics | `/metrics` | GET | Prometheus metrics (promhttp) |
| Docs | `/api/v1/docs/openapi.yaml` | GET | OpenAPI 3.0 spec |
| Auth | `/api/v1/auth/providers` | GET | List enabled OAuth providers |
| Auth | `/api/v1/auth/login/{provider}` | GET | Start OAuth2 flow |
| Auth | `/api/v1/auth/callback/{provider}` | GET | OAuth2 callback |
| Auth | `/api/v1/auth/refresh` | POST | Refresh JWT token |
| Auth | `/api/v1/auth/logout` | POST | Invalidate session |
| User | `/api/v1/user/me` | GET/PUT | Current user profile |
| Orgs | `/api/v1/orgs` | GET/POST | List/create organizations |
| Orgs | `/api/v1/orgs/{orgID}/members` | GET/POST/DELETE | Manage members |
| Repos | `/api/v1/repos` | GET/POST | List/register repositories |
| Repos | `/api/v1/repos/{repoID}/index` | POST | Trigger indexing |
| Reviews | `/api/v1/reviews` | GET/POST | List/trigger reviews |
| Reviews | `/api/v1/reviews/{id}` | GET | Review result |
| Reviews | `/api/v1/reviews/{id}/comments` | GET | Review comments |
| Reviews | `/api/v1/reviews/{id}/ws` | GET | WebSocket progress |
| LLM | `/api/v1/llm/providers` | GET | LLM provider status |
| LLM | `/api/v1/llm/providers/test` | POST | Test LLM connection |
| LLM | `/api/v1/llm/stream` | POST | SSE streaming completion |
| Admin | `/api/v1/admin/stats` | GET | System statistics |
| Admin | `/api/v1/admin/health/detailed` | GET | Detailed health |
| Webhooks | `/api/v1/webhooks/github` | POST | GitHub webhook |
| Webhooks | `/api/v1/webhooks/gitlab` | POST | GitLab webhook |
| Webhooks | `/api/v1/webhooks/bitbucket` | POST | Bitbucket webhook |
| Webhooks | `/api/v1/webhooks/azure-devops` | POST | Azure DevOps webhook |

### C++ Engine (`mono/engine/`)

The engine is a **standalone gRPC server** that handles compute-intensive operations:

- **Code Indexing**: Parses repositories, extracts symbols, creates embeddings
- **Retrieval**: Hybrid search (lexical + vector + graph) for relevant context
- **Heuristics**: Fast rule-based checks that don't require LLM
- **TMS**: Brain-inspired Triune Memory System (LTM + STM + MTM)

**Key Files:**
- `src/server/main.cpp` — Server entry point, CLI parsing, signal handling
- `src/server/engine_service_impl.cpp` — gRPC service implementation
- `include/engine_api.h` — Core Engine API

**Communication:**
- Listens on port **50051** (configurable via `--port` or `ENGINE_PORT`)
- Protocol: gRPC (defined in `proto/engine.proto`)
- TLS optional (controlled by `ENGINE_TLS_ENABLED`)

**Startup:**
```bash
# Direct
./bin/rtvortex --host 0.0.0.0 --port 50051 --config config/default.yml

# Via Makefile
make run-engine

# Environment variable
RTVORTEX_HOME=/path/to/rt_home ./bin/rtvortex --server
```

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
    C++ Engine                                  Go Server
    (CMake reads directly)                 (protoc-gen-go generates
                                            Go stubs at build time)
```

| Proto File | Purpose | Direction |
|------------|---------|-----------|
| `engine.proto` | Engine service | Go → C++ (Internal) |
| `api.proto` | External API | SDKs/CLI → Go (External) |
| `session.proto` | Auth/sessions | Shared types |

## How Go Interacts with C++ Engine

The Go server communicates with the C++ engine exclusively through gRPC. The connection is managed by two key components:

### Connection Pool (`internal/engine/pool.go`)

```
Go Server                                        C++ Engine
─────────                                        ──────────
┌─────────────────┐                                 ┌────────────┐
│  engine.Pool    │──── gRPC channels (N) ───────▶ │  :50051    │
│                 │     (round-robin)               │            │
│  healthCheckLoop│──── connectivity state ───────▶│  gRPC      │
│  (every 10s)    │     machine monitoring          │  Server    │
│                 │                                 │            │
│  checkAndReconn │──── automatic reconnect ──────▶│            │
│  ect()          │     on state change             │            │
└─────────────────┘                                 └────────────┘
        │
        ▼ Prometheus gauges
  engine_pool_total_conns
  engine_pool_healthy_conns
```

- Maintains a pool of `MaxChannels` gRPC connections (default 4)
- Background health check loop monitors `connectivity.State` every 10 seconds
- Automatic reconnection on `TransientFailure` or `Shutdown` states
- Prometheus gauges track pool health in real time

### gRPC Client (`internal/engine/client.go`)

Wraps all engine RPCs into typed Go methods:

| Go Method | gRPC RPC | Purpose |
|-----------|----------|---------|
| `IndexRepository()` | `IndexRepository` | Index a repo's source files |
| `Search()` | `Search` | Hybrid search (lexical + vector + graph) |
| `BuildContext()` | `BuildContext` | Build review context from diff |
| `GetIndexStatus()` | `GetIndexStatus` | Check indexing progress |
| `ConfigureStorage()` | `ConfigureStorage` | Push storage config at startup |

### Review Data Flow

```
1. Webhook arrives at Go server (e.g., POST /api/v1/webhooks/github)
2. Go verifies webhook signature, extracts PR metadata
3. Go fetches diff from VCS platform (GitHub/GitLab/Bitbucket/Azure)
4. Go sends diff to C++ Engine via gRPC:
   a. engine.IndexRepository() — ensure repo is indexed
   b. engine.BuildContext() — get relevant code context for diff
5. Go constructs LLM prompt from context + diff
6. Go calls LLM provider (OpenAI/Anthropic/Ollama)
7. Go parses LLM response into structured review comments
8. Go posts comments back to VCS platform via REST API
9. Go records review in PostgreSQL + emits WebSocket progress events
```

```
┌──────────┐     ┌───────────────┐     ┌────────────┐     ┌─────────┐
│  GitHub  │────▶│  Go Server    │────▶│  C++ Engine │     │  OpenAI │
│  Webhook │     │               │     │  (gRPC)     │     │  (HTTP) │
└──────────┘     │  1. Verify    │     │             │     └────▲────┘
                 │  2. Fetch diff│     │  3. Index   │          │
                 │  4. ──────────│────▶│  5. Context │          │
                 │  6. Prompt ───│─────│─────────────│──────────┘
                 │  7. Parse     │     │             │
                 │  8. Comment──▶│     └─────────────┘
                 │  9. Record    │
                 └───────────────┘
```

## Database & Storage

| Store | Component | Purpose |
|-------|-----------|---------|
| PostgreSQL | **Go server only** | Users, reviews, repos, orgs, audit logs, webhooks, usage |
| Redis | **Go server only** | Sessions, rate limiting, cache |
| Cloud Storage | **C++ Engine** | Index data, embeddings (S3/GCS/Azure/OCI/MinIO) |
| Local files | C++ Engine | Index data (`.aipr/index/`) for local mode |

### Database Schema (PostgreSQL)

The Go server manages 11 tables:

| Table | Purpose |
|-------|---------|
| `users` | User accounts (from OAuth) |
| `oauth_identities` | OAuth provider credentials (AES-256-GCM encrypted) |
| `organizations` | Multi-tenant organizations |
| `org_members` | Organization membership + roles |
| `repositories` | Registered repositories |
| `reviews` | Review history + results |
| `review_comments` | Individual review comments |
| `usage_daily` | Daily usage statistics |
| `webhook_events` | Received webhook audit trail |
| `audit_log` | Security event log |
| `schema_info` | Schema version tracking |

Schema scripts are in `mono/server-go/db/sql/initData.sql` and copied to `rt_home/data/sql/` during build.

**Storage Configuration Flow:**
```
rtserverprops.xml (Single Source of Truth)
         │
         ▼
┌──────────────────┐     ConfigureStorage RPC       ┌──────────────────┐
│   Go Server      │ ─────────────────────────────▶│   C++ Engine     │
│   (reads XML)    │                                │ (builds backend) │
└──────────────────┘                                └──────────────────┘
```

The Go server reads storage configuration from `rtserverprops.xml` and pushes it to the C++ engine via the `ConfigureStorage` gRPC call at startup.

**Important:** The C++ Engine has **no database connections**. It's a pure compute service.

## Deployment Models

### 1. Single Host (Development)

Both components on same machine:

```bash
# Option A: Makefile (recommended)
make run

# Option B: Manual
# Terminal 1: Start engine
RTVORTEX_HOME=./rt_home ./rt_home/bin/rtvortex --server

# Terminal 2: Start server
RTVORTEX_HOME=./rt_home ./rt_home/bin/RTVortexGo
```

### 2. Docker Compose

```bash
docker compose up -d
```

Services:
- `engine` — C++ gRPC server (internal, port 50051)
- `server` — Go server (exposed 8080)
- `postgres` — Database
- `redis` — Cache

### 3. Kubernetes / Helm

```bash
helm install rtvortex deploy/helm/rtvortex/
```

### 4. Separate Hosts

**Engine Host:**
```bash
ENGINE_HOST=0.0.0.0 ENGINE_PORT=50051 ./bin/rtvortex --server
```

**Server Host:**
```bash
ENGINE_HOST=engine.internal.example.com ENGINE_PORT=50051 ./bin/RTVortexGo
```

## Configuration

### Configuration Files

| File | Used By | Purpose |
|------|---------|---------|
| `config/rtserverprops.xml` | **Both** (via gRPC) | Single source of truth: DB, Redis, LLM, Storage, Engine |
| `config/vcsplatforms.xml` | Go server | Platform OAuth (GitHub, GitLab, Bitbucket, Azure DevOps) |
| `config/default.yml` | C++ Engine | Indexing, retrieval, review settings |

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
│    Go Server        │    gRPC        │    C++ Engine       │
│                     │──────────────▶│                     │
│  - LLM config       │  Configure     │  - Storage backend  │
│  - DB/Redis         │  Storage()     │  - Index paths      │
│  - Engine client    │                │  - TLS settings     │
│  - OAuth providers  │                │                     │
└─────────────────────┘                └─────────────────────┘
```

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `RTVORTEX_HOME` | Auto-discovered | Root directory for config, data, temp |
| `ENGINE_HOST` | `localhost` | C++ engine hostname |
| `ENGINE_PORT` | `50051` | C++ engine port |
| `ENGINE_TLS_ENABLED` | `false` | Enable TLS for gRPC |

## CI/CD Pipelines

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `engine.yml` | Changes to `mono/engine/**` | C++ build, test, package |
| `server.yml` | Changes to `mono/server-go/**` | Go build, test, vet, coverage |
| `ci.yml` | All pushes | Integration tests with both |
| `release.yml` | Merge to main | Version bump, publish artifacts |

## Security

- **TLS/mTLS**: Both engine and server support TLS
- **Authentication**: JWT for REST API, OAuth2 for user login (6 providers)
- **Token Encryption**: AES-256-GCM for OAuth tokens stored in PostgreSQL
- **Rate Limiting**: Redis-backed sliding window (per category: api, auth, webhook)
- **Audit Logging**: Async fire-and-forget security event logging to PostgreSQL
- **Webhook Verification**: HMAC signature verification for all 4 VCS platforms
- **Network Isolation**: Engine doesn't need public access

## Observability

### Prometheus Metrics

The Go server exports 16 metrics at `GET /metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `rtvortex_http_requests_total` | Counter | HTTP requests by method/path/status |
| `rtvortex_http_request_duration_seconds` | Histogram | Request latency |
| `rtvortex_review_requests_total` | Counter | Reviews by platform/status |
| `rtvortex_review_duration_seconds` | Histogram | End-to-end review time |
| `rtvortex_review_pipeline_step_duration_seconds` | Histogram | Per-step pipeline timing |
| `rtvortex_llm_requests_total` | Counter | LLM calls by provider/model/status |
| `rtvortex_llm_request_duration_seconds` | Histogram | LLM latency |
| `rtvortex_llm_tokens_total` | Counter | Token usage |
| `rtvortex_engine_rpc_total` | Counter | Engine gRPC calls |
| `rtvortex_engine_rpc_duration_seconds` | Histogram | Engine gRPC latency |
| `rtvortex_engine_pool_total_conns` | Gauge | Total pool connections |
| `rtvortex_engine_pool_healthy_conns` | Gauge | Healthy pool connections |
| `rtvortex_ws_active_connections` | Gauge | Active WebSocket connections |
| `rtvortex_ws_messages_sent_total` | Counter | WebSocket messages sent |
| `rtvortex_auth_events_total` | Counter | Auth events |
| `rtvortex_rate_limit_rejections_total` | Counter | Rate limit rejections |

### Health Checks

| Endpoint | Purpose | Checks |
|----------|---------|--------|
| `GET /health` | Liveness probe | Always returns 200 |
| `GET /ready` | Readiness probe | PostgreSQL + Redis + Engine connectivity |
| `GET /version` | Version info | Build version, commit hash, build date |

## C++ Engine Internals

### Brain-Inspired Memory Architecture (TMS)

~16,300 lines of C++17 across 35 source files and 17 headers.

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
│                 └───────────┬────────────┘                          │
│                             │                                       │
│                 ┌───────────▼────────────┐                          │
│                 │  Compute Controller    │                          │
│                 │ FAST/BALANCED/THOROUGH │                          │
│                 └────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────────┘
```

### Build Dependencies (C++)

| Dependency | Purpose | How Resolved |
|------------|---------|--------------|
| **libcurl** | HTTP embedding API calls | System package |
| **nlohmann/json** | JSON parsing | FetchContent |
| **ONNX Runtime** | Local embedding inference | FetchContent |
| **FAISS** | Vector similarity search | System (requires BLAS) |
| **tree-sitter** | AST parsing (8 grammars) | FetchContent |
| **gRPC + Protobuf** | Server communication | FetchContent |
| **Google Test** | Unit tests | FetchContent |

### Cross-Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| Linux x86_64 | ✅ Primary | Full build, CI tested |
| Linux ARM64 | ✅ Supported | Cross-compile in CI |
| macOS x64 | ✅ Supported | Homebrew deps |
| macOS ARM64 (M1+) | ✅ Supported | Native ARM64 |
| Windows x64 | ✅ Supported | vcpkg deps, MSVC |

### Scaling: Multi-Instance Deployment

```
                    ┌──────────────────────┐
                    │  Go Server(s)        │
                    │  (Stateless, N×)     │
                    └──────────┬───────────┘
                               │
                    ┌──────────▼───────────┐
                    │   gRPC Load Balancer │
                    │   (Envoy / K8s Svc)  │
                    └──┬─────┬─────┬───────┘
                       │     │     │
              ┌────────┘     │     └────────┐
              ▼              ▼              ▼
      ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
      │ Engine #1    │ │ Engine #2    │ │ Engine #3    │
      │ repos: A,B   │ │ repos: C,D   │ │ repos: E,F   │
      └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
             └────────────────┼────────────────┘
                              ▼
                   ┌──────────────────────┐
                   │   Shared Storage     │
                   │   (S3/GCS/MinIO)     │
                   └──────────────────────┘
```

| Workload | Engines | Instance Size | Storage |
|----------|---------|---------------|---------|
| Small team (1-5 repos) | 1 | 2 CPU / 4 GB | Local |
| Medium (5-20 repos) | 2-3 | 4 CPU / 8 GB | S3 / MinIO |
| Large org (20-100 repos) | 4-8 | 8 CPU / 16 GB | S3 partitioned |
| Enterprise (100+ repos) | 8+ per region | 8 CPU / 16 GB | Regional S3 |

## Version Management

Version is **fully automated** — no manual edits required:

```
release.yml (merge to main)
    │
    ├── Bumps mono/VERSION → creates git tag
    │
    ▼
┌──────────────────────┐          ┌──────────────────────┐
│  C++ Engine (CMake)  │          │  Go Server (ldflags) │
│                      │          │                      │
│  git describe →      │          │  -X main.version=    │
│  version.h           │          │  -X main.commit=     │
│  AIPR_VERSION        │          │  -X main.buildDate=  │
│  AIPR_GIT_HASH       │          │                      │
└──────────────────────┘          └──────────────────────┘
```

Both binaries share the same version from `git describe --tags`.
