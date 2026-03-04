# Setup Guide

Complete guide for local development, CI/CD integration, and distribution.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick Start](#quick-start)
3. [Building](#building)
4. [Configuration](#configuration)
5. [Platform Authentication](#platform-authentication)
6. [CI/CD Integration](#cicd-integration)
7. [Distribution Package](#distribution-package)
8. [TLS Certificates](#tls-certificates)
9. [Docker Deployment](#docker-deployment)
10. [Troubleshooting](#troubleshooting)

---

## Prerequisites

| Tool | Version | Required | Purpose |
|------|---------|----------|---------|
| Go | 1.24+ | Yes | API server |
| CMake | 3.20+ | Yes | C++ engine build |
| g++ | 11+ | Yes | C++ compiler |
| Make | Any | Yes | Unified build controller |
| PostgreSQL | 15+ | Runtime | Database |
| Redis | 7+ | Runtime | Sessions, rate limiting, cache |
| Docker | Latest | Optional | Containerized deployment |

### System Dependencies (Linux / Ubuntu)

```bash
# Go
wget https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# C++ build tools
sudo apt-get update
sudo apt-get install -y \
  build-essential cmake g++ \
  libcurl4-openssl-dev \
  libssl-dev \
  libomp-dev \
  libopenblas-dev \
  liblapack-dev \
  libgflags-dev

# PostgreSQL + Redis
sudo apt-get install -y postgresql redis-server
```

### System Dependencies (macOS)

```bash
brew install go cmake libomp openblas faiss protobuf grpc abseil postgresql redis
```

### FAISS (Linux only — must be installed separately)

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

---

## Quick Start

```bash
# Clone
git clone https://github.com/AuralithAI/RT-AI-PR-Reviewer.git
cd RT-AI-PR-Reviewer

# Build everything (C++ engine + Go server + config + ONNX model)
make

# Edit configuration
nano rt_home/config/rtserverprops.xml   # Database, LLM, engine settings
nano rt_home/config/vcsplatforms.xml    # GitHub/GitLab/Bitbucket/Azure OAuth

# Setup database
make db-install

# Run both components
make run
```

After `make run`:
- Go API server is at `http://localhost:8080`
- C++ engine is at `localhost:50051` (gRPC, internal)
- Prometheus metrics at `http://localhost:8080/metrics`
- OpenAPI spec at `http://localhost:8080/api/v1/docs/openapi.yaml`
- Health check at `http://localhost:8080/health`

---

## Building

### Unified Build (Recommended)

```bash
# Build everything into rt_home/
make

# Build individual components
make engine     # C++ engine only
make server     # Go server only
make config     # Copy config files
make models     # Download ONNX model
```

### Build Output

```
rt_home/
├── bin/
│   ├── rtvortex          ← C++ engine binary (~64 MB)
│   └── RTVortexGo        ← Go server binary (~20 MB)
├── config/
│   ├── rtserverprops.xml  # Server config
│   ├── vcsplatforms.xml   # Platform OAuth config
│   ├── *.yml              # Engine profiles (default, large-repo, etc.)
│   ├── certificates/      # TLS certificates (dev)
│   └── model-providers/   # LLM provider config templates
├── data/sql/
│   └── initData.sql       # PostgreSQL schema
├── models/
│   ├── all-MiniLM-L6-v2.onnx  # Embedding model (~87 MB)
│   ├── tokenizer.json
│   └── vocab.txt
└── temp/                   # Logs + ephemeral scratch (RT_TEMP)
```

### Go Server (Standalone)

```bash
cd mono/server-go

# Build
go build -trimpath -o RTVortexGo ./cmd/rtvortex-server/

# Build with version injection
go build -trimpath \
  -ldflags "-s -w \
    -X main.version=$(git describe --tags --always) \
    -X main.commit=$(git rev-parse --short HEAD) \
    -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o RTVortexGo ./cmd/rtvortex-server/

# Run tests
go test -race -cover ./...

# Run linter
go vet ./...
```

### C++ Engine (Standalone)

```bash
# Configure
cmake -B build -S mono/engine \
  -DCMAKE_BUILD_TYPE=Release \
  -DAIPR_BUILD_SERVER=ON \
  -DAIPR_BUILD_TESTS=ON

# Build
cmake --build build --parallel $(nproc)

# Run tests
ctest --test-dir build --output-on-failure -C Release
```

---

## Configuration

### Configuration Files

| File | Purpose |
|------|---------|
| `config/rtserverprops.xml` | Database, Redis, LLM, engine, server, security settings |
| `config/vcsplatforms.xml` | VCS platform OAuth credentials |
| `config/default.yml` | C++ engine indexing, retrieval, review settings |

### rtserverprops.xml

```xml
<?xml version="1.0" encoding="UTF-8"?>
<rtvortex-config>
    <!-- HTTP Server -->
    <server port="8080"
            read-timeout="30s"
            write-timeout="60s"
            shutdown-timeout="15s"/>

    <!-- Database (PostgreSQL) -->
    <database host="localhost" port="5432"
              name="rtvortex" username="rtvortex" password="your_password"
              max-conns="25" min-conns="5"/>

    <!-- Redis (sessions, rate limiting, cache) -->
    <redis addr="localhost:6379" password="" db="0"/>

    <!-- C++ Engine connection -->
    <engine host="${ENGINE_HOST:localhost}"
            port="${ENGINE_PORT:50051}"
            max-channels="4"
            negotiation-type="${ENGINE_NEGOTIATION:PLAINTEXT}"/>

    <!-- LLM Providers -->
    <llm primary="openai" fallback="ollama">
        <openai api-key="${LLM_OPENAI_API_KEY}"
                base-url="https://api.openai.com/v1"
                model="gpt-4-turbo-preview"/>
        <anthropic api-key="${LLM_ANTHROPIC_API_KEY}"
                   model="claude-3-5-sonnet-20241022"/>
        <ollama base-url="http://localhost:11434"
                model="codellama"/>
    </llm>

    <!-- Storage (pushed to C++ engine via gRPC) -->
    <storage type="local">
        <local path=".aipr/index"/>
        <!-- <s3 bucket="my-bucket" region="us-east-1"/> -->
    </storage>

    <!-- Security -->
    <security encryption-key="${TOKEN_ENCRYPTION_KEY}"/>

    <!-- Logging -->
    <log level="info" format="text"/>
</rtvortex-config>
```

### vcsplatforms.xml

```xml
<?xml version="1.0" encoding="UTF-8"?>
<vcs-platforms>
    <github enabled="true"
            client-id="${GITHUB_CLIENT_ID}"
            client-secret="${GITHUB_CLIENT_SECRET}"
            api-url="https://api.github.com"
            webhook-secret="${GITHUB_WEBHOOK_SECRET}"/>

    <gitlab enabled="false"
            application-id="${GITLAB_APPLICATION_ID}"
            application-secret="${GITLAB_APPLICATION_SECRET}"
            api-url="https://gitlab.com"
            webhook-secret="${GITLAB_WEBHOOK_SECRET}"/>

    <bitbucket enabled="false"
               client-id="${BITBUCKET_CLIENT_ID}"
               client-secret="${BITBUCKET_CLIENT_SECRET}"
               api-url="https://api.bitbucket.org/2.0"
               webhook-secret="${BITBUCKET_WEBHOOK_SECRET}"/>

    <azure-devops enabled="false"
                  client-id="${AZURE_DEVOPS_CLIENT_ID}"
                  tenant-id="${AZURE_DEVOPS_TENANT_ID}"
                  organization="${AZURE_DEVOPS_ORG}"
                  webhook-secret="${AZURE_DEVOPS_WEBHOOK_SECRET}"/>
</vcs-platforms>
```

### Environment Variables

All XML attributes support `${ENV_VAR:default}` syntax. Key variables:

| Variable | Purpose |
|----------|---------|
| `RTVORTEX_HOME` | Root directory (auto-discovered if unset) |
| `ENGINE_HOST` | C++ engine hostname |
| `ENGINE_PORT` | C++ engine port |
| `LLM_OPENAI_API_KEY` | OpenAI API key |
| `LLM_ANTHROPIC_API_KEY` | Anthropic API key |
| `TOKEN_ENCRYPTION_KEY` | 32-byte hex key for AES-256-GCM |
| `GITHUB_CLIENT_ID` | GitHub OAuth app client ID |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth app client secret |
| `GITHUB_WEBHOOK_SECRET` | GitHub webhook HMAC secret |

---

## Platform Authentication

### How It Works

RTVortex is **platform-agnostic**. Enable the platforms you need in `vcsplatforms.xml`.
All platforms can be active simultaneously.

### OAuth2 Flow

```
Browser → GET /api/v1/auth/login/github → Redirect to GitHub OAuth
GitHub  → GET /api/v1/auth/callback/github → Exchange code for token
Server  → Create/update user, generate JWT, set session in Redis
Browser ← JWT access token + refresh token
```

### Creating OAuth Apps

| Platform | Where to Create | Callback URL |
|----------|-----------------|--------------|
| GitHub | Settings > Developer settings > OAuth Apps | `https://your-server/api/v1/auth/callback/github` |
| GitLab | User Settings > Applications | `https://your-server/api/v1/auth/callback/gitlab` |
| Bitbucket | Workspace Settings > OAuth consumers | `https://your-server/api/v1/auth/callback/bitbucket` |
| Azure DevOps | Azure Portal > App Registrations | `https://your-server/api/v1/auth/callback/azure-devops` |
| Google | Google Cloud Console > Credentials | `https://your-server/api/v1/auth/callback/google` |
| Microsoft | Azure Portal > App Registrations | `https://your-server/api/v1/auth/callback/microsoft` |

### Self-Hosted Instances

For self-hosted platforms, configure the base/API URLs:

```xml
<!-- GitHub Enterprise -->
<github enabled="true"
        api-url="https://github.yourcompany.com/api/v3"
        client-id="${GITHUB_CLIENT_ID}"
        client-secret="${GITHUB_CLIENT_SECRET}"/>

<!-- Self-Hosted GitLab -->
<gitlab enabled="true"
        api-url="https://gitlab.yourcompany.com"
        application-id="${GITLAB_APPLICATION_ID}"
        application-secret="${GITLAB_APPLICATION_SECRET}"/>

<!-- Bitbucket Server -->
<bitbucket enabled="true"
           api-url="https://bitbucket.yourcompany.com/rest/api/1.0"
           client-id="${BITBUCKET_CLIENT_ID}"
           client-secret="${BITBUCKET_CLIENT_SECRET}"/>
```

---

## CI/CD Integration

### How CI/CD Integration Works

There are **two ways** to integrate RTVortex into CI/CD:

#### Option A: Webhook-Based (Automatic)

```
PR Created → Webhook → RTVortex Server → Reviews PR → Comments on PR
```

#### Option B: CLI/Action-Based (Manual Trigger)

```
PR Created → CI Pipeline → RTVortex CLI → Reviews PR → Comments on PR
```

### GitHub Actions

```yaml
# .github/workflows/pr-review.yml
name: AI PR Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: AI PR Review
        uses: AuralithAI/ai-pr-reviewer-action@v1
        with:
          api-url: https://your-server.com
          api-key: ${{ secrets.AIPR_API_KEY }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### GitLab CI

```yaml
# .gitlab-ci.yml
ai-pr-review:
  stage: test
  image: auralithai/aipr-cli:latest
  script:
    - aipr review --gitlab-mr $CI_MERGE_REQUEST_IID
  only:
    - merge_requests
  variables:
    AIPR_API_URL: https://your-server.com
    AIPR_API_KEY: $AIPR_API_KEY
```

### Jenkins

```groovy
pipeline {
    agent any
    stages {
        stage('AI PR Review') {
            when { changeRequest() }
            steps {
                withCredentials([
                    string(credentialsId: 'aipr-api-key', variable: 'AIPR_API_KEY')
                ]) {
                    sh '''
                        docker run --rm \
                            -e AIPR_API_URL=https://your-server.com \
                            -e AIPR_API_KEY=$AIPR_API_KEY \
                            auralithai/aipr-cli:latest \
                            review --pr ${CHANGE_ID}
                    '''
                }
            }
        }
    }
}
```

### Bitbucket Pipelines

```yaml
# bitbucket-pipelines.yml
pipelines:
  pull-requests:
    '**':
      - step:
          name: AI PR Review
          image: auralithai/aipr-cli:latest
          script:
            - aipr review --bitbucket-pr $BITBUCKET_PR_ID
```

### Azure Pipelines

```yaml
# azure-pipelines.yml
trigger: none
pr:
  branches:
    include: ['*']

pool:
  vmImage: 'ubuntu-latest'

steps:
  - script: |
      docker run --rm \
        -e AIPR_API_URL=https://your-server.com \
        -e AIPR_API_KEY=$(AIPR_API_KEY) \
        auralithai/aipr-cli:latest \
        review --azure-pr $(System.PullRequest.PullRequestId)
    displayName: 'AI PR Review'
```

### Summary: Which Approach?

| Approach | Best For | Pros | Cons |
|----------|----------|------|------|
| **Webhook** | Teams wanting auto-review | Zero config per repo, instant | Requires server deployment |
| **Action/CLI** | Teams wanting control | Per-repo config, flexible | More setup per repo |

---

## Distribution Package

### Building a Distribution

```bash
# Build everything
make

# The rt_home/ directory IS the distribution:
tar -czf rtvortex-$(git describe --tags).tar.gz rt_home/
```

### Package Structure

```
rt_home/
├── bin/
│   ├── rtvortex          # C++ engine (static binary)
│   └── RTVortexGo        # Go server (static binary)
├── config/
│   ├── rtserverprops.xml # Server config
│   ├── vcsplatforms.xml  # Platform OAuth config
│   ├── default.yml       # Engine defaults
│   ├── certificates/     # TLS certs (dev only)
│   └── model-providers/  # LLM config templates
├── data/sql/
│   └── initData.sql      # PostgreSQL schema
├── models/
│   └── all-MiniLM-L6-v2.onnx  # Embedding model
└── temp/                  # Logs + scratch (created at runtime)
```

### Running from Distribution

```bash
# Extract
tar -xzf rtvortex-v1.0.0.tar.gz
cd rt_home

# Configure
nano config/rtserverprops.xml
nano config/vcsplatforms.xml

# Setup database
psql -U postgres -c "CREATE USER rtvortex WITH PASSWORD 'your_password';"
psql -U postgres -c "CREATE DATABASE rtvortex OWNER rtvortex;"
psql -U rtvortex -d rtvortex -f data/sql/initData.sql

# Start engine (background)
RTVORTEX_HOME=$(pwd) ./bin/rtvortex --server &

# Start server (foreground)
RTVORTEX_HOME=$(pwd) ./bin/RTVortexGo
```

---

## Docker Deployment

### Dockerfile (Go Server)

The Go server includes a multi-stage Dockerfile at `mono/server-go/Dockerfile`:

```bash
# Build image
cd mono/server-go
docker build -t rtvortex-server .

# Run
docker run -d \
    -p 8080:8080 \
    -e DATABASE_HOST=host.docker.internal \
    -e DATABASE_PORT=5432 \
    -e DATABASE_NAME=rtvortex \
    -e DATABASE_USERNAME=rtvortex \
    -e DATABASE_PASSWORD=secret \
    -e REDIS_ADDR=host.docker.internal:6379 \
    -e ENGINE_HOST=host.docker.internal \
    -e ENGINE_PORT=50051 \
    -e GITHUB_CLIENT_ID=xxx \
    -e GITHUB_CLIENT_SECRET=xxx \
    -e LLM_OPENAI_API_KEY=xxx \
    rtvortex-server
```

### Docker Compose

```yaml
version: '3.8'

services:
  engine:
    build: ./mono/engine
    ports:
      - "50051:50051"
    volumes:
      - ./rt_home/config:/app/config
      - ./rt_home/models:/app/models
      - engine-data:/app/data
    environment:
      RTVORTEX_HOME: /app

  server:
    build: ./mono/server-go
    ports:
      - "8080:8080"
    depends_on:
      - engine
      - postgres
      - redis
    volumes:
      - ./rt_home/config:/app/config
    environment:
      RTVORTEX_HOME: /app
      ENGINE_HOST: engine
      ENGINE_PORT: 50051
      DATABASE_HOST: postgres
      REDIS_ADDR: redis:6379

  postgres:
    image: postgres:15
    environment:
      POSTGRES_USER: rtvortex
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: rtvortex
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./rt_home/data/sql/initData.sql:/docker-entrypoint-initdb.d/init.sql

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes

volumes:
  engine-data:
  pgdata:
```

---

## TLS Certificates

The distribution includes self-signed certificates in `config/certificates/` for **development only**.

### Development (Default)

TLS is disabled by default for gRPC. Enable it with:

```xml
<!-- config/rtserverprops.xml -->
<engine host="localhost" port="50051"
        negotiation-type="TLS"
        tls-cert="config/certificates/server.crt"
        tls-key="config/certificates/server.key"
        tls-ca="config/certificates/ca.crt"/>
```

### Production Certificates

**DO NOT use the included certificates in production.**

#### Option 1: Let's Encrypt

```bash
certbot certonly --standalone -d your-domain.com
```

#### Option 2: Self-Signed

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt -subj "/CN=RTVortex-CA"

# Generate Server Certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr -subj "/CN=your-domain.com"
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt
```

---

## Database Setup

### Create Database

```bash
# As PostgreSQL superuser
psql -U postgres <<SQL
CREATE USER rtvortex WITH PASSWORD 'your_password';
CREATE DATABASE rtvortex OWNER rtvortex;
GRANT ALL PRIVILEGES ON DATABASE rtvortex TO rtvortex;
SQL
```

### Initialize Schema

```bash
# Apply schema (11 tables)
psql -U rtvortex -d rtvortex -f rt_home/data/sql/initData.sql
```

Or use the Makefile:

```bash
make db-create    # Create role + database
make db-init      # Apply schema
make db-install   # Both (after building)
```

### Schema Tables

| Table | Purpose |
|-------|---------|
| `users` | User accounts |
| `oauth_identities` | OAuth credentials (encrypted) |
| `organizations` | Multi-tenant orgs |
| `org_members` | Membership + roles |
| `repositories` | Registered repos |
| `reviews` | Review history |
| `review_comments` | Review line comments |
| `usage_daily` | Usage statistics |
| `webhook_events` | Webhook audit trail |
| `audit_log` | Security events |
| `schema_info` | Schema version |

---

## Troubleshooting

### Build Issues

**Go not found:**
```bash
export PATH=$PATH:/usr/local/go/bin
go version  # should show 1.24+
```

**CMake not found:**
```bash
sudo apt install cmake    # Linux
brew install cmake        # macOS
```

**FAISS linking errors:**
```bash
sudo ldconfig  # Refresh shared library cache
```

### Connection Issues

**PostgreSQL connection refused:**
```bash
# Check if running
pg_isready -h localhost -p 5432

# Check auth
psql -U rtvortex -d rtvortex -c "SELECT 1;"
```

**Redis connection refused:**
```bash
redis-cli ping  # Should return PONG
```

**Engine gRPC connection failed:**
```bash
# Check if engine is running
curl -s http://localhost:8080/ready | python3 -m json.tool

# Check engine directly (requires grpcurl)
grpcurl -plaintext localhost:50051 list
```

### Runtime Issues

**"failed to resolve RTVORTEX_HOME":**
```bash
# Set explicitly
export RTVORTEX_HOME=/path/to/rt_home
```

**"no JWT secret configured":**

Add a JWT secret to `rtserverprops.xml` or set `JWT_SECRET` env var. Without it, a random secret is generated (sessions won't survive restarts).

**"Token encryptor init failed":**

The `encryption-key` in `rtserverprops.xml` must be a 32-byte hex string (64 hex chars). Generate one:
```bash
openssl rand -hex 32
```

### Checking Health

```bash
# Quick health check
curl http://localhost:8080/health

# Detailed readiness (checks DB + Redis + Engine)
curl http://localhost:8080/ready

# Version info
curl http://localhost:8080/version

# Prometheus metrics
curl http://localhost:8080/metrics | head -50
```
