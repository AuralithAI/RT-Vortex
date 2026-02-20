# Contributing to AI PR Reviewer

Thank you for your interest in contributing to AI PR Reviewer! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Contribution Guidelines](#contribution-guidelines)
- [Pull Request Process](#pull-request-process)
- [Coding Standards](#coding-standards)

## Code of Conduct

This project adheres to a Code of Conduct. By participating, you are expected to uphold this code. Please report unacceptable behavior to conduct@auralithai.com.

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Set up your development environment (see below)
4. Create a feature branch from `main`
5. Make your changes
6. Submit a pull request

## Development Setup

### Prerequisites

- **C++ Toolchain**: GCC 11+ / Clang 14+ / MSVC 2022
- **CMake**: 3.20+
- **Java**: JDK 21+ (or use the bundled JRE for runtime)
- **Gradle**: 8.5+ (or use the bundled version)
- **Python**: 3.10+ (for SDK and tooling)
- **Node.js**: 18+ (for Node SDK)

### Quick Start

```bash
# Clone the repository
git clone https://github.com/AuralithAI/RT-AI-PR-Reviewer.git
cd RT-AI-PR-Reviewer

# Run setup (downloads dependencies, sets up toolchains)
./scripts/setup.sh

# Build everything
./scripts/build.sh

# Run tests
./scripts/test.sh
```

### Manual Setup

If you have Gradle and Java already installed:

```bash
# Build the server
cd server
gradle build

# Build the engine
cd core/engine-cpp
mkdir build && cd build
cmake ..
make -j$(nproc)

# Build the CLI
cd cli
# Follow CLI-specific build instructions
```

## Project Structure

```
ai-pr-reviewer/
├─ core/engine-cpp/    # C++ engine (indexing, retrieval, review signals)
├─ server/             # Java server (API, jobs, auth, policy)
├─ cli/                # Universal CLI runner
├─ sdk/                # Client SDKs (Java, Python, Node)
├─ integrations/       # Platform adapters (GitHub, GitLab, etc.)
├─ schemas/            # JSON schemas (contracts)
├─ config/             # Configuration profiles
├─ docs/               # Documentation
├─ evals/              # Quality evaluation suite
└─ deploy/             # Deployment artifacts
```

## Contribution Guidelines

### What We're Looking For

- Bug fixes with tests
- Performance improvements with benchmarks
- New language support in the chunker
- Documentation improvements
- Integration adapters for new platforms
- Evaluation cases (golden PRs)

### Before You Start

1. Check existing issues and PRs to avoid duplicates
2. For major changes, open an issue first to discuss
3. Ensure your change aligns with the project's architecture

## Pull Request Process

1. **Branch Naming**: Use descriptive names
   - `feat/add-rust-parser`
   - `fix/memory-leak-indexer`
   - `docs/update-deployment-guide`

2. **Commit Messages**: Follow conventional commits
   ```
   feat(engine): add Rust AST chunker support
   fix(server): resolve memory leak in index worker
   docs: update GitHub integration guide
   ```

3. **Testing**: All PRs must include appropriate tests
   - C++: Add tests in `core/engine-cpp/tests/`
   - Java: Add tests in `server/src/test/java/`
   - Integration: Add smoke tests if applicable

4. **Documentation**: Update relevant documentation

5. **Review**: Address all review comments before merge

## Coding Standards

### C++ (Engine)

- Follow the `.clang-format` configuration
- Use modern C++ (C++17 minimum)
- Prefer RAII and smart pointers
- Document public APIs in headers

### Java (Server)

- Follow Google Java Style Guide
- Use Checkstyle (configuration in `ci/lint/checkstyle.xml`)
- Write Javadoc for public APIs
- Use dependency injection

### Python (SDK)

- Follow PEP 8
- Use type hints
- Format with Black
- Lint with Ruff

### General

- Keep functions small and focused
- Write self-documenting code
- Add comments for complex logic
- Avoid premature optimization

## Questions?

Feel free to open an issue for questions or reach out to the maintainers.
