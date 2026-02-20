# AI-PR-Reviewer

<div align="center">

**Production-Grade AI-Powered Code Review System**

[![Java 21+](https://img.shields.io/badge/Java-21+-orange.svg)](https://openjdk.org/)
[![Gradle 8.5+](https://img.shields.io/badge/Gradle-8.5+-02303A.svg)](https://gradle.org/)
[![C++17](https://img.shields.io/badge/C++-17-00599C.svg)](https://isocpp.org/)
[![Spring Boot](https://img.shields.io/badge/Spring%20Boot-3.2+-6DB33F.svg)](https://spring.io/projects/spring-boot)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)
[![CI](https://github.com/AuralithAI/RT-AI-PR-Reviewer/actions/workflows/ci.yml/badge.svg)](https://github.com/AuralithAI/RT-AI-PR-Reviewer/actions/workflows/ci.yml)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED.svg)](https://hub.docker.com/)
[![gRPC](https://img.shields.io/badge/gRPC-TLS-4285F4.svg)](https://grpc.io/)

A platform-neutral PR review engine with connectors for GitHub, GitLab, Bitbucket, and Jenkins.

[Setup Guide](docs/setup.md) | [Architecture](docs/architecture.md)

</div>

---

## Features

- **Platform Agnostic**: Works with GitHub, GitLab, and Bitbucket (cloud & self-hosted)
- **CI/CD Integration**: GitHub Actions, GitLab CI, Jenkins, Bitbucket Pipelines
- **High Performance**: C++ engine for code indexing and analysis via gRPC
- **LLM Powered**: Supports OpenAI, Anthropic, and compatible APIs
- **Self-Hostable**: Deploy on your infrastructure with full control
- **TLS/mTLS Support**: Secure gRPC communication with included dev certificates

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| JDK | 21+ | Server runtime |
| Gradle | 8.5+ | Build system |
| CMake | 3.20+ | C++ engine build |
| PostgreSQL | 15+ | Database (runtime) |
| Redis | 7+ | Cache (runtime) |

## Quick Start

```bash
# Clone
git clone https://github.com/AuralithAI/RT-AI-PR-Reviewer.git
cd RT-AI-PR-Reviewer/mono

# Check prerequisites
./gradlew checkPrereqs

# Build distribution
./gradlew distZip

# Extract and run
unzip build/distributions/aipr-*.zip -d /opt/
cd /opt/aipr-*
./setup.sh                    # Configure environment
nano config/rtserverprops.xml # Edit configuration
./run.sh                      # Start server
```

## Repository Structure

```
RT-AI-PR-Reviewer/
├── mono/                       # Monorepo source code
│   ├── engine/                 # C++ indexing/retrieval engine
│   ├── server/                 # Java Spring Boot API server
│   ├── cli/                    # Command-line interface
│   ├── sdks/                   # Client SDKs (Java, Python, Node)
│   ├── integrations/           # Platform integrations
│   ├── config/                 # Configuration files
│   │   ├── rtserverprops.xml   # Server configuration
│   │   ├── vcsplatforms.xml    # Platform OAuth config
│   │   └── certificates/       # TLS certificates (dev)
│   ├── proto/                  # gRPC protocol definitions
│   ├── build.gradle            # Gradle build
│   └── settings.gradle         # Gradle settings
│
├── docs/                       # Documentation
│   ├── setup.md                # Setup, CI/CD, distribution
│   └── architecture.md         # System architecture
│
├── .github/workflows/          # CI/CD workflows
├── LICENSE                     # Apache 2.0
└── README.md                   # This file
```

## Distribution Package

Run `./gradlew distZip` to create a production-ready package:

```
aipr-<version>.zip
├── setup.sh / setup.bat        # Environment setup (run first)
├── run.sh / run.bat            # Start server
├── bin/
│   └── aipr-server             # Direct startup script
├── lib/
│   ├── aipr-server.jar         # Server (thin JAR)
│   ├── *.jar                   # Runtime dependencies
│   └── aipr-engine.so/dll      # C++ engine native library
├── config/
│   ├── rtserverprops.xml       # Server config (database, redis, grpc, llm)
│   └── vcsplatforms.xml        # Platform OAuth config
├── certificates/               # TLS certs (dev only, regenerate for prod)
├── data/sql/                   # Database migrations
└── docs/                       # Documentation
```

## Configuration

Edit XML configuration files (no environment variables needed):

**`config/rtserverprops.xml`** - Server settings:
```xml
<database>
    <host>localhost</host>
    <port>5432</port>
    <name>aipr</name>
    <username>aipr</username>
    <password>your_password</password>
</database>
```

**`config/vcsplatforms.xml`** - Platform OAuth:
```xml
<github>
    <enabled>true</enabled>
    <clientId>your_client_id</clientId>
    <clientSecret>your_secret</clientSecret>
</github>
```

## Platform Authentication

Configure which platforms to enable in `config/vcsplatforms.xml`, then authenticate via:
- `http://localhost:8080/api/v1/auth/github/login`
- `http://localhost:8080/api/v1/auth/gitlab/login`
- `http://localhost:8080/api/v1/auth/bitbucket/login`

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
| [Setup Guide](docs/setup.md) | Local setup, CI/CD, TLS certificates, distribution |
| [Architecture](docs/architecture.md) | System design, authentication, gRPC communication |

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
