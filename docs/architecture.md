# AI PR Reviewer - Architecture

## Quick Reference

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              EXTERNAL LAYER                                 │
│                                                                             │
│   GitHub/GitLab           CLI/SDKs            Web UI                        │
│   Webhooks                                                                  │
│       │                      │                  │                           │
│       │ (HTTP POST)          │ (gRPC)          │ (HTTP)                     │
│       ▼                      ▼                  ▼                           │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                     JAVA SERVER                                     │    │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐         │    │
│  │  │   REST API     │  │   gRPC API     │  │  Orchestrator  │         │    │
│  │  │   Port 8080    │  │   Port 9090    │  │                │         │    │
│  │  └────────┬───────┘  └────────┬───────┘  └───────┬────────┘         │    │
│  │           └───────────────────┴──────────────────┘                  │    │
│  │                          │                                          │    │
│  │           ┌──────────────┼──────────────┐                           │    │
│  │           │              │              │                           │    │
│  │           ▼              ▼              ▼                           │    │
│  │   ┌────────────┐  ┌────────────┐  ┌────────────┐                    │    │
│  │   │ PostgreSQL │  │   Redis    │  │ Engine     │                    │    │
│  │   │ Port 5432  │  │ Port 6379  │  │ Client     │                    │    │
│  │   └────────────┘  └────────────┘  └─────┬──────┘                    │    │
│  └─────────────────────────────────────────┼───────────────────────────┘    │
│                                            │                                │
│                                            │ gRPC (engine.proto)            │
│                                            │ Port 50051                     │
│                                            ▼                                │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                     C++ ENGINE (gRPC Server)                        │    │
│  │                                                                     │    │
│  │   ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐    │    │
│  │   │  Indexer   │  │  Retriever │  │ Heuristics │  │  Symbols   │    │    │
│  │   │  - AST     │  │  - Lexical │  │  - Rules   │  │  - Graph   │    │    │
│  │   │  - Chunks  │  │  - Vector  │  │  - Fast    │  │  - Refs    │    │    │
│  │   └────────────┘  └────────────┘  └────────────┘  └────────────┘    │    │
│  │                         │                                           │    │
│  │                         ▼                                           │    │
│  │                  ┌────────────────┐                                 │    │
│  │                  │ Local Storage  │  (No DB connection!)            │    │
│  │                  │ .aipr/index/   │                                 │    │
│  │                  └────────────────┘                                 │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
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

### Proto Definitions (`mono/proto/`)

gRPC service definitions:

| Proto File | Purpose | Who Uses It | Direction |
|------------|---------|-------------|-----------|
| `engine.proto` | Engine service | Java → C++ | **Internal** |
| `api.proto` | External API | SDKs/CLI → Java | **External** |
| `session.proto` | Auth/sessions | Shared types | Both |

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
| Local files | C++ Engine | Index data (`.aipr/index/`) |

**Important:** The C++ Engine has **no database connections**. It's a pure compute service that:
- Reads repository files from disk
- Stores index data locally
- Responds to gRPC requests

All persistence, queuing, and user management is handled by the Java Server.

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
| `config/default.yml` | Both | Unified config: indexing, retrieval, review, model settings |
| `config/rtserverprops.xml` | Java only | Server runtime: engine connection, DB, TLS |
| `config/large-repo.yml` | Override | Profile for monorepos |
| `config/perf-focused.yml` | Override | Profile prioritizing performance checks |
| `config/strict-security.yml` | Override | Profile for security-sensitive repos |

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
