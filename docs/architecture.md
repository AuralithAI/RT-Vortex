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
┌─────────────────────┐               ┌─────────────────────┐
│    Java Server      │    gRPC       │    C++ Engine       │
│                     │──────────────▶│                     │
│  - LLM config       │  Configure    │  - Storage backend  │
│  - DB/Redis         │  Storage()    │  - Index paths      │
│  - Engine client    │               │  - TLS settings     │
└─────────────────────┘               └─────────────────────┘
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

### Brain-Inspired Memory Architecture

The engine implements a novel memory system inspired by cognitive architectures:

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Memory Coordinator                             │
│                                                                     │
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐            │
│  │     LTM       │  │     STM       │  │     MTM       │            │
│  │ Long-Term     │  │ Short-Term    │  │ Meta-Task     │            │
│  │ Memory        │  │ Memory        │  │ Memory        │            │
│  │               │  │               │  │               │            │
│  │ FAISS Index   │  │ Session Cache │  │ Patterns      │            │
│  │ Vector Search │  │ Conversation  │  │ Strategies    │            │
│  │ Hybrid BM25   │  │ Query Cache   │  │ Feedback      │            │
│  └───────┬───────┘  └───────┬───────┘  └───────┬───────┘            │
│          │                  │                  │                    │
│          └──────────────────┼──────────────────┘                    │
│                             │                                       │
│                 ┌───────────▼───────────┐                           │
│                 │ Cross-Memory Attention │                          │
│                 │                        │                          │
│                 │ - Adaptive weights     │                          │
│                 │ - Context-aware fusion │                          │
│                 │ - Source reliability   │                          │
│                 └────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────────┘
```

**Memory Components:**

| Component | Purpose | Persistence |
|-----------|---------|-------------|
| LTM (Long-Term Memory) | Persistent repository knowledge with FAISS vector search | Yes (disk) |
| STM (Short-Term Memory) | Session context, conversation history, query cache | No (in-memory) |
| MTM (Meta-Task Memory) | Review patterns, strategies, learned behaviors | Yes (JSON) |
| Cross-Memory Attention | Weighted retrieval across all memories | N/A |

### Vector Search (FAISS)

Production-ready vector search with:
- **Index types**: IVF-PQ for memory efficiency, Flat for accuracy
- **BLAS backends**: OpenBLAS, MKL, or Apple Accelerate (auto-detected)
- **Fallback**: Brute-force cosine similarity when FAISS unavailable

### Storage Backend

Abstract storage interface supporting multiple backends, configured via `rtserverprops.xml`:

```
┌────────────────────────────────────────────────────────────────────┐
│                     StorageBackend Interface                       │
│   read(key) → data   |   write(key, data)   |   list(prefix)       │
└───────────────────────────────┬────────────────────────────────────┘
                                │
    ┌───────────┬───────────────┼───────────────┬───────────┐
    ▼           ▼               ▼               ▼           ▼
┌────────┐ ┌────────┐    ┌────────────┐    ┌────────┐ ┌────────┐
│ Local  │ │  AWS   │    │   Azure    │    │  GCP   │ │  OCI   │
│filesys │ │   S3   │    │   Blob     │    │  GCS   │ │ Object │
└────────┘ └────────┘    └────────────┘    └────────┘ └────────┘
                │
                ▼
         ┌────────────┐
         │   MinIO    │
         │ (S3-compat)│
         └────────────┘
```

**Configuration in rtserverprops.xml:**
```xml
<storage type="s3">
    <local base-path="./data"/>
    <s3 bucket="${S3_BUCKET}" region="${S3_REGION:us-east-1}" 
        endpoint="${S3_ENDPOINT:}"/>
    <gcs bucket="${GCS_BUCKET}" project="${GCS_PROJECT}"/>
    <azure container="${AZURE_CONTAINER}" account="${AZURE_STORAGE_ACCOUNT}"/>
    <oci namespace="${OCI_NAMESPACE}" bucket="${OCI_BUCKET}" 
         compartment="${OCI_COMPARTMENT}"/>
    <minio bucket="${MINIO_BUCKET}" endpoint="${MINIO_ENDPOINT}" 
           access-key="${MINIO_ACCESS_KEY}" secret-key="${MINIO_SECRET_KEY}"/>
</storage>
```

**Cloud Storage Features:**
- AWS Signature V4 authentication (no SDK dependency)
- Pre-signed URL generation
- IRSA/Workload Identity support for Kubernetes
- Compatible with S3, GCS, Azure Blob, OCI Object Storage, MinIO

### Tree-sitter AST Parsing

Intelligent code chunking using tree-sitter CST:
- 8 languages: C, C++, Java, Python, JavaScript, TypeScript, Go, Rust
- Preserves function/class boundaries
- Extracts docstrings and symbols
- Falls back to regex-based heuristics for unsupported languages

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

