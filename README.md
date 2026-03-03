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

A platform-neutral PR review engine with connectors for GitHub, GitLab, Bitbucket, and Azure DevOps.

[Setup Guide](docs/setup.md) | [Architecture](docs/architecture.md)

</div>

---

## Features

- **Platform Agnostic**: Works with GitHub, GitLab, Bitbucket, and Azure DevOps (cloud & self-hosted)
- **CI/CD Integration**: GitHub Actions, GitLab CI, Jenkins, Bitbucket Pipelines, Azure Pipelines
- **High Performance**: C++ engine for code indexing and analysis via gRPC
- **LLM Powered**: Supports OpenAI, Anthropic, Azure OpenAI, Ollama, and compatible APIs
- **Multi-Cloud Storage**: Local, AWS S3, GCS, Azure Blob, OCI Object Storage, MinIO
- **Self-Hostable**: Deploy on your infrastructure with full control
- **TLS/mTLS Support**: Secure gRPC communication with included dev certificates
- **Unified Configuration**: Single XML config (`rtserverprops.xml`) drives both Java and C++ components

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

## Building the C++ Engine

The C++ engine is an independent CMake project that can be built separately from the Java server.

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

### Configure & Build

From the repository root:

```bash
# Configure (fetches gRPC, abseil, nlohmann/json, tree-sitter, ONNX Runtime automatically)
cmake -B build -S mono/engine \
  -DCMAKE_BUILD_TYPE=Release \
  -DAIPR_BUILD_TESTS=ON \
  -DAIPR_BUILD_SERVER=ON \
  -DAIPR_USE_FAISS=ON \
  -DAIPR_USE_TREE_SITTER=ON

# Build (all targets)
cmake --build build --parallel $(nproc)
```

### CMake Options

| Option | Default | Description |
|--------|---------|-------------|
| `AIPR_BUILD_TESTS` | `ON` | Build unit tests (`aipr-engine-tests`) |
| `AIPR_BUILD_SERVER` | `ON` | Build the gRPC server (`aipr-engine-server`) |
| `AIPR_BUILD_TOOLS` | `ON` | Build utility tools |
| `AIPR_USE_FAISS` | `ON` | Enable FAISS vector search |
| `AIPR_USE_TREE_SITTER` | `ON` | Enable tree-sitter AST parsing |
| `AIPR_USE_ONNX` | `ON` | Enable ONNX Runtime local embeddings |

### Build Targets

| Target | Description |
|--------|-------------|
| `aipr-engine` | Core static library |
| `aipr-engine-server` | gRPC server executable |
| `aipr-engine-doctor` | Diagnostics tool |
| `aipr-engine-bench` | Benchmarking tool |
| `aipr-engine-tests` | Unit test suite (Google Test) |
| `tms-demo` | TMS demo application |

### Running Tests

```bash
ctest --test-dir build --output-on-failure -C Release
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

All configuration is centralized in `config/rtserverprops.xml`. The Java server reads this file and pushes relevant settings (storage, LLM, etc.) to the C++ engine via gRPC at startup.

**`config/rtserverprops.xml`** - Server settings:
```xml
<!-- Database -->
<database url="jdbc:postgresql://localhost:5432/aipr"
          username="aipr" password="your_password"/>

<!-- LLM Providers (multiple supported with failover) -->
<llm primary="openai" fallback="ollama">
    <openai api-key="${LLM_OPENAI_API_KEY}" model="gpt-4-turbo-preview"/>
    <anthropic api-key="${LLM_ANTHROPIC_API_KEY}"/>
    <azure-openai endpoint="${LLM_AZURE_OPENAI_ENDPOINT}" deployment="gpt-4"/>
    <ollama base-url="http://localhost:11434"/>
</llm>

<!-- Storage (pushed to C++ engine via gRPC) -->
<storage type="s3">
    <s3 bucket="my-bucket" region="us-east-1"/>
</storage>
```

**`config/vcsplatforms.xml`** - Platform OAuth:
```xml
<github enabled="true" client-id="your_id" client-secret="your_secret"/>
<azure-devops enabled="true" 
              client-id="your_id" 
              tenant-id="your_tenant"
              webhook-secret="your_hmac_secret"/>
```

## Platform Authentication

Configure which platforms to enable in `config/vcsplatforms.xml`, then authenticate via:
- `http://localhost:8080/api/v1/auth/github/login`
- `http://localhost:8080/api/v1/auth/gitlab/login`
- `http://localhost:8080/api/v1/auth/bitbucket/login`
- `http://localhost:8080/api/v1/auth/azure-devops/login`

## LLM Provider Management

The server auto-discovers local Ollama instances and provides a REST API for provider management:

```bash
# List all providers with health status
curl http://localhost:8080/api/v1/llm/providers

# Switch active provider
curl -X POST http://localhost:8080/api/v1/llm/providers/switch \
  -d '{"provider": "ollama-local"}'
```

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
