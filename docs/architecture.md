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
│  REST API     ◄── chi router (60+ endpoints)                        │
│  WebSocket    ◄── Real-time review + swarm progress                 │
│  Webhooks     ◄── GitHub, GitLab, Bitbucket, Azure DevOps           │
│  Auth         ◄── JWT + OAuth2 (6 providers) + AES-256-GCM          │
│  Vault        ◄── Per-user keychain (BIP39 recovery, server KEK)    │
│  Swarm        ◄── 9-agent teams, ELO scoring, task pipeline         │
│  Cross-Repo   ◄── Repo links, federated search, dep graph           │
│  RAG Chat     ◄── Codebase Q&A with citations (SSE streaming)       │
│  PR Sync      ◄── Background PR discovery + pre-embedding           │
│  MCP          ◄── Tool integrations (Jira, Slack, Linear, custom)   │
│  Benchmarks   ◄── Automated review quality evaluation (ELO)         │
│  Metrics      ◄── Prometheus (25+ counters/histograms/gauges)       │
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
                               ▲
                               │ gRPC + Redis Streams
                      ┌────────┴───────┐
                      │ Python Agent   │
                      │ Swarm (opt.)   │
                      └────────────────┘
```

**Port Summary:**

| Port | Service | Protocol | Access |
|------|---------|----------|--------|
| 8080 | Go REST API + WebSocket | HTTP/WS | External |
| 3000 | Next.js Dashboard | HTTP | External |
| 50051 | C++ Engine | gRPC | Internal only |
| 5432 | PostgreSQL | TCP | Internal only |
| 6379 | Redis | TCP | Internal only |

## Overview

RTVortex is a multi-component system for automated code review powered by LLMs and multi-agent swarms.

1. **C++ Engine (`rtvortex`)** — High-performance gRPC server for code indexing, retrieval, knowledge graph, and heuristic analysis
2. **Go API Server (`RTVortexGo`)** — REST/WebSocket API for webhooks, authentication, orchestration, LLM integration, swarm coordination, cross-repo analysis, vault, and chat
3. **Python Agent Swarm (optional)** — 9 specialized AI agents that collaborate on complex reviews via task pipelines
4. **Next.js Dashboard** — Web UI for repo management, review history, knowledge graph visualization, swarm monitoring, and settings

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
│   ┌───────────┐  ┌────────┴─────┐  ┌──────────┐  ┌──────────┐       │
│   │  Swarm    │  │  Cross-Repo  │  │  Vault   │  │  RAG     │       │
│   │  Teams    │  │  Observatory │  │ Keychain │  │  Chat    │       │
│   │  ELO/HITL │  │  Fed Search  │  │ BIP39    │  │  SSE     │       │
│   └───────────┘  └──────────────┘  └──────────┘  └──────────┘       │
│                                                                     │
│   ┌───────────────────────▼───────────────────────┐                 │
│   │           Engine Client (gRPC Pool)           │                 │
│   │  IndexRepository, Search, BuildContext,       │                 │
│   │  GetRepoFileMap, FederatedSearch, Manifest    │                 │
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
│   ┌───────────────────────────────────────────────┐                 │
│   │    Knowledge Graph (SQLite WAL)               │                 │
│   │  Nodes: file_summary, function, class, module │                 │
│   │  Edges: IMPORTS, CALLS, CONTAINS, REFERENCES  │                 │
│   │  inferFileEdges() for file-level aggregation  │                 │
│   └───────────────────────────────────────────────┘                 │
└─────────────────────────────────────────────────────────────────────┘
                            ▲
                            │ gRPC + Redis Streams
┌───────────────────────────┴─────────────────────────────────────────┐
│                    Python Agent Swarm (Optional)                    │
│   ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐               │
│   │Orchestr. │ │Architect │ │Senior Dev│ │Junior Dev│               │
│   └──────────┘ └──────────┘ └──────────┘ └──────────┘               │
│   ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │
│   │    QA    │ │ Security │ │   Docs   │ │   Ops    │ │  UI/UX   │  │
│   └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────┘  │
│                                                                     │
│   Auth: per-agent JWT    Tasks: Redis Streams + polling             │
│   LLM: proxied via Go   Feedback: ELO scoring + HITL                │
└─────────────────────────────────────────────────────────────────────┘
```

## Components

### Go API Server (`mono/server-go/`)

The Go server handles external API, webhooks, authentication, LLM orchestration, swarm coordination, cross-repo analysis, vault, and chat.
Written in Go 1.24 using `chi/v5` for routing. Single statically-compiled binary (~20MB).

**Key responsibilities:**
- REST API (60+ endpoints) for users, reviews, repos, orgs, webhooks, LLM, swarm, chat, cross-repo, vault, MCP, admin
- OAuth2 authentication (GitHub, GitLab, Google, Microsoft, Bitbucket, LinkedIn)
- JWT token management with Redis-backed sessions
- Per-user encrypted keychain with BIP39 recovery phrases
- 12-step review pipeline coordinating VCS → Engine → LLM → VCS
- Agent swarm infrastructure (9 roles, ELO scoring, task pipeline, HITL gates)
- Cross-repo observatory (repo linking, federated search, dependency graph)
- RAG chat with SSE streaming and code citations
- PR sync worker (background VCS polling, pre-embedding)
- MCP tool integrations (Jira, Slack, Linear, custom templates)
- Benchmark runner for review quality evaluation
- WebSocket real-time review + swarm progress streaming
- SSE streaming for LLM completions
- Prometheus metrics (25+ metrics across 10+ subsystems)
- AES-256-GCM encryption for OAuth tokens at rest
- Redis-backed sliding window rate limiting (per category)
- Async audit logging for security events
- Background job scheduler (session cleanup, LLM health, index cleanup, swarm janitor)

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
| Chat | `/api/v1/chat/sessions` | GET/POST | Chat session management |
| Chat | `/api/v1/chat/sessions/{id}/messages` | POST | Send message (SSE streaming response with citations) |
| Swarm | `/api/v1/swarm/tasks` | GET/POST | List/create swarm tasks |
| Swarm | `/api/v1/swarm/tasks/{id}` | GET/PUT | Task status, plan approval, cancellation |
| Swarm | `/api/v1/swarm/tasks/{id}/feedback` | POST | Human feedback (ELO scoring) |
| Swarm | `/api/v1/swarm/teams` | GET | List active swarm teams |
| Swarm | `/api/v1/swarm/agents` | GET | List registered agents + ELO scores |
| Swarm | `/api/v1/swarm/ws` | GET | WebSocket swarm activity feed |
| Cross-Repo | `/api/v1/repos/{repoID}/links` | GET/POST | Manage cross-repo links |
| Cross-Repo | `/api/v1/repos/{repoID}/links/{linkID}` | GET/PUT/DELETE | Single link CRUD |
| Cross-Repo | `/api/v1/repos/{repoID}/cross-repo/manifest` | GET | Repo structural manifest |
| Cross-Repo | `/api/v1/repos/{repoID}/cross-repo/dependencies` | GET | Cross-repo dependencies |
| Cross-Repo | `/api/v1/repos/{repoID}/cross-repo/search` | POST | Federated search across linked repos |
| Cross-Repo | `/api/v1/orgs/{orgID}/cross-repo/graph` | POST | Build org-level dependency graph |
| File Map | `/api/v1/repos/{repoID}/file-map` | GET | Knowledge graph (nodes + edges for visualization) |
| Vault | `/api/v1/vault/keychain/status` | GET | Keychain initialization status |
| Vault | `/api/v1/vault/keychain/init` | POST | Initialize per-user keychain (returns BIP39 phrase) |
| Vault | `/api/v1/vault/keychain/secrets` | GET/PUT | List/store keychain secrets |
| Vault | `/api/v1/vault/keychain/recover` | POST | Recover keychain with BIP39 phrase |
| MCP | `/api/v1/mcp/providers` | GET | List MCP providers + connection status |
| MCP | `/api/v1/mcp/providers/{name}/connect` | POST | Connect to MCP provider |
| MCP | `/api/v1/mcp/providers/{name}/test` | POST | Test MCP connection |
| PR Sync | `/api/v1/repos/{repoID}/prs` | GET | List tracked pull requests |
| Benchmarks | `/api/v1/benchmarks/run` | POST | Start benchmark evaluation |
| Benchmarks | `/api/v1/benchmarks/runs/{id}` | GET | Benchmark run results |

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

The Go server manages 20+ tables:

| Table | Purpose |
|-------|---------|
| `users` | User accounts (from OAuth) |
| `oauth_identities` | OAuth provider credentials (AES-256-GCM encrypted) |
| `organizations` | Multi-tenant organizations |
| `org_members` | Organization membership + roles |
| `repositories` | Registered repositories |
| `repo_members` | Per-repo membership and roles |
| `reviews` | Review history + results |
| `review_comments` | Individual review comments |
| `tracked_pull_requests` | Discovered PRs from VCS platforms (PR sync) |
| `chat_sessions` | RAG chat conversation sessions |
| `chat_messages` | Individual chat messages with citations |
| `repo_links` | Cross-repo directed links (observatory) |
| `repo_link_events` | Audit trail for link mutations |
| `swarm_teams` | Active swarm team instances |
| `swarm_agents` | Registered swarm agents (role, ELO, heartbeat) |
| `swarm_tasks` | Swarm task queue and lifecycle |
| `mcp_connections` | Active MCP provider connections |
| `mcp_call_log` | MCP tool call audit trail |
| `keychain_keys` | Per-user wrapped master keys (keychain) |
| `keychain_secrets` | Encrypted per-user secrets |
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
| `config/default.yml` | C++ Engine | Indexing, retrieval, review settings |

VCS platform credentials (OAuth tokens, webhook secrets) are configured per-user
via the dashboard UI and stored in the encrypted vault.

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
| `OAUTH_BASE_URL` | (derived) | External URL for OAuth callbacks (needed when binding `0.0.0.0`) |
| `LLM_OPENAI_API_KEY` | — | OpenAI API key |
| `LLM_ANTHROPIC_API_KEY` | — | Anthropic API key |
| `LLM_GEMINI_API_KEY` | — | Google Gemini API key |
| `LLM_GROK_API_KEY` | — | xAI Grok API key |
| `TOKEN_ENCRYPTION_KEY` | — | 32-byte hex key for AES-256-GCM vault + keychain |

## Agent Swarm

The agent swarm is an **optional** component that enables multi-agent collaborative reviews. Python agents authenticate with per-agent JWTs and communicate through Go, which holds all credentials.

### Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│  Go Server (Credential Holder)                                     │
│                                                                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐            │
│  │ TaskMgr  │  │ TeamMgr  │  │ ELO Svc  │  │ LLMProxy │            │
│  │ assign   │  │ form/    │  │ score    │  │ proxy    │            │
│  │ pipeline │  │ disband  │  │ promote  │  │ to LLM   │            │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                          │
│  │ PRCreator│  │ MemorySvc│  │ MCPCaller│                          │
│  │ auto-PR  │  │ HITL     │  │ tools    │                          │
│  └──────────┘  └──────────┘  └──────────┘                          │
└────────────────────────┬───────────────────────────────────────────┘
                         │  internal API + Redis Streams
┌────────────────────────▼───────────────────────────────────────────┐
│  Python Agent Swarm                                                │
│                                                                    │
│  9 Specialized Agents:                                             │
│  ┌─────────────┐ ┌──────────┐ ┌───────────┐ ┌───────────┐          │
│  │ Orchestrator│ │ Architect│ │ Senior Dev│ │ Junior Dev│          │
│  └─────────────┘ └──────────┘ └───────────┘ └───────────┘          │
│  ┌─────────┐ ┌──────────┐ ┌──────┐ ┌──────┐ ┌──────┐               │
│  │   QA    │ │ Security │ │ Docs │ │  Ops │ │UI/UX │               │
│  └─────────┘ └──────────┘ └──────┘ └──────┘ └──────┘               │
│                                                                    │
│  Auth: X-Service-Secret → per-agent JWT                            │
│  LLM: all calls proxied through Go (never direct)                  │
│  Tools: MCP tool calls via Go endpoints                            │
└────────────────────────────────────────────────────────────────────┘
```

### Task Lifecycle

```
submitted → planning → plan_review (HITL gate) → implementing
         → self_review → diff_review → pr_creating → completed
```

- **ELO Scoring**: Agents receive 1-5 human ratings mapped to ELO (K=32, baseline 1200)
- **Auto-Tier**: Background process promotes/demotes agents based on ELO thresholds
- **Team Formation**: Dynamic teams (max 5 concurrent), warm pool for instant startup
- **Janitor**: Background goroutine cleans up idle teams, stale heartbeats, offline agents

## Encrypted Vault & Keychain

```
┌─────────────────────────────────────────────────────────────────┐
│  Per-User Keychain                                              │
│                                                                 │
│  Master Key ─── wrapped by Server KEK ──► PostgreSQL            │
│       │                                                         │
│       ├── Derived Key (encrypt)  ──► encrypt secrets            │
│       └── Derived Key (HMAC)     ──► integrity check            │
│                                                                 │
│  Recovery: 12-word BIP39 phrase → re-derive master key          │
│  Cache: in-memory derived keys with TTL (30 min default)        │
│  Eviction: background goroutine purges expired keys             │
│                                                                 │
│  Stored Secrets:                                                │
│    LLM API keys, model preferences, agent routes,               │
│    VCS tokens, MCP tokens, custom settings                      │
└─────────────────────────────────────────────────────────────────┘
```

- **Server KEK**: Derived from `TOKEN_ENCRYPTION_KEY` — in production use HSM/KMS
- **Startup Rehydration**: LLM keys, model choices, and routes loaded from keychain at server start
- **Pluggable Backend**: `vault.SecretStore` interface — file-based for dev, swap in HashiCorp Vault / AWS SM / GCP SM

## Cross-Repo Observatory

```
┌────────────────────────────────────────────────────────────────────┐
│  Org-Level Cross-Repo Graph                                        │
│                                                                    │
│    Repo A ──── IMPORTS ────► Repo B                                │
│      │                        │                                    │
│      └──── REFERENCES ───────►Repo C                               │
│                                                                    │
│  Share Profiles: full | symbols | metadata | none                  │
│  Authorizer: checks user membership + link share profile           │
└────────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Purpose |
|-----------|---------|
| `Authorizer` | Per-link access control based on share profiles and org membership |
| `Handler` | CRUD for cross-repo links with audit logging |
| `DepGraphService` | Structural manifests, cross-repo dependency edges, org-level build graph |
| `FederatedSearchService` | Query across linked repos; merges and score-normalizes results |
| `GraphHandler` | HTTP endpoints for dependency graph and federated search |
| `PipelineEnricher` | Injects cross-repo context into the review pipeline |

### Federated Search Flow

```
1. User queries from Repo A
2. FederatedSearchService fetches all repos linked to A
3. Authorizer checks user access + share profile for each
4. Engine executes parallel searches across authorized repos
5. Results merged, score-normalized, returned with attribution
```

## Knowledge Graph Visualization

The dashboard includes an interactive knowledge graph viewer for each indexed repository.

### Renderers

| Renderer | Library | When Used | Features |
|----------|---------|-----------|----------|
| **DOM** | @xyflow/react v12 | ≤500 nodes | Rich DOM labels, edge click, detail panels |
| **WebGL** | cosmos.gl v2 | >500 nodes | GPU-accelerated, 1000s of nodes, cluster labels |

### WebGL Renderer Details

- **Colors**: `setPointColors(Float32Array)` → GPU shader, values normalized 0–1
- **Language Palette**: 22 language colors for `file_summary` nodes (Python blue, TypeScript teal, Rust copper, etc.)
- **Simulation**: Cosmograph-like tuning — high repulsion, cluster strength, link spring for tight directory clusters
- **Cluster Labels**: Directory names rendered at cluster centroids via `spaceToScreenPosition()`
- **Dark/Light Mode**: MutationObserver watches `<html>` class for theme changes, rebuilds graph

### File-Level Edge Inference (C++ Engine)

When the UI requests `file_summary` nodes only, there are typically no direct edges between them. The engine's `inferFileEdges()` method:

1. JOINs `kg_edges` with `kg_nodes` on both endpoints to get `file_path`
2. Groups by `(src_file_path, dst_file_path, edge_type)` with `COUNT(*)` as weight
3. Returns synthetic file-level edges with negative IDs
4. ~85 lines of SQL, indexed on `repo_id`, `file_path`, `src_id`, `dst_id`

## RAG Chat

```
User Question → Engine Search (FREE: semantic + lexical + graph)
                     │
                     ▼  relevant code chunks
              Build Prompt (question + context + conversation history)
                     │
                     ▼
              LLM Synthesis (streamed via SSE)
                     │
                     ▼
              Response with citations to files/lines
```

- **Conversation History**: Configurable window (default 10 messages)
- **Context Chunks**: Up to 10 code chunks retrieved per question
- **Zero-Cost Retrieval**: All search is handled by the C++ engine (no LLM tokens)
- **Temperature**: 0.3 for precise code-focused answers

## PR Sync Worker

Background service that keeps the tracked pull requests table up to date:

| Config | Default | Description |
|--------|---------|-------------|
| `SyncInterval` | 5 min | How often to poll VCS platforms |
| `EmbedInterval` | 2 min | How often to check for PRs needing embedding |
| `MaxPRsPerRepo` | 200 | Open PRs fetched per repository |
| `MaxConcurrentSyncs` | 4 | Parallel repo sync workers |
| `StaleAfter` | 24h | Mark unseen PRs as stale |
| `EnableEmbedding` | true | Pre-embed PR diffs in the engine |

## MCP Integrations

The Model Context Protocol (MCP) system provides tool integrations for agents and the review pipeline:

- **Built-in Providers**: Jira, Slack, Linear (with OAuth token management via vault)
- **Custom Templates**: Define custom MCP actions with validation, simulation, and testing
- **Per-User Tokens**: MCP tokens stored in the per-user keychain via `VaultFactory`
- **Metrics**: `rtvortex_mcp_calls_total`, `rtvortex_mcp_call_duration_seconds`, `rtvortex_mcp_active_connections`

## CI/CD Pipelines

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `engine.yml` | Changes to `mono/engine/**` | C++ build, test, package |
| `server.yml` | Changes to `mono/server-go/**` | Go build, test, vet, coverage |
| `ci.yml` | All pushes | Integration tests with both |
| `release.yml` | Merge to main | Version bump, publish artifacts |

## Security

- **TLS/mTLS**: Both engine and server support TLS
- **Authentication**: JWT for REST API, OAuth2 for user login (6 providers), per-agent JWT for swarm
- **Per-User Keychain**: AES-256-GCM encrypted secrets with BIP39 recovery, server KEK wrapping
- **Token Encryption**: AES-256-GCM for OAuth tokens stored in PostgreSQL
- **Rate Limiting**: Redis-backed sliding window (per category: api, auth, webhook)
- **Audit Logging**: Async fire-and-forget security event logging to PostgreSQL (includes cross-repo link events)
- **Webhook Verification**: HMAC signature verification for all 4 VCS platforms
- **Swarm Auth**: Service secret (derived from JWT key) for agent registration; per-agent JWTs for all subsequent calls
- **Cross-Repo Authorizer**: Share-profile-based access control for cross-repo data exposure
- **Network Isolation**: Engine doesn't need public access

## Self-Healing Pipeline (Phase 11)

Automatic resilience and recovery for the LLM agent swarm:

```
  Agent LLM Call
        │
        ▼
  ┌─────────────┐    success/failure
  │ Go Proxy    │ ──────────────────► ┌────────────────────────┐
  │ /llm/probe  │                     │  SelfHealService       │
  └─────────────┘                     │                        │
                                      │  ┌──────────────────┐  │
                                      │  │ Circuit Breakers │  │
                                      │  │ per-provider     │  │
                                      │  │ closed → open →  │  │
                                      │  │ half_open → …    │  │
                                      │  └──────────────────┘  │
                                      │                        │
                                      │  ┌──────────────────┐  │
                                      │  │ Stuck Task       │  │
                                      │  │ Detector         │  │
                                      │  │ (30s loop)       │  │
                                      │  └──────────────────┘  │
                                      │                        │
                                      │  ┌──────────────────┐  │
                                      │  │ Audit Event Log  │  │
                                      │  │ (PostgreSQL)     │  │
                                      │  └──────────────────┘  │
                                      └────────────────────────┘
```

### Circuit Breaker State Machine

Each LLM provider has an independent circuit breaker:

| Transition | Trigger | Effect |
|------------|---------|--------|
| closed → open | 5 consecutive failures | Block traffic for 2 min |
| open → half_open | Open duration expires | Allow limited probe traffic |
| half_open → closed | 3 successes in half_open | Fully restore traffic |
| half_open → open | Any failure in half_open | Re-open for another 2 min |
| * → closed | Manual reset via dashboard | Admin override |

### Self-Heal Event Types

| Event | Severity | Description |
|-------|----------|-------------|
| `circuit_opened` | critical | Provider circuit breaker tripped |
| `circuit_half_open` | warn | Provider entering recovery probe |
| `circuit_closed` | info | Provider recovered |
| `stuck_task_detected` | warn | Task in-progress beyond 45 min |
| `task_timeout_recovery` | info | Stuck task auto-resubmitted |
| `provider_failover` | warn | Provider failed, traffic rerouted |

### Endpoints

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `POST /internal/swarm/self-heal/provider-outcome` | Agent JWT | Report provider success/failure |
| `GET /internal/swarm/self-heal/provider-status` | Agent JWT | Check circuit breaker state |
| `GET /api/v1/swarm/self-heal/summary` | User JWT | Dashboard overview |
| `GET /api/v1/swarm/self-heal/events` | User JWT | Paginated event log |
| `POST /api/v1/swarm/self-heal/events/{id}/resolve` | User JWT | Mark event resolved |
| `POST /api/v1/swarm/self-heal/circuits/{provider}/reset` | User JWT | Manual circuit reset |
| `GET /api/v1/swarm/self-heal/circuits` | User JWT | List all circuit states |

### Prometheus Metrics (Self-Heal)

| Metric | Type | Description |
|--------|------|-------------|
| `rtvortex_swarm_self_heal_events_total` | Counter | Events by type |
| `rtvortex_swarm_self_heal_circuit_transitions_total` | Counter | Circuit state transitions |
| `rtvortex_swarm_self_heal_provider_failures_total` | Counter | Provider failure reports |
| `rtvortex_swarm_self_heal_cycle_seconds` | Histogram | Background loop duration |
| `rtvortex_swarm_self_heal_open_circuits` | Gauge | Currently open circuits |

## Observability Dashboard (Phase 12)

Unified real-time observability across the entire swarm: health scoring,
metrics time-series, per-provider performance analytics, and cost tracking
with budget alerts.

### Architecture

```
 ┌──────────────────────────────────────────────────────────────────────┐
 │                   ObservabilityService (Go)                          │
 │                                                                      │
 │  Background Loop (60s)                                               │
 │  ┌─────────────────────────────────────────────────────────────────┐ │
 │  │ collectMetrics() ─► computeHealthScore() ─► persistSnapshot()   │ │
 │  │        │                     │                      │           │ │
 │  │        ▼                     ▼                      ▼           │ │
 │  │  swarm overview       5-dimension score    swarm_metrics_       │ │
 │  │  + LLM stats          (0-100 composite)    snapshots table      │ │
 │  │  + probe stats                                                  │ │
 │  │                                                                 │ │
 │  │ persistProviderPerf() ─► gcOldData()                            │ │
 │  │        │                      │                                 │ │
 │  │        ▼                      ▼                                 │ │
 │  │  swarm_provider_        DELETE WHERE                            │ │
 │  │  perf_log table         created_at < 90d                        │ │
 │  └─────────────────────────────────────────────────────────────────┘ │
 │                                                                      │
 │  HTTP Handlers                                                       │
 │  ┌──────────────────────────────────────────────────────────────┐    │
 │  │ GET /dashboard  → full dashboard payload (current +          │    │
 │  │                    time-series + providers + health + cost)  │    │
 │  │ GET /time-series → metric snapshots only                     │    │
 │  │ GET /providers   → aggregated provider performance           │    │
 │  │ GET /cost        → cost summary (today/week/month)           │    │
 │  │ GET /health      → latest health score + breakdown           │    │
 │  │ PUT /budget      → set monthly cost budget                   │    │
 │  └──────────────────────────────────────────────────────────────┘    │
 └──────────────────────────────────────────────────────────────────────┘
         │                          │                        │
         ▼                          ▼                        ▼
 ┌───────────────┐   ┌──────────────────────┐   ┌──────────────────┐
 │ Next.js Page  │   │ Python Client Module │   │ Prometheus       │
 │ recharts      │   │ observability.py     │   │ 4 new metrics    │
 │ /dashboard/   │   │ go_client.py         │   │                  │
 │ swarm/        │   │                      │   │                  │
 │ observability │   │                      │   │                  │
 └───────────────┘   └──────────────────────┘   └──────────────────┘
```

### Database Tables (Migration 000025)

| Table | Purpose | Key Columns |
|-------|---------|-------------|
| `swarm_metrics_snapshots` | Periodic system-wide metric snapshots (60s interval) | active/pending/completed/failed tasks, online/busy agents, LLM calls/tokens/latency/error rate, probe/consensus stats, circuit/heal counts, estimated cost, health score |
| `swarm_provider_perf_log` | Per-provider performance tracking | provider, calls, successes, failures, tokens, avg/p95/p99 latency, error rate, cost, consensus wins |
| `swarm_cost_budget` | Monthly cost budget with alert thresholds | scope, month, budget_usd, spent_usd, alert_threshold (UNIQUE on scope+month) |

### Health Score Algorithm

Composite score 0–100 from five equally-weighted dimensions (20 pts each):

| Dimension | Source | Formula |
|-----------|--------|---------|
| Task Success Rate | completed / (completed + failed) | ratio × 20 |
| Agent Availability | online_agents > 0 | online > 0 → 20, else 0 |
| Provider Circuits | open_circuits count | 0 open → 20, ≥3 → 0 |
| Queue Depth | pending_tasks + queue_depth | ≤10 → 20, ≥100 → 0, else linear |
| LLM Error Rate | llm errors / total calls | 0% → 20, ≥20% → 0, else linear |

### Cost Estimation

Token-based cost estimation per provider (per 1K tokens):

| Provider | Cost per 1K tokens |
|----------|--------------------|
| OpenAI | $0.005 |
| Anthropic | $0.008 |
| Google | $0.004 |
| Mistral | $0.006 |
| DeepSeek | $0.002 |
| Cohere | $0.005 |
| Default | $0.005 |

### REST Endpoints

All under `/api/v1/swarm/observability/`:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/dashboard?hours=24` | Full dashboard payload with time-series, providers, health, cost |
| GET | `/time-series?hours=24` | Metric snapshot time-series only |
| GET | `/providers` | Aggregated provider performance (latest snapshot) |
| GET | `/providers/{provider}` | Single provider detail with time-series |
| GET | `/cost` | Cost summary: today, this week, this month, by-provider |
| GET | `/health` | Latest health score with 5-dimension breakdown |
| PUT | `/budget` | Set or update monthly cost budget and alert threshold |

### Prometheus Metrics (Observability)

| Metric | Type | Description |
|--------|------|-------------|
| `rtvortex_swarm_observability_snapshots_total` | Counter | Total metric snapshots collected |
| `rtvortex_swarm_observability_cycle_seconds` | Histogram | Snapshot collection cycle duration |
| `rtvortex_swarm_observability_health_score` | Gauge | Current composite health score (0-100) |
| `rtvortex_swarm_observability_estimated_cost_usd` | Gauge | Current estimated cost in USD |

### Frontend

The observability dashboard is accessible at `/dashboard/swarm/observability`
and provides:

- **Health Score Gauge** — Large circular health indicator (0-100) with color-coded status
- **System Metrics Cards** — Active tasks, online agents, LLM calls, latency
- **Task Activity Chart** — Stacked area chart of task states over time (recharts)
- **LLM Performance Chart** — Dual-axis line chart: calls + latency over time
- **Provider Performance** — Bar chart + detail table comparing providers by calls, error rate, latency, cost, consensus win rate
- **Cost Tracking** — Today/week/month costs, per-provider horizontal bar chart, budget progress bar with alert thresholds
- **Health Breakdown** — Five-dimension progress bars showing contribution of each dimension

Auto-refreshes every 30 seconds with configurable time range (1h / 6h / 24h / 72h).

### Python Client

```python
from swarm.observability import (
    get_observability_dashboard,
    get_health_score,
    get_cost_summary,
    get_provider_perf,
    HealthBreakdown,
    CostSummary,
    ProviderPerfPoint,
)
```

## Observability

### Prometheus Metrics

The Go server exports 25+ metrics at `GET /metrics`:

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
| `rtvortex_swarm_tasks_total` | Counter | Swarm tasks by status |
| `rtvortex_swarm_active_teams` | Gauge | Currently active swarm teams |
| `rtvortex_swarm_active_agents` | Gauge | Currently registered agents |
| `rtvortex_swarm_task_duration_seconds` | Histogram | Task completion time |
| `rtvortex_swarm_elo_distribution` | Histogram | Agent ELO score distribution |
| `rtvortex_mcp_calls_total` | Counter | MCP provider calls by provider/action/status |
| `rtvortex_mcp_call_duration_seconds` | Histogram | MCP call latency |
| `rtvortex_mcp_active_connections` | Gauge | Active MCP connections by provider |

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

### Ingestion Pipeline

Repository indexing uses a **batched streaming architecture** to handle repositories
of any size without exhausting system memory.

```
┌──────────────────────────────────────────────────────────────────────┐
│                   ingestRepository() — Batched Pipeline              │
│                                                                      │
│  Phase 0: listFiles()                                                │
│  ─────────────────                                                   │
│  walkDirectory() → filter by extensions/excludes/size                │
│  Output: vector<string> of file paths (lightweight, ~few MB)         │
│                                                                      │
│  Split into batches of 5,000 files                                   │
│                                                                      │
│  ┌─── For each batch ────────────────────────────────────────────┐   │
│  │                                                               │   │
│  │  Phase 1: parseFiles()                                        │   │
│  │  ─ tree-sitter AST parsing (8 languages)                      │   │
│  │  ─ fallback line-based chunking (~512 tokens/chunk)           │   │
│  │  ─ import/dependency extraction                               │   │
│  │                                                               │   │
│  │  Phase 2: Enrich                                              │   │
│  │  ─ HierarchyBuilder: file summaries + structural prefixes     │   │
│  │  ─ MemoryAccountClassifier: dev/ops/security/history tags     │   │
│  │                                                               │   │
│  │  Phase 3: embedChunks() — Parallel ONNX Workers               │   │
│  │  ─ N workers (auto: cores/4), each with own ONNX session      │   │
│  │  ─ MiniLM-L6-v2 (384-dim), mini-batch of 32 per inference     │   │
│  │  ─ Embedding cache (content-hash → vector) avoids recompute   │   │
│  │                                                               │   │
│  │  Phase 4: Store                                               │   │
│  │  ─ ingestChunksWithEmbeddings() → FAISS IVF index (LTM)       │   │
│  │  ─ KnowledgeGraph::buildFromChunks() → SQLite graph           │   │
│  │                                                               │   │
│  │  Release: clear + shrink_to_fit → return memory to OS         │   │
│  └───────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  Phase 5: save() → persist FAISS index + MTM + cache to disk         │
└──────────────────────────────────────────────────────────────────────┘
```

**Why batched?** A 130 GB repository can produce 200K+ files and 9.5M chunks. Loading all
chunks + embeddings simultaneously would consume ~61 GB of RAM. The batched pipeline keeps
peak working memory under ~3-6 GB regardless of repository size.

**Parallel embedding:** Each ONNX worker runs its own inference session on separate CPU
threads. On a 24-core machine this auto-configures to 6 workers × 4 intra-op threads,
fully saturating all cores during embedding.

| Repository Size | Files | Chunks | Batches | Peak RSS | Est. Time |
|-----------------|-------|--------|---------|----------|-----------|
| Small (<1 GB) | <5K | <100K | 1 | ~1 GB | <5 min |
| Medium (1-10 GB) | 5-50K | 100K-1M | 1-10 | ~2-4 GB | 5-30 min |
| Large (10-50 GB) | 50-200K | 1-5M | 10-40 | ~4-6 GB | 1-4 hrs |
| Massive (50+ GB) | 200K+ | 5-10M+ | 40+ | ~5-8 GB | 4-10 hrs |

### Indexing Modes

The engine supports three indexing actions via the `index_action` proto field:

| Action | Behavior | Use Case |
|--------|----------|----------|
| `index` | Clone + full index (default) | First-time indexing |
| `reindex` | Re-parse existing local clone | Code changed, re-embed |
| `reclone` | Delete clone, re-clone, full index | Branch switch, corrupted clone |

The Go server's Web UI exposes these as buttons with a branch selector dropdown.

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
