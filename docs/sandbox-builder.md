# Sandbox Builder

## Overview

The Sandbox Builder is an ephemeral container-based build validation system
that verifies code changes produced by the Agent Swarm compile and pass basic
checks before a pull request is created. It runs as a pipeline stage between
diff production and PR creation, catching build failures early and preventing
broken code from reaching review.

When the swarm's code agents finish implementing changes, the builder
automatically detects the project's build system, resolves required secrets,
provisions a hardened Docker container, executes the build command, and
reports the outcome — all without human intervention unless secrets are
missing or the build fails.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                       Swarm Task Pipeline                                │
│                                                                          │
│  submitted → planning → plan_review → implementing → self_review         │
│      → diff_review ──────────────────────────────────────────────┐       │
│                                                                  ▼       │
│                                                        ┌─────────────┐   │
│                                                        │   Builder   │   │
│                                                        │   Stage     │   │
│                                                        └──────┬──────┘   │
│                                                 ┌─────────────┼────────┐ │
│                                                 ▼             ▼        │ │
│                                            [success]      [fail]       │ │
│                                                 │             │        │ │
│                                                 ▼             ▼        │ │
│                                           pr_creating    build_failed  │ │
│                                                 │             │        │ │
│                                                 ▼         retry (×2)   │ │
│                                            completed          │        │ │
│                                                               ▼        │ │
│                                                        build_blocked   │ │
└────────────────────────────────────────────────────────────────────────┘ │
```

### Component Diagram

```
┌────────────────────────────────────────────────────────────────────────┐
│  Python Agent Swarm                                                    │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Pipeline Orchestrator (redis_consumer.py)                      │   │
│  │                                                                 │   │
│  │  After diff_review ──► Feature flag check ──► BuilderAgent      │   │
│  │                        (RTVORTEX_SANDBOX_ENABLED)               │   │
│  └────────────────────────────────────┬────────────────────────────┘   │
│                                       │                                │
│  ┌────────────────────────────────────▼────────────────────────────┐   │
│  │  BuilderAgent (agents/builder.py)                               │   │
│  │                                                                 │   │
│  │  1. run_probe()            → POST /internal/swarm/sandbox/probe │   │
│  │  2. confirm_and_execute()  → HITL gate → POST /resolve-execute  │   │
│  │  3. analyse_and_retry()    → GET /logs → POST /retry (×2)       │   │
│  └────────────────────────────────────┬────────────────────────────┘   │
│                                       │ HTTP                           │
└───────────────────────────────────────┼────────────────────────────────┘
                                        │
┌───────────────────────────────────────▼────────────────────────────────┐
│  Go API Server                                                         │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Sandbox Handlers                                               │   │
│  │                                                                 │   │
│  │  HandleProbeEnv          Scan files, detect build system,       │   │
│  │                          cross-reference secrets                │   │
│  │  HandleResolveAndExecute Resolve secrets from keychain,         │   │
│  │                          provision container, run build         │   │
│  │  HandleRetry             Re-execute a failed build              │   │
│  │  HandleStatus / Logs     Query build state and output           │   │
│  │  HandleListArtifacts     Retrieve collected build artifacts     │   │
│  │  HandleBuildComplexity   Complexity scoring and history         │   │
│  │  HandleAuditEvents       Query audit trail                      │   │
│  └────────────────────────────────┬────────────────────────────────┘   │
│                                   │                                    │
│  ┌────────────────────────────────▼────────────────────────────────┐   │
│  │  Execution Engine                                               │   │
│  │                                                                 │   │
│  │  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐     │   │
│  │  │ Plan         │   │ Executor     │   │ BuildStore       │     │   │
│  │  │ Generator    │   │ (Docker CLI) │   │ (PostgreSQL)     │     │   │
│  │  │              │   │              │   │                  │     │   │
│  │  │ Detect build │   │ Create       │   │ Insert / Get /   │     │   │
│  │  │ system,      │   │ container,   │   │ Complete / List  │     │   │
│  │  │ select image,│   │ inject       │   │ builds, retries  │     │   │
│  │  │ parse        │   │ secrets,     │   │ artifacts,       │     │   │
│  │  │ SANDBOX.md   │   │ wait, logs,  │   │ complexity,      │     │   │
│  │  │              │   │ destroy      │   │ fingerprints     │     │   │
│  │  └──────────────┘   └──────────────┘   └──────────────────┘     │   │
│  │                                                                 │   │
│  │  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐     │   │
│  │  │ Complexity   │   │ Fingerprint  │   │ Cache            │     │   │
│  │  │ Scorer       │   │ Engine       │   │ Manager          │     │   │
│  │  │              │   │              │   │                  │     │   │
│  │  │ 5-dimension  │   │ SHA-256 of   │   │ Per-repo Docker  │     │   │
│  │  │ weighted     │   │ build config,│   │ volumes for      │     │   │
│  │  │ scoring,     │   │ fast-path    │   │ dependency       │     │   │
│  │  │ resource     │   │ detection    │   │ layer caching    │     │   │
│  │  │ hints        │   │              │   │                  │     │   │
│  │  └──────────────┘   └──────────────┘   └──────────────────┘     │   │
│  │                                                                 │   │
│  │  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐     │   │
│  │  │ Redaction    │   │ Audit        │   │ Artifact         │     │   │
│  │  │ Engine       │   │ Logger       │   │ Collector        │     │   │
│  │  │              │   │              │   │                  │     │   │
│  │  │ 11 regex     │   │ 10 event     │   │ Classify, tar,   │     │   │
│  │  │ patterns +   │   │ types,       │   │ store binaries,  │     │   │
│  │  │ exact-match  │   │ slog + DB    │   │ coverage, test   │     │   │
│  │  │ replacement  │   │ persistence  │   │ reports          │     │   │
│  │  └──────────────┘   └──────────────┘   └──────────────────┘     │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  User-Facing API                                                │   │
│  │                                                                 │   │
│  │  GET /api/v1/swarm/tasks/{id}/builds     List builds by task    │   │
│  │  GET /api/v1/swarm/builds/{id}/logs      Fetch build logs       │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Docker Host                                                           │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Ephemeral Build Container                                      │   │
│  │                                                                 │   │
│  │  --user 1000:1000              (non-root)                       │   │
│  │  --network=none                (no network)                     │   │
│  │  --read-only                   (read-only root FS)              │   │
│  │  --security-opt=no-new-privileges                               │   │
│  │  --memory / --cpus             (resource capped)                │   │
│  │  --tmpfs /tmp                  (compiler scratch space)         │   │
│  │  -v /workspace                 (injected source files)          │   │
│  │  -v /cache                     (dependency cache volume)        │   │
│  │  --env SECRET_A=***            (runtime-only secrets)           │   │
│  │  --rm                          (auto-remove on exit)            │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  Builder Images:                                                       │
│  rtvortex/builder-go, builder-jvm, builder-python, builder-node,       │
│  builder-cpp, builder-rust, builder-general                            │
└────────────────────────────────────────────────────────────────────────┘
```

### BuilderAgent Execution Sequence

```
  BuilderAgent                  Go Server                     Docker Host
  ────────────                  ─────────                     ───────────
       │                            │                              │
       │  POST /sandbox/probe       │                              │
       │  (repo files + contents)   │                              │
       │───────────────────────────▶│                              │
       │                            │  Detect build system         │
       │                            │  Scan for env var refs       │
       │                            │  Cross-ref with keychain     │
       │◀───────────────────────────│                              │
       │  {build_system, secrets,   │                              │
       │   ready, recommendations}  │                              │
       │                            │                              │
       │  ┌──────────────────────┐  │                              │
       │  │ HITL Gate            │  │                              │
       │  │ Post plan to user    │  │                              │
       │  │ Wait for approval    │  │                              │
       │  │ (120s auto-approve)  │  │                              │
       │  └──────────┬───────────┘  │                              │
       │             │ approved     │                              │
       │                            │                              │
       │  POST /sandbox/resolve-    │                              │
       │       execute              │                              │
       │  (task, repo, secrets,     │                              │
       │   workspace files)         │                              │
       │───────────────────────────▶│                              │
       │                            │  Generate BuildPlan          │
       │                            │  Check smart-skip            │
       │                            │  Check fingerprint fast-path │
       │                            │  Resolve secrets from vault  │
       │                            │  Prepare workspace dir       │
       │                            │                              │
       │                            │  docker run (hardened)       │
       │                            │─────────────────────────────▶│
       │                            │                              │ Build
       │                            │                              │ runs
       │                            │  exit code + stdout/stderr   │
       │                            │◀─────────────────────────────│
       │                            │                              │
       │                            │  docker rm -f                │
       │                            │─────────────────────────────▶│
       │                            │                              │
       │                            │  Redact logs                 │
       │                            │  Collect artifacts           │
       │                            │  Compute complexity          │
       │                            │  Zero secrets from memory    │
       │                            │  Wipe workspace              │
       │                            │  Persist to DB               │
       │                            │  Emit audit events           │
       │◀───────────────────────────│                              │
       │  {exit_code, build_id,     │                              │
       │   logs, complexity,        │                              │
       │   artifacts, fingerprint}  │                              │
       │                            │                              │
       │  [if failed + retryable]   │                              │
       │                            │                              │
       │  POST /sandbox/retry       │                              │
       │───────────────────────────▶│  (repeat execution cycle)    │
       │                            │─────────────────────────────▶│
       │                            │◀─────────────────────────────│
       │◀───────────────────────────│                              │
       │                            │                              │
```

## Execution Flow

### 1. Probe Phase

After code agents produce diffs, the pipeline orchestrator invokes the
BuilderAgent. The agent collects scannable source files from the workspace
cache (build configs, Dockerfiles, source files with environment variable
references) and sends them to the Go server's probe endpoint.

The probe:
- Detects the build system from file patterns (Go, Gradle, Maven, CMake, Python, Node, Rust, Make, Docker)
- Scans source files for environment variable references using language-specific patterns
- Cross-references discovered variables with the user's repo-scoped secrets in the keychain
- Returns matched secrets, missing secrets, a recommended build command, and base image

### 2. HITL Confirmation

The agent posts a structured build plan summary to the task conversation,
visible in the web dashboard. If secrets are missing the human is prompted
to approve (proceed anyway), reject (abort), or add secrets first. If all
secrets are present the human sees a confirmation prompt with normal urgency.
Auto-approval triggers on HITL timeout (120 seconds).

### 3. Build Execution

On approval the agent calls the resolve-execute endpoint. The Go server:

1. Generates a `BuildPlan` from the request — selecting the build system, command, base image, and resource limits
2. Checks for a `SANDBOX.md` or `BUILD.md` in the workspace for explicit override instructions
3. Evaluates the smart-skip logic — if only non-code files changed (docs, images, configs), the build is bypassed
4. Computes a build-config fingerprint (SHA-256 of dependency files) and checks for a fast-path match with the last successful build
5. Resolves secret values from the encrypted keychain (plaintext held only in memory)
6. Prepares a temporary workspace directory with the injected source files
7. Provisions a hardened Docker container with all security constraints applied
8. Injects secrets as container environment variables and starts the build
9. Waits for the container to exit or the timeout to fire
10. Captures stdout/stderr, redacts all sensitive patterns from the log output
11. Collects build artifacts (binaries, coverage reports, test results)
12. Destroys the container and securely wipes the workspace (zero-before-delete)
13. Computes a complexity score and persists the build record to PostgreSQL
14. Emits audit events and updates Prometheus metrics

### 4. Failure Analysis and Retry

If the build fails, the agent fetches the full (redacted) logs and classifies
the failure:

| Category | Retryable | Cause |
|----------|-----------|-------|
| Authentication | No | Missing or invalid secrets |
| Compilation | No | Syntax errors, type mismatches, undefined references |
| Dependency | Yes | Registry flakes, version conflicts |
| Transient | Yes | Network errors, OOM, timeouts |
| Timeout | Yes | Build exceeded time limit |
| Unknown | Yes | Unclassified failures |

Retryable failures are automatically retried up to 2 times. Each retry is
posted as an agent message in the task conversation with the failure analysis.
After 3 total failures (initial + 2 retries) the task transitions to
`build_blocked` and requires human intervention.

### 5. Build Artifacts

After each build the artifact collector scans the workspace for:
- **Binaries** — compiled executables
- **Coverage** — code coverage reports (lcov, cobertura, etc.)
- **Test reports** — JUnit XML, pytest output
- **Logs** — build system output

Artifacts are classified, archived as tarballs, and stored in PostgreSQL.
They can be retrieved via the artifacts endpoint for inspection.

## Build System Detection

The plan generator inspects repository root files to auto-detect the
build system and select an appropriate base image:

| Build System | Detection Files | Default Command | Base Image |
|-------------|----------------|-----------------|------------|
| Go | `go.mod` | `go build ./...` | `rtvortex/builder-go` |
| Gradle | `build.gradle`, `build.gradle.kts` | `gradle build` | `rtvortex/builder-jvm` |
| Maven | `pom.xml` | `mvn package` | `rtvortex/builder-jvm` |
| CMake | `CMakeLists.txt` | `cmake --build .` | `rtvortex/builder-cpp` |
| Python | `pyproject.toml`, `setup.py` | `pip install -e .` | `rtvortex/builder-python` |
| Node | `package.json` | `npm run build` | `rtvortex/builder-node` |
| Rust | `Cargo.toml` | `cargo build` | `rtvortex/builder-rust` |
| Make | `Makefile` | `make` | `rtvortex/builder-general` |
| Docker | `Dockerfile` | `docker build .` | `rtvortex/builder-general` |

Repositories can override any of these defaults by placing a `SANDBOX.md` or
`BUILD.md` with YAML front-matter at the repository root.

## Smart Skip

Not all diffs require a build. The skip evaluator checks whether the
changeset contains only non-code files:

- Documentation (`.md`, `.txt`, `.rst`, `.adoc`)
- Images (`.png`, `.jpg`, `.svg`, `.gif`, `.ico`)
- Configuration (`.yml`, `.yaml`, `.toml`, `.ini`, `.json` outside `package.json`)
- CI definitions (`.github/`, `.gitlab-ci.yml`)
- License files

If all changed files match skip patterns the build is bypassed with a
`skipped` status and the pipeline proceeds directly to PR creation.

## Dependency Layer Caching

Each repository gets a persistent Docker volume for its build system's
dependency cache. This avoids re-downloading dependencies on every build:

| Build System | Cache Path | Volume Name |
|-------------|------------|-------------|
| Go | `/go/pkg/mod` | `rtvortex-cache-{repo}-go` |
| Node | `/root/.npm` | `rtvortex-cache-{repo}-npm` |
| Python | `/root/.cache/pip` | `rtvortex-cache-{repo}-pip` |
| Gradle | `/root/.gradle` | `rtvortex-cache-{repo}-gradle` |
| Maven | `/root/.m2` | `rtvortex-cache-{repo}-m2` |
| Rust | `/root/.cargo/registry` | `rtvortex-cache-{repo}-cargo` |
| CMake | `/root/.cmake` | `rtvortex-cache-{repo}-cmake` |

## Build-Config Fingerprinting

Before executing a build, the system computes a SHA-256 fingerprint of the
project's dependency declaration files (`go.mod`, `go.sum`, `package.json`,
`package-lock.json`, `pom.xml`, `Cargo.toml`, `requirements.txt`, etc.).

If the fingerprint matches the last successful build for the same repository,
the system takes a **fast path** — skipping dependency installation and
running only the compile/test step. This significantly reduces build times
for changes that don't modify dependencies.

## Complexity Scoring

Every build receives a complexity score (0.0–1.0) computed from five
weighted dimensions:

| Signal | Weight | Source |
|--------|--------|--------|
| File count | 0.15 | Number of workspace files |
| Build system | 0.20 | System complexity (Gradle/CMake = high, Go = low) |
| Secret count | 0.15 | Number of secret references |
| Pre-commands | 0.10 | Number of pre-build commands |
| Workspace size | 0.10 | Total file bytes |
| Duration | 0.30 | Actual execution time (post-build) |

Scores map to labels: **trivial** (<0.10), **simple** (<0.25),
**moderate** (<0.50), **complex** (<0.75), **extreme** (≥0.75).

Resource hints (memory, CPU, timeout multipliers) scale with the
classification. Historical statistics (success rate, average duration by
label) are tracked per repository to inform future builds.

## Security

### Secret Lifecycle

Secrets follow a strict lifecycle that ensures plaintext values never touch
disk, logs, or persistent storage:

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  User stores     │     │  Probe returns   │     │  Human approves  │
│  secret via UI   │────▶│  key names only  │────▶│  build plan      │
│  (AES-256-GCM)   │     │  (never values)  │     │  (HITL gate)     │
└──────────────────┘     └──────────────────┘     └────────┬─────────┘
                                                           │
                                                           ▼
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  Memory wiped    │     │  Container runs  │     │  Go resolves     │
│  (zeroed after   │◀────│  and exits       │◀────│  secrets from    │
│  container exit) │     │  → destroyed     │     │  keychain        │
└──────────────────┘     └──────────────────┘     │  (in-memory only)│
                                                  │  → injects as    │
                                                  │  container env   │
                                                  └──────────────────┘
```

### Container Hardening

Every build container runs with the following constraints:

- **Non-root execution** — `--user 1000:1000`
- **Network isolation** — `--network=none` prevents outbound calls
- **Read-only root filesystem** — `--read-only` when sandbox mode is enabled
- **No privilege escalation** — `--security-opt=no-new-privileges:true`
- **Seccomp profile** — Custom syscall whitelist restricts available system calls
- **Resource limits** — Memory (2 GB default), CPU (2 cores default), timeout (10 min default)
- **Tmpfs scratch** — `/tmp` mounted as tmpfs for compiler intermediates
- **Auto-removal** — `--rm` ensures the container is deleted on exit
- **Secure cleanup** — Workspace files are zero-filled before deletion

### Log Redaction

All build logs pass through a redaction engine before being stored or
returned to clients. The engine applies 11 regex patterns covering:

- AWS access keys, GitHub tokens, GitLab tokens
- JWTs, PEM private keys, connection strings
- Email addresses, password assignments, IP:port pairs

Additionally, any secret value that was injected into the container is
checked against the log output via exact-match replacement. The response
includes a `logs_redacted` flag indicating whether any redaction occurred.

### Audit Trail

Ten event types are logged to structured slog output and optionally
persisted to the `swarm_audit_events` PostgreSQL table:

| Event | Trigger |
|-------|---------|
| `secret_access` | Secret value resolved from keychain |
| `secret_denied` | Secret resolution failed (key not found or access denied) |
| `container_created` | Ephemeral container started |
| `container_destroyed` | Container removed after build |
| `log_redacted` | Sensitive patterns scrubbed from logs |
| `workspace_scrubbed` | Workspace files zeroed and removed from disk |
| `access_denied` | Build ownership check failed |
| `data_export` | Build data exported via API |
| `config_change` | Sandbox configuration modified |
| `ownership_check` | Build ownership validated successfully |

Audit events can be queried via `GET /sandbox/audit` with filters on
user, repository, build, action, and time range.

## Configuration

The sandbox is configured via the `<sandbox>` element in
`rtserverprops.xml`:

```xml
<sandbox enabled="${RTVORTEX_SANDBOX_ENABLED:false}"
         always-validate="false"
         default-sandbox-mode="true"
         max-timeout-seconds="600"
         max-memory-mb="2048"
         max-cpu="2"
         max-retries="2"/>
```

| Property | Default | Description |
|----------|---------|-------------|
| `enabled` | `false` | Master feature flag — sandbox is opt-in |
| `always-validate` | `false` | Force builds even for non-code changes |
| `default-sandbox-mode` | `true` | Read-only workspace by default |
| `max-timeout-seconds` | `600` | Maximum build duration |
| `max-memory-mb` | `2048` | Maximum container memory |
| `max-cpu` | `2` | Maximum container CPU cores |
| `max-retries` | `2` | Maximum retry attempts on failure |

The Python swarm reads `RTVORTEX_SANDBOX_ENABLED` as an environment
variable for the feature flag and parses the full `<sandbox>` element
from the server configuration for limit overrides.

## API Reference

### Internal Endpoints (Agent → Go Server)

All under `/internal/swarm/sandbox/`:

| Method | Path | Description |
|--------|------|-------------|
| POST | `/plan` | Generate a build plan from repository analysis |
| POST | `/probe` | Run pre-build environment probe |
| POST | `/execute` | Execute a build from an existing plan |
| POST | `/resolve-execute` | Resolve secrets and execute in one call |
| POST | `/retry` | Retry a failed build |
| GET | `/status/{id}` | Query build status |
| GET | `/logs/{id}` | Fetch build logs (redacted) |
| GET | `/secrets` | List available secret names for a repo |
| GET | `/artifacts/{id}` | List collected build artifacts |
| GET | `/complexity/{repo_id}` | Complexity scoring and historical stats |
| GET | `/health` | Runtime health check and config limits |
| GET | `/audit` | Query audit events with filters |

### User-Facing Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/swarm/tasks/{id}/builds` | List builds for a task with summary stats |
| GET | `/api/v1/swarm/builds/{id}/logs` | Fetch build logs for display in the dashboard |

### Keychain Endpoints (Build Secrets)

| Method | Path | Description |
|--------|------|-------------|
| PUT | `/api/v1/repos/{repoID}/build-secrets` | Store a repo-scoped build secret |
| GET | `/api/v1/repos/{repoID}/build-secrets` | List secret names for a repo |
| DELETE | `/api/v1/repos/{repoID}/build-secrets/{name}` | Delete a build secret |

## Web Dashboard

The task detail page includes a **Builds** tab that displays all build
executions for the task:

- **Build list** — expandable rows showing status, build system, command, duration, exit code, retry count, and container image
- **Status indicators** — color-coded badges for success, failed, blocked, skipped, and running states
- **Sandbox mode** — shield icon for builds running in read-only mode
- **Retry history** — retry count badges on builds that required re-execution
- **Log viewer** — dark terminal-style viewer with clipboard copy and redaction indicator
- **Secret references** — list of injected secret names (never values)
- **Summary statistics** — total builds, success rate, average duration, total retries
- **Auto-refresh** — polls for updates every 10 seconds during active builds

## Observability

### Prometheus Metrics

22 metrics under the `rtvortex_sandbox_*` namespace:

| Metric | Type | Description |
|--------|------|-------------|
| `build_total` | CounterVec | Builds by status |
| `build_duration_seconds` | Histogram | Execution duration |
| `build_retries_total` | Counter | Retry attempts |
| `build_secret_injects_total` | Counter | Secrets injected into containers |
| `build_containers_active` | Gauge | Currently running containers |
| `build_cache_hits_total` | Counter | Dependency cache hits |
| `build_skipped_total` | Counter | Builds skipped (non-code changes) |
| `probe_total` | Counter | Probes executed |
| `probe_missing_secrets_total` | Counter | Missing secrets detected |
| `hitl_confirmations_total` | CounterVec | HITL approval outcomes |
| `secret_resolutions_total` | CounterVec | Secret resolution results |
| `artifacts_collected_total` | Counter | Artifacts collected |
| `artifact_bytes_total` | Counter | Total artifact bytes |
| `workspace_injections_total` | Counter | Workspace file injections |
| `build_complexity_total` | CounterVec | Builds by complexity label |
| `build_complexity_score` | Histogram | Raw complexity scores (0–1) |
| `build_failure_probability` | Histogram | Predicted failure probability |
| `build_fast_path_total` | Counter | Fast-path builds (fingerprint match) |
| `build_fingerprint_hits_total` | Counter | Fingerprint cache matches |
| `build_config_limits_applied_total` | Counter | Server config overrides applied |
| `audit_events_total` | CounterVec | Audit events by action type |
| `log_redactions_total` | Counter | Log redaction operations |
| `secret_leak_detections_total` | Counter | Verbatim secret leak detections |
| `ownership_check_failures_total` | Counter | Failed ownership validations |

## Database

### Tables

| Table | Purpose |
|-------|---------|
| `swarm_builds` | Build execution records — status, exit code, duration, command, image, secrets, complexity, fingerprint |
| `swarm_audit_events` | Audit trail — action, user, repo, build, detail (JSONB), timestamp |
| `keychain_secrets` | Encrypted repo-scoped secrets (with `repo_id` for build secret scoping) |

### Schema

```sql
CREATE TABLE swarm_builds (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID NOT NULL REFERENCES swarm_tasks(id),
    repo_id       UUID NOT NULL REFERENCES repositories(id),
    user_id       UUID NOT NULL REFERENCES users(id),
    build_system  TEXT NOT NULL,
    command       TEXT NOT NULL,
    base_image    TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending',
    exit_code     INTEGER,
    log_summary   TEXT NOT NULL DEFAULT '',
    secret_names  TEXT[] NOT NULL DEFAULT '{}',
    sandbox_mode  BOOLEAN NOT NULL DEFAULT true,
    retry_count   INTEGER NOT NULL DEFAULT 0,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    complexity    JSONB,
    fingerprint   JSONB
);

CREATE TABLE swarm_audit_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action     TEXT NOT NULL,
    user_id    UUID,
    repo_id    UUID,
    build_id   UUID,
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## SANDBOX.md Specification

Repository authors can place a `SANDBOX.md` or `BUILD.md` at the root to
provide explicit build instructions. The builder parses YAML front-matter
for configuration overrides:

```markdown
---
build_system: gradle
base_image: eclipse-temurin:17-jdk-jammy
timeout: 300
memory: 4g
cpu: 4
---

## Build
`./gradlew build -x test`

## Test
`./gradlew test`

## Required Secrets
- SONAR_TOKEN
- ARTIFACTORY_URL
```

Supported front-matter fields: `build_system`, `base_image`, `command`,
`pre_commands`, `timeout`, `memory`, `cpu`, `sandbox_mode`.

When present, the builder uses these instructions directly instead of
inferring from file patterns.
