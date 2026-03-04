# Setup Guide

Complete guide for local development, CI/CD integration, and distribution.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick Start](#quick-start)
3. [Platform Authentication](#platform-authentication)
4. [CI/CD Integration](#cicd-integration)
5. [Distribution Package](#distribution-package)
6. [TLS Certificates](#tls-certificates)
7. [Troubleshooting](#troubleshooting)

---

## Prerequisites

| Tool | Version | Required |
|------|---------|----------|
| JDK | 21+ | Yes |
| Gradle | 8.5+ | Yes |
| CMake | 3.20+ | Yes |
| Docker | Latest | Optional |
| PostgreSQL | 15+ | For runtime |
| Redis | 7+ | For runtime |

---

## Quick Start

```bash
# Clone
git clone https://github.com/AuralithAI/RT-AI-PR-Reviewer.git
cd RT-AI-PR-Reviewer/mono

# Check prerequisites
./gradlew checkPrereqs

# Build distribution package
./gradlew distZip

# Output: build/distributions/aipr-<version>.zip
```

---

## Platform Authentication

### How It Works

AI PR Reviewer is **platform-agnostic**. Users choose which platform(s) they want to use via configuration. The server supports **all three simultaneously** - users authenticate with whichever they need.

### Scenario 1: User Wants GitHub Only

```yaml
# config/application.yml (or environment variables)
aipr:
  auth:
    github:
      enabled: true
      client-id: ${GITHUB_CLIENT_ID}
      client-secret: ${GITHUB_CLIENT_SECRET}
    gitlab:
      enabled: false
    bitbucket:
      enabled: false
```

Start server, then:
```
http://localhost:8080/api/v1/auth/github/login
```

### Scenario 2: User Wants GitLab Only

```yaml
aipr:
  auth:
    github:
      enabled: false
    gitlab:
      enabled: true
      application-id: ${GITLAB_APPLICATION_ID}
      application-secret: ${GITLAB_APPLICATION_SECRET}
      base-url: https://gitlab.com  # or self-hosted URL
    bitbucket:
      enabled: false
```

Start server, then:
```
http://localhost:8080/api/v1/auth/gitlab/login
```

### Scenario 3: User Wants Bitbucket Only

```yaml
aipr:
  auth:
    github:
      enabled: false
    gitlab:
      enabled: false
    bitbucket:
      enabled: true
      client-id: ${BITBUCKET_CLIENT_ID}
      client-secret: ${BITBUCKET_CLIENT_SECRET}
```

Start server, then:
```
http://localhost:8080/api/v1/auth/bitbucket/login
```

### Scenario 4: Enterprise - All Platforms

```yaml
aipr:
  auth:
    github:
      enabled: true
      client-id: ${GITHUB_CLIENT_ID}
      client-secret: ${GITHUB_CLIENT_SECRET}
    gitlab:
      enabled: true
      application-id: ${GITLAB_APPLICATION_ID}
      application-secret: ${GITLAB_APPLICATION_SECRET}
    bitbucket:
      enabled: true
      client-id: ${BITBUCKET_CLIENT_ID}
      client-secret: ${BITBUCKET_CLIENT_SECRET}
```

Users choose their platform at login:
```
http://localhost:8080/api/v1/auth/github/login
http://localhost:8080/api/v1/auth/gitlab/login
http://localhost:8080/api/v1/auth/bitbucket/login
```

### Self-Hosted / Enterprise Instances

For self-hosted GitLab, GitHub Enterprise, or Bitbucket Server, configure the base URLs:

**GitHub Enterprise:**
```yaml
aipr:
  auth:
    github:
      enabled: true
      base-url: https://github.yourcompany.com
      api-url: https://github.yourcompany.com/api/v3
      client-id: ${GITHUB_CLIENT_ID}
      client-secret: ${GITHUB_CLIENT_SECRET}
```

**Self-Hosted GitLab:**
```yaml
aipr:
  auth:
    gitlab:
      enabled: true
      base-url: https://gitlab.yourcompany.com
      application-id: ${GITLAB_APPLICATION_ID}
      application-secret: ${GITLAB_APPLICATION_SECRET}
```

**Bitbucket Server/Data Center:**
```yaml
aipr:
  auth:
    bitbucket:
      enabled: true
      base-url: https://bitbucket.yourcompany.com
      api-url: https://bitbucket.yourcompany.com/rest/api/1.0
      client-id: ${BITBUCKET_CLIENT_ID}
      client-secret: ${BITBUCKET_CLIENT_SECRET}
```

**Default URLs (Cloud):**

| Platform | Default Base URL | Default API URL |
|----------|------------------|-----------------|
| GitHub | `https://github.com` | `https://api.github.com` |
| GitLab | `https://gitlab.com` | (same as base-url) |
| Bitbucket | `https://bitbucket.org` | `https://api.bitbucket.org/2.0` |

### Creating OAuth Apps

| Platform | Where to Create | Callback URL |
|----------|-----------------|--------------|
| GitHub | Settings > Developer settings > OAuth Apps | `https://your-server/api/v1/auth/github/callback` |
| GitLab | User Settings > Applications | `https://your-server/api/v1/auth/gitlab/callback` |
| Bitbucket | Workspace Settings > OAuth consumers | `https://your-server/api/v1/auth/bitbucket/callback` |

---

## CI/CD Integration

### How CI/CD Integration Works

There are **two ways** to integrate AI PR Reviewer into CI/CD:

#### Option A: Webhook-Based (Automatic)

The server receives webhooks from GitHub/GitLab/Bitbucket and automatically reviews PRs.

```
PR Created --> Webhook --> AI PR Reviewer --> Comments on PR
```

**Setup:**
1. Deploy AI PR Reviewer server
2. Configure webhook in your repository settings
3. PRs are automatically reviewed

#### Option B: CLI/Action-Based (Manual Trigger)

Run AI PR Reviewer as a step in your pipeline.

```
PR Created --> CI Pipeline --> AI PR Reviewer CLI --> Comments on PR
```

### GitHub Actions Integration

**Using the GitHub Action:**

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
          # Option 1: Use hosted service
          api-url: https://api.aipr.auralith.ai
          api-key: ${{ secrets.AIPR_API_KEY }}
          
          # Option 2: Use self-hosted server
          # api-url: https://your-server.com
          # api-key: ${{ secrets.AIPR_API_KEY }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**Using Webhook (auto-review all PRs):**

1. Go to Repository Settings > Webhooks
2. Add webhook:
   - URL: `https://your-server/api/v1/webhooks/github`
   - Content type: `application/json`
   - Secret: Your webhook secret
   - Events: Pull requests

### GitLab CI/CD Integration

**Using GitLab CI:**

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
    GITLAB_TOKEN: $GITLAB_TOKEN
```

**Using Webhook (auto-review all MRs):**

1. Go to Project Settings > Webhooks
2. Add webhook:
   - URL: `https://your-server/api/v1/webhooks/gitlab`
   - Secret token: Your webhook secret
   - Trigger: Merge request events

### Jenkins Integration

**Jenkinsfile:**

```groovy
pipeline {
    agent any
    
    stages {
        stage('AI PR Review') {
            when {
                changeRequest()
            }
            steps {
                withCredentials([
                    string(credentialsId: 'aipr-api-key', variable: 'AIPR_API_KEY'),
                    string(credentialsId: 'github-token', variable: 'GITHUB_TOKEN')
                ]) {
                    sh '''
                        # Using CLI
                        docker run --rm \
                            -e AIPR_API_URL=https://your-server.com \
                            -e AIPR_API_KEY=$AIPR_API_KEY \
                            -e GITHUB_TOKEN=$GITHUB_TOKEN \
                            auralithai/aipr-cli:latest \
                            review --pr ${CHANGE_ID} --repo ${GIT_URL}
                    '''
                }
            }
        }
    }
}
```

### Bitbucket Pipelines Integration

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
          variables:
            AIPR_API_URL: https://your-server.com
            AIPR_API_KEY: $AIPR_API_KEY
            BITBUCKET_TOKEN: $BITBUCKET_TOKEN
```

### Summary: Which Approach?

| Approach | Best For | Pros | Cons |
|----------|----------|------|------|
| **Webhook** | Teams wanting auto-review | Zero config per repo, instant | Requires server deployment |
| **Action/CLI** | Teams wanting control | Per-repo config, flexible | More setup per repo |

---

## Distribution Package

### Package Structure

When you run `./gradlew distZip`, it creates a production-ready distribution:

```
aipr-<version>.zip
|
+-- setup.sh / setup.bat    # Run first to configure environment
+-- run.sh / run.bat        # Start the server
|
+-- bin/                    # Executables
|   +-- aipr-server         # Server startup script (Linux/Mac)
|   +-- aipr-server.bat     # Server startup script (Windows)
|
+-- lib/                    # Libraries
|   +-- aipr-server.jar     # Main server JAR
|   +-- aipr-engine.so      # C++ engine (Linux)
|   +-- aipr-engine.dll     # C++ engine (Windows)
|   +-- aipr-engine.dylib   # C++ engine (macOS)
|
+-- config/                 # Configuration
|   +-- rtserverprops.xml   # Server config (database, redis, grpc, llm)
|   +-- vcsplatforms.xml    # Platform config (GitHub/GitLab/Bitbucket)
|   +-- application.yml     # Spring config
|   +-- logback.xml         # Logging config
|
+-- certificates/           # TLS certificates
|   +-- ca.crt              # CA certificate
|   +-- server.crt          # Server certificate
|   +-- server.key          # Server private key
|   +-- client.crt          # Client certificate (for mTLS)
|   +-- client.key          # Client private key
|
+-- data/                   # Runtime data
|   +-- sql/                # Database migration scripts
|   +-- cache/              # Local cache (created at runtime)
|
+-- temp/                   # Logs + temporary files (RT_TEMP)
|
+-- docs/                   # Documentation
|
+-- README.txt              # Quick start instructions
+-- LICENSE
```

### Building the Distribution

```bash
cd mono

# Build everything and create distribution
./gradlew distZip

# Output location
ls build/distributions/
# aipr-1.0.0.zip

# Or build individual components
./gradlew distTar      # Creates .tar.gz
./gradlew installDist  # Creates unzipped in build/install/
```

### Running from Distribution

```bash
# Extract
unzip aipr-1.0.0.zip
cd aipr-1.0.0

# Run setup (configures RT_HOME, validates environment)
./setup.sh        # Linux/Mac
# or
setup.bat         # Windows

# Edit configuration
nano config/rtserverprops.xml     # Database, Redis, gRPC, LLM settings
nano config/vcsplatforms.xml      # GitHub/GitLab/Bitbucket OAuth

# Start server
./run.sh          # Linux/Mac
# or
run.bat           # Windows
```

### Docker Distribution

```bash
# Build Docker image
./gradlew dockerBuild

# Or pull from registry
docker pull ghcr.io/auralithai/aipr-server:latest

# Run
docker run -d \
    -p 8080:8080 \
    -e DATABASE_URL=jdbc:postgresql://host.docker.internal:5432/aipr \
    -e REDIS_HOST=host.docker.internal \
    -e GITHUB_CLIENT_ID=xxx \
    -e GITHUB_CLIENT_SECRET=xxx \
    ghcr.io/auralithai/aipr-server:latest
```

---

## TLS Certificates

The distribution includes self-signed certificates in `certificates/` for **local development only**.

### Development (Default)

TLS is disabled by default. The included certificates allow you to test TLS locally:

```xml
<!-- config/rtserverprops.xml -->
<engine>
    <tls>
        <enabled>true</enabled>
        <cert-path>certificates/server.crt</cert-path>
        <key-path>certificates/server.key</key-path>
        <ca-path>certificates/ca.crt</ca-path>
    </tls>
</engine>
```

### Production Certificates

**DO NOT use the included certificates in production.** Generate proper certificates:

#### Option 1: Let's Encrypt (Recommended)

```bash
certbot certonly --standalone -d your-domain.com
# Certificates: /etc/letsencrypt/live/your-domain.com/
```

#### Option 2: Self-Signed (Internal Use)

Use the included script to generate new certificates:

```bash
# Linux/Mac
./scripts/generate-certs.sh /path/to/output

# Windows (requires OpenSSL)
scripts\generate-certs.bat C:\path\to\output
```

Or manually:

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt -subj "/CN=AIPR-CA"

# Generate Server Certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr -subj "/CN=your-domain.com"
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt
```

#### Option 3: Corporate CA

Use certificates signed by your organization's Certificate Authority.

### Mutual TLS (mTLS)

For additional security, enable client certificate verification:

```xml
<engine>
    <tls>
        <enabled>true</enabled>
        <cert-path>certificates/server.crt</cert-path>
        <key-path>certificates/server.key</key-path>
        <ca-path>certificates/ca.crt</ca-path>
        <mtls-enabled>true</mtls-enabled>
        <client-cert-path>certificates/client.crt</client-cert-path>
        <client-key-path>certificates/client.key</client-key-path>
    </tls>
</engine>
```

---

## Troubleshooting

### Build Issues

**Gradle version mismatch:**
```bash
./gradlew wrapper --gradle-version 8.5
```

**CMake not found:**
```bash
# Windows: Install CMake and add to PATH
# Linux: sudo apt install cmake
# macOS: brew install cmake
```

### Connection Issues

**PostgreSQL connection refused:**
```bash
docker ps | grep postgres
docker logs aipr-postgres
```

**Redis connection refused:**
```bash
docker ps | grep redis
docker logs aipr-redis
```

### Authentication Issues

**OAuth callback error:**
- Verify callback URL matches exactly (including trailing slash)
- Check client ID/secret are correct
- Ensure redirect URI is registered in platform settings
