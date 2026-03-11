# RTVortex — Go Server Architecture

## Overview

The Go API server (`RTVortexGo`) is the primary external-facing component of RTVortex.
It replaces the original Java/Spring Boot server with a leaner, statically-compiled Go binary.

**Key stats:**
- **Language**: Go 1.24
- **Source files**: 54 `.go` files
- **Lines of code**: ~14,400 lines
- **Binary size**: ~20 MB (statically compiled, stripped)
- **Startup time**: <1 second
- **Dependencies**: 15 direct, no CGo

```
┌─────────────────────────────────────────────────────────────────────┐
│                     RTVortexGo Architecture                         │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  cmd/rtvortex-server/main.go                                 │   │
│  │  Entry point • DI wiring • CLI flags • Graceful shutdown     │   │
│  └────────────────────────────┬─────────────────────────────────┘   │
│                               │                                     │
│  ┌────────────────────────────▼─────────────────────────────────┐   │
│  │  internal/server/server.go                                   │   │
│  │  Chi router • Global middleware • Route groups               │   │
│  └──────┬──────────────┬──────────────┬─────────────────────────┘   │
│         │              │              │                             │
│  ┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐                      │
│  │  api/       │ │  ws/       │ │  metrics/  │                      │
│  │  handlers   │ │  websocket │ │  prom      │                      │
│  │  (32+ eps)  │ │  hub       │ │  (16)      │                      │
│  └──────┬──────┘ └────────────┘ └────────────┘                      │
│         │                                                           │
│  ┌──────▼───────────────────────────────────────────────────────┐   │
│  │                    Service Layer                             │   │
│  │  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐      │   │
│  │  │ review │ │  llm   │ │  vcs   │ │  auth  │ │ engine │      │   │
│  │  │pipeline│ │registry│ │registry│ │ jwt+   │ │ client │      │   │
│  │  │(12step)│ │(3 prov)│ │(4 plat)│ │ oauth2 │ │ (grpc) │      │   │
│  │  └────────┘ └────────┘ └────────┘ └────────┘ └────────┘      │   │
│  └──────┬──────────┬──────────┬──────────┬──────────┬───────────┘   │
│         │          │          │          │          │               │
│  ┌──────▼──────────▼──────────▼──────────▼──────────▼──────────┐    │
│  │                    Data Layer                               │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐   │    │
│  │  │  store/  │  │ session/ │  │  audit/  │  │  crypto/   │   │    │
│  │  │ pgx pool │  │  redis   │  │  logger  │  │  aes-gcm   │   │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └────────────┘   │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Infrastructure                                             │    │
│  │  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐             │    │
│  │  │ config │  │  rtenv │  │  rtlog │  │ backgnd│             │    │
│  │  │  XML   │  │  HOME  │  │ dual-  │  │ sched  │             │    │
│  │  │ parser │  │ resolve│  │ output │  │ (cron) │             │    │
│  │  └────────┘  └────────┘  └────────┘  └────────┘             │    │
│  └─────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

## Dependency Injection

RTVortexGo uses **manual dependency injection** — no DI framework, no reflection, no magic.
All wiring happens in `main.go` (~460 lines):

```go
// Wiring order in main.go:
1. rtenv.Resolve()           // Find RTVORTEX_HOME
2. rtlog.Setup(env)          // File + stdout logging
3. config.Load(opts)         // Parse XML configuration
4. store.NewPostgresPool()   // PostgreSQL connection
5. session.NewRedisClient()  // Redis connection
6. engine.NewPool()          // gRPC connection pool to C++ engine
7. store.New*Repository()    // Database repositories (5)
8. engine.NewClient()        // gRPC client wrapper
9. auth.NewJWTManager()      // JWT token manager
10. session.NewManager()     // Redis session manager
11. auth.NewProviderRegistry() // OAuth2 providers (6)
12. rtcrypto.NewTokenEncryptor() // AES-256-GCM
13. llm.NewRegistry()        // LLM providers (OpenAI, Anthropic, Ollama)
14. vcs.NewPlatformRegistry() // VCS clients (GitHub, GitLab, Bitbucket, Azure)
15. review.NewPipeline()     // 12-step review pipeline
16. indexing.NewService()    // Indexing service
17. ws.NewHub()              // WebSocket hub
18. session.NewRateLimiter() // Rate limiter (3 categories)
19. audit.NewLogger()        // Audit logger
20. background.NewScheduler() // Background job scheduler
21. server.New(deps)         // Build router with all dependencies
22. http.Server.ListenAndServe() // Start HTTP server
```

All dependencies flow down through the `server.Dependencies` struct:

```go
type Dependencies struct {
    Config          *config.Config
    DB              *store.DB
    Redis           *session.RedisClient
    EnginePool      *engine.Pool
    Version         string

    EngineClient    *engine.Client
    JWTMgr          *auth.JWTManager
    SessionMgr      *session.Manager
    OAuthReg        *auth.ProviderRegistry
    TokenEncryptor  *rtcrypto.TokenEncryptor
    LLMRegistry     *llm.Registry
    VCSRegistry     *vcs.PlatformRegistry
    ReviewPipeline  *review.Pipeline
    IndexingService *indexing.Service
    RateLimiter     *session.RateLimiter
    AuditLogger     *audit.Logger
    WSHub           *ws.Hub

    UserRepo        *store.UserRepository
    RepoRepo        *store.RepositoryRepo
    ReviewRepo      *store.ReviewRepository
    OrgRepo         *store.OrgRepository
    WebhookRepo     *store.WebhookRepository
}
```

## Middleware Stack

Middleware is applied in order via `chi`:

```
Request → RequestID → RealIP → Logger → Recoverer → Compress
       → Timeout(60s) → Prometheus Metrics → CORS → [Route Group Middleware]
```

| Middleware | Package | Purpose |
|-----------|---------|---------|
| `RequestID` | chi | Unique request ID header |
| `RealIP` | chi | Extract real IP from X-Forwarded-For |
| `Logger` | chi | Access logging |
| `Recoverer` | chi | Panic recovery → 500 |
| `Compress(5)` | chi | gzip response compression |
| `Timeout(60s)` | chi | Request timeout |
| `rtmetrics.Middleware` | internal | Prometheus HTTP metrics |
| `cors.Handler` | go-chi/cors | CORS headers |
| `auth.Middleware` | internal | JWT verification (protected routes) |
| `auth.RequireRole` | internal | Role-based access (admin routes) |
| `session.RateLimitMiddleware` | internal | Per-category rate limiting |

Route groups with additional middleware:

```
/api/v1/auth/*          → RateLimitMiddleware("auth", 20/min)
/api/v1/* (protected)   → auth.Middleware + RateLimitMiddleware("api", 100/min)
/api/v1/webhooks/*      → RateLimitMiddleware("webhook", 60/min)
```

## Review Pipeline

The review pipeline (`internal/review/pipeline.go`) is a 12-step process with WebSocket progress events at each step:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Review Pipeline (12 Steps)                   │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐     │
│  │ 1. Valid │→ │ 2. Fetch │→ │ 3. Parse │→ │ 4. Skip      │     │
│  │   -ate   │  │   Diff   │  │   Diff   │  │   Patterns   │     │
│  └──────────┘  └──────────┘  └──────────┘  └──────────────┘     │
│        │              │              │              │           │
│  ┌─────▼────┐  ┌──────▼─────┐  ┌────▼──────┐  ┌───▼────────┐    │
│  │ 5. Chunk │→ │ 6. Index   │→ │ 7. Build  │→ │ 8. Prompt  │    │
│  │   Files  │  │   (Engine) │  │   Context │  │   Construct│    │
│  └──────────┘  └────────────┘  └───────────┘  └────────────┘    │
│        │              │              │              │           │
│  ┌─────▼────┐  ┌──────▼─────┐  ┌─────▼─────┐  ┌─────▼──────┐    │
│  │ 9. LLM   │→ │ 10. Parse  │→ │ 11. Post  │→ │ 12. Record │    │
│  │   Call   │  │   Response │  │   Comment │  │   Review   │    │
│  └──────────┘  └────────────┘  └───────────┘  └────────────┘    │
│                                                                 │
│  Each step emits → WebSocket ProgressEvent + Prometheus timer   │
└─────────────────────────────────────────────────────────────────┘
```

### Step Details

| Step | Name | Component | Description |
|------|------|-----------|-------------|
| 1 | Validate | Pipeline | Check repo exists, permissions, rate limits |
| 2 | Fetch Diff | VCS Client | Fetch PR diff from platform API |
| 3 | Parse Diff | Pipeline | Parse unified diff into file-level hunks |
| 4 | Skip Patterns | Pipeline | Apply glob skip patterns (node_modules, etc.) |
| 5 | Chunk Files | Pipeline | Split files into review-sized chunks |
| 6 | Index | Engine (gRPC) | Ensure repo is indexed in C++ engine |
| 7 | Build Context | Engine (gRPC) | Get relevant code context for changed files |
| 8 | Prompt | Pipeline | Construct LLM prompt from context + diff |
| 9 | LLM Call | LLM Provider | Send prompt to OpenAI/Anthropic/Ollama |
| 10 | Parse Response | Pipeline | Extract structured comments from LLM output |
| 11 | Post Comments | VCS Client | Post review comments back to PR |
| 12 | Record | Store | Save review to PostgreSQL |

Steps 6 and 7 are gRPC calls to the C++ engine. Step 9 is an HTTP call to the LLM provider. All other steps execute in the Go server.

## LLM Provider System

```
┌────────────────────────────────────────────────────────────────┐
│  llm.Registry                                                  │
│                                                                │
│  ┌──────────────┐  ┌──────────────────┐  ┌──────────────┐      │
│  │ OpenAI       │  │ Anthropic        │  │ Ollama       │      │
│  │              │  │                  │  │              │      │
│  │ Complete()   │  │ Complete()       │  │ Complete()   │      │
│  │ SSE Stream   │  │ SSE Stream       │  │ NDJSON Stream│      │
│  │              │  │ (content_block_  │  │              │      │
│  │ data: {json} │  │  delta events)   │  │ {"response":}│      │
│  └──────────────┘  └──────────────────┘  └──────────────┘      │
│                                                                │
│  Interface:                                                    │
│    Provider: Name(), Complete(ctx, prompt, model) → string     │
│    StreamingProvider: StreamComplete(ctx, prompt, model, ch)   │
│                                                                │
│  Fallback: primary → fallback on error                         │
│  Registry: StreamComplete() checks StreamingProvider interface │
└────────────────────────────────────────────────────────────────┘
```

### Streaming Protocols

| Provider | Protocol | Format | Event Field |
|----------|----------|--------|-------------|
| OpenAI | SSE | `data: {"choices":[{"delta":{"content":"..."}}]}` | `content` |
| Anthropic | SSE | `event: content_block_delta` + `data: {"delta":{"text":"..."}}` | `text` |
| Ollama | NDJSON | `{"response":"...", "done":false}` | `response` |

The SSE endpoint at `POST /api/v1/llm/stream` normalizes all formats into:
```
data: {"chunk":"text fragment","done":false}
data: {"chunk":"","done":true,"model":"gpt-4o","provider":"openai"}
```

## VCS Platform Abstraction

```
┌────────────────────────────────────────────────────────────────┐
│  vcs.PlatformRegistry                                          │
│                                                                │
│  Interface: Platform                                           │
│    Name() string                                               │
│    GetPullRequest(ctx, owner, repo, prNum) (*PR, error)        │
│    GetDiff(ctx, owner, repo, prNum) (string, error)            │
│    ListFiles(ctx, owner, repo, prNum) ([]File, error)          │
│    PostComment(ctx, owner, repo, prNum, comment) error         │
│    VerifyWebhook(req) ([]byte, error)                          │
│                                                                │
│  ┌────────────┐ ┌────────────┐ ┌─────────────┐ ┌─────────────┐ │
│  │  GitHub    │ │  GitLab    │ │  Bitbucket  │ │  Azure      │ │
│  │  REST v3   │ │  REST v4   │ │  REST 2.0   │ │  DevOps     │ │
│  │  ~280 loc  │ │  ~270 loc  │ │  ~310 loc   │ │  ~340 loc   │ │
│  └────────────┘ └────────────┘ └─────────────┘ └─────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

Each platform client implements:
- PR metadata fetching
- Diff retrieval
- File listing
- Comment posting (inline + top-level)
- Webhook signature verification (HMAC)

## WebSocket System

```
┌────────────────────┐     subscribe(reviewID)    ┌──────────┐
│  Browser / Client  │ ◀──────────────────────── │  ws.Hub  │
│  (WebSocket conn)  │                            │          │
│                    │     ProgressEvent          │ Rooms    │
│  GET /reviews/     │ ◀──────────────────────── │  map[    │
│  {id}/ws           │     {step, status, msg}    │  uuid →  │
└────────────────────┘                            │  []conn] │
                                                  │          │
                        ┌─────────────────────────│──────────│
                        │  review.Pipeline        │          │
                        │  SetProgressFunc(cb)   ─▶ Broadcast│
                        └─────────────────────────└──────────┘
```

- Clients connect via `GET /api/v1/reviews/{reviewID}/ws`
- Hub maintains rooms keyed by review UUID
- Pipeline emits progress events at each of the 12 steps
- Events include: step name, index, total, status, message, metadata
- Connections are auto-cleaned on disconnect

## Repository Management

The Go server includes a web UI for managing repository indexing.

### Indexing Modes

| Action | Proto Field (`index_action`) | Behavior |
|--------|------------------------------|----------|
| **Index** | `INDEX` | Clone repo (if needed) and build full index |
| **Reindex** | `REINDEX` | Re-embed existing local clone without re-cloning |
| **Reclone** | `RECLONE` | Delete local clone, fresh `git clone`, and reindex |

### Branch Listing

`GET /api/v1/repos/{id}/branches` runs `git ls-remote` against the repo's clone URL
and returns all remote branch names. The Web UI renders these in a dropdown so users
can select which branch to index.

### Metrics Dashboard

The Web UI streams real-time engine metrics via Server-Sent Events:

```
Browser                  Go Server              C++ Engine
  │   GET /metrics/sse     │   StreamMetrics()    │
  │ ◀────────────────────  │ ◀─────────────────── │
  │   event: metrics       │   (1s poll, gRPC)    │
  │   data: {json}         │                      │
  │                        │                      │
```

Displayed metrics include: FAISS index status, MiniLM model readiness,
embedding backend type, confidence gate scores, and LLM avoidance rate.

## Token Encryption

OAuth tokens (access + refresh) are encrypted at rest using AES-256-GCM:

```
┌──────────────┐    Encrypt     ┌──────────────────┐    Store    ┌────────────┐
│ OAuth Token  │ ─────────────▶ │ AES-256-GCM      │ ──────────▶│ PostgreSQL │
│ (plaintext)  │                │ 12-byte nonce     │            │ oauth_     │
└──────────────┘                │ 32-byte key       │            │ identities │
                                │ (from config)     │            └────────────┘
                                └──────────────────┘

┌────────────┐    Fetch    ┌──────────────────┐    Decrypt    ┌──────────────┐
│ PostgreSQL │ ──────────▶│ AES-256-GCM      │ ─────────────▶│ OAuth Token  │
│ (encrypted)│            │ nonce||ciphertext │              │ (plaintext)  │
└────────────┘            └──────────────────┘              └──────────────┘
```

- Key derived from `encryption-key` in `rtserverprops.xml` security config
- If no key is configured, falls back to no-op (tokens stored unencrypted, warning logged)
- Nonce is randomly generated per encryption and prepended to ciphertext

## Rate Limiting

Redis-backed sliding window rate limiter with per-category configuration:

| Category | Limit | Scope |
|----------|-------|-------|
| `api` | 100 req/min | All authenticated API endpoints |
| `auth` | 20 req/min | OAuth login/callback/refresh |
| `webhook` | 60 req/min | VCS webhook endpoints |

```
┌─────────┐      ┌───────────────────┐      ┌───────┐
│ Request │────▶│ RateLimitMiddleware│────▶│ Redis │
│         │      │ (category, key)   │      │ INCR  │
│         │      │                   │      │ EXPIRE│
│         │      │ 429 Too Many ◀────│─────│       │
│         │      │ Requests          │      │       │
└─────────┘      └───────────────────┘      └───────┘
                         │
                         ▼ Prometheus counter
              rtvortex_rate_limit_rejections_total
```

## Audit Logging

Security-relevant events are logged asynchronously to PostgreSQL:

```go
// Usage in handlers:
h.AuditLogger.Log(ctx, audit.Event{
    Action:   "user.login",
    UserID:   user.ID,
    Resource: "session",
    Detail:   "OAuth login via GitHub",
    IP:       r.RemoteAddr,
})
```

Events are sent to a buffered channel and written in a background goroutine to avoid blocking request handling.

## Background Scheduler

The scheduler runs periodic maintenance tasks:

| Task | Interval | Purpose |
|------|----------|---------|
| Session cleanup | Every 15 min | Evict expired sessions from Redis |
| LLM health check | Every 60s | Ping LLM providers, update health status |
| Index job cleanup | Every hour | Remove completed index jobs older than 7 days |

## Graceful Shutdown

```
SIGINT/SIGTERM received
         │
         ▼
1. Stop accepting new HTTP connections
2. Wait for in-flight requests (configurable timeout)
3. Cancel root context
4. Stop background scheduler
5. Stop WebSocket hub
6. Close engine gRPC pool
7. Close Redis connection
8. Close PostgreSQL pool
9. Flush log files
```

## Configuration Loading

The Go server reads two XML files at startup:

```
rtserverprops.xml                    vcsplatforms.xml
─────────────────                    ────────────────
<server port="8080"/>                <github enabled="true"
<database host="localhost"                   client-id="..."
          port="5432" .../>                  client-secret="..."/>
<redis addr="localhost:6379"/>       <gitlab enabled="true" .../>
<engine host="localhost"             <bitbucket enabled="true" .../>
        port="50051"/>               <azure-devops enabled="true" .../>
<llm primary="openai">
  <openai api-key="..." .../>
  <anthropic api-key="..." .../>
</llm>
<security encryption-key="..."/>
```

Config auto-discovery:
1. CLI flags (`--config`, `--vcs-config`)
2. `$RTVORTEX_HOME/config/rtserverprops.xml`
3. `./config/rtserverprops.xml`
4. `../config/rtserverprops.xml`

## Build & Run

```bash
# Build (standalone)
cd mono/server-go
go build -trimpath -o RTVortexGo ./cmd/rtvortex-server/

# Build with version injection
go build -trimpath \
  -ldflags "-s -w -X main.version=v1.0.0 -X main.commit=abc1234 -X main.buildDate=2026-03-04" \
  -o RTVortexGo ./cmd/rtvortex-server/

# Build via root Makefile (into rt_home/bin/)
make server

# Run
RTVORTEX_HOME=/path/to/rt_home ./RTVortexGo

# Run with custom config
./RTVortexGo --config /etc/rtvortex/rtserverprops.xml --vcs-config /etc/rtvortex/vcsplatforms.xml

# Tests
go test -race -cover ./...

# Vet
go vet ./...
```

## Why Go over Java

| Aspect | Java/Spring Boot | Go |
|--------|------------------|----|
| Binary size | ~200 MB (JRE + JARs) | ~20 MB (static binary) |
| Startup time | 3-8 seconds | <1 second |
| Memory (idle) | 200-500 MB (JVM heap) | 15-30 MB |
| Deployment | JRE required | Single binary, zero deps |
| Docker image | 400+ MB | 30 MB (scratch/alpine) |
| Concurrency | Thread pool + virtual threads | Goroutines (M:N scheduler) |
| Build time | 30-60s (Gradle) | 5-10s |
| Cross-compile | Complex (GraalVM native) | `GOOS=linux GOARCH=arm64 go build` |
| DI framework | Spring IoC (reflection) | Manual wiring (explicit) |
| XML config | JAXB annotations | encoding/xml (no annotations) |
