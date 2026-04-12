# Sandbox Builder — Architecture & Implementation

## Overview

The Sandbox Builder validates code changes through ephemeral container builds
before PRs are created.  It integrates as a pipeline stage in the swarm task
lifecycle, running after implementation agents produce diffs and before diff
review begins.

```
implementing → self_review → diff_review
    → build_validating → [success] → pr_creating → completed
    → build_validating → [fail]    → build_failed → [fix approved] → retry (×2)
                                   → build_blocked (3rd failure)
```

## Component Map

### Go Server — `internal/sandbox/`

| File | Lines | Purpose |
|------|-------|---------|
| `handler.go` | 1124 | 11 HTTP handlers: plan, execute, resolve-execute, retry, status, logs, secrets, probe, artifacts, complexity, health |
| `executor.go` | 490 | `ContainerRuntime` interface + `DockerRuntime` (Docker SDK), workspace preparation, container lifecycle |
| `plan.go` | 292 | `BuildPlan` struct, `GeneratePlan()` from request + file analysis, `SANDBOX.md`/`BUILD.md` parsing |
| `store.go` | 264 | `BuildStore` — PostgreSQL persistence via pgx (`swarm_builds` table): insert, get, list, complete, retry, complexity, fingerprint |
| `complexity.go` | 469 | Build complexity scoring: signal extraction, weighted scoring (0–1), classification (trivial/simple/moderate/complex/extreme), resource hints, historical stats |
| `artifacts.go` | 254 | Build artifact collection: classification (binary/coverage/test-report/log), workspace archive/extract, log parsing |
| `fingerprint.go` | 213 | Build-config fingerprinting: SHA-256 of build files (go.mod, package.json, etc.), fast-path detection when fingerprint matches last success |
| `metrics.go` | 208 | 22 Prometheus metrics: build total/duration/retries, secret injects, cache hits, probe stats, complexity distribution, audit events, leak detection |
| `audit.go` | 187 | `AuditLogger` with structured slog + optional PostgreSQL persistence, 10 event types, `ValidateBuildOwnership`, `SecureCleanupWorkspace` |
| `redact.go` | 95 | Log redaction: 11 regex patterns (AWS keys, GitHub/GitLab tokens, JWTs, PEM keys, connection strings, emails, passwords, IPs), exact-match secret replacement |
| `sandbox.go` | 117 | `ContainerRuntime` interface, `DockerRuntime` struct, `MockRuntime` for tests |
| `build_system.go` | 86 | Build system detection from file patterns: Go, Gradle, Maven, CMake, Python, Node, Rust, Make, Docker, Custom |
| `skip.go` | 173 | Smart skip logic: non-code-only diffs (docs, images, config) bypass builds |
| `cache.go` | 84 | Dependency layer caching: per-repo Docker volume mounts for Go, Node, Python, Gradle, Maven, Rust, CMake caches |

### Go Server — Routes (registered in `server.go`)

All under `/internal/swarm/sandbox/`:

| Method | Path | Handler | Phase |
|--------|------|---------|-------|
| POST | `/plan` | `HandleGeneratePlan` | 1 |
| POST | `/probe` | `HandleProbeEnv` | 3 |
| POST | `/execute` | `HandleExecute` | 4 |
| POST | `/resolve-execute` | `HandleResolveAndExecute` | 4 |
| POST | `/retry` | `HandleRetry` | 6 |
| GET | `/status/{id}` | `HandleStatus` | 5 |
| GET | `/logs/{id}` | `HandleLogs` | 5 |
| GET | `/secrets` | `HandleListBuildSecrets` | 2 |
| GET | `/artifacts/{id}` | `HandleListArtifacts` | 8 |
| GET | `/complexity/{repo_id}` | `HandleBuildComplexity` | 9 |
| GET | `/health` | `HandleHealth` | 10 |

### Go Server — Tests (`tests/sandbox/`)

| File | Tests | Coverage |
|------|-------|----------|
| `handler_test.go` | 602 lines | Full pipeline: plan→execute→resolve-execute, workspace injection, skip logic, complexity in response |
| `complexity_test.go` | 702 lines | Signal extraction, scoring bounds, classification labels, resource hints, historical stats, duration buckets |
| `fingerprint_test.go` | 558 lines | Hash computation, fast-path detection, config-driven limits, health endpoint |
| `executor_test.go` | 369 lines | Container create/wait/destroy, workspace preparation, mock runtime |
| `compliance_test.go` | 372 lines | Redaction patterns (12 types), audit logger operations, ownership validation, secure cleanup |
| `artifacts_test.go` | 283 lines | Artifact classification, workspace archive round-trip, log parsing |
| `plan_test.go` | 237 lines | Plan generation, build system defaults, SANDBOX.md parsing |
| `store_test.go` | 224 lines | DB integration tests (skipped without `SANDBOX_TEST_DB`) |
| `skip_cache_test.go` | 177 lines | Smart skip patterns, cache volume resolution |
| `build_system_test.go` | 81 lines | Build system detection from file lists |

### Python Swarm — `mono/swarm/`

| File | Lines | Purpose |
|------|-------|---------|
| `agents/builder.py` | 845 | `BuilderAgent` class: probe, confirm+execute, analyse+retry, system prompt, HITL flow |
| `go_client.py` | 1190 | 12 sandbox methods: probe, plan, secrets, resolve-execute, status, logs, retry, artifacts, complexity, health, audit-events |
| `redis_consumer.py` | — | Pipeline integration: `RTVORTEX_SANDBOX_ENABLED` feature flag, builder stage insertion, probe→confirm→execute flow |
| `tools/workspace_tools.py` | — | `workspace_edit_or_create`, `workspace_create_module` tools for workspace hardening |

### Database (Migration 000027)

```sql
-- Adds repo_id to keychain_secrets
ALTER TABLE keychain_secrets ADD COLUMN repo_id UUID REFERENCES repositories(id);

-- Build execution records
CREATE TABLE swarm_builds (
    id, task_id, repo_id, user_id, build_system, command, base_image,
    status, exit_code, log_summary, secret_names, sandbox_mode,
    retry_count, duration_ms, created_at, completed_at,
    complexity JSONB, fingerprint JSONB
);
```

### Configuration

XML (`mono/config/rtserverprops.xml`):
```xml
<sandbox enabled="${RTVORTEX_SANDBOX_ENABLED:false}"
         always-validate="false"
         default-sandbox-mode="true"
         max-timeout-seconds="600"
         max-memory-mb="2048"
         max-cpu="2"
         max-retries="2"/>
```

Go struct (`config/config.go`):
```go
type SandboxConfig struct {
    Enabled        bool
    AlwaysValidate bool
    DefaultSandbox bool
    MaxTimeoutSec  int  // 600
    MaxMemoryMB    int  // 2048
    MaxCPU         int  // 2
    MaxRetries     int  // 2
}
```

Python: `RTVORTEX_SANDBOX_ENABLED` env var checked in `redis_consumer.py`.

## Security Model

### Secret Lifecycle

```
User stores secret → Keychain (AES-256-GCM encrypted in Postgres)
    ↓
BuilderAgent queries secret names → GET /sandbox/secrets
    ↓ (key names only, never values)
User approves build (HITL) → Build triggered
    ↓
Go executor calls keychain.GetSecret() for each ref
    ↓ (plaintext in-memory only)
Values injected as container env vars → container.Create(Env: [...])
    ↓
Container runs and exits → container destroyed
    ↓
Go zeroes secretSnapshot map → memory wiped
```

### Container Hardening

- **No root**: `User: "1000:1000"`
- **No network**: `NetworkMode: "none"`
- **No host mounts**: Only temp workspace directory
- **Resource limits**: Memory (2GB default), CPU (2 cores default), timeout (10 min)
- **Auto-cleanup**: Container destroyed on exit + workspace dir removed
- **Secret wiping**: `SecureCleanupWorkspace` zeroes in-memory file maps, overwrites disk files

### Log Redaction

All build logs are scrubbed before persistence or response:

| Pattern | Example |
|---------|---------|
| AWS AKIA keys | `AKIAIOSFODNN7EXAMPLE` |
| GitHub tokens | `ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx` |
| GitLab tokens | `glpat-xxxxxxxxxxxxxxxxxxxx` |
| JWTs | `eyJhbGciOiJI...` |
| PEM private keys | `-----BEGIN RSA PRIVATE KEY-----` |
| Connection strings | `postgres://user:pass@host` |
| Email addresses | `user@example.com` |
| Password assignments | `PASSWORD=secret`, `"secret": "value"` |
| IP:port | `10.0.0.1:5432` |

### Audit Trail

10 audit event types logged to slog + optional PostgreSQL:

| Action | Trigger |
|--------|---------|
| `secret_access` | Secret value resolved from keychain |
| `secret_denied` | Secret resolution failed |
| `container_created` | Ephemeral container started |
| `container_destroyed` | Container removed |
| `log_redacted` | Secret patterns scrubbed from logs |
| `workspace_scrubbed` | Workspace files zeroed and removed |
| `access_denied` | Ownership check failed |
| `data_export` | Build data exported |
| `config_change` | Sandbox config modified |
| `ownership_check` | Build ownership validated |

## Prometheus Metrics

22 metrics under `rtvortex_sandbox_*`:

| Metric | Type | Description |
|--------|------|-------------|
| `build_total` | CounterVec | Builds by status (success/failed/blocked/skipped) |
| `build_duration_seconds` | Histogram | Execution duration |
| `build_retries_total` | Counter | Retry attempts |
| `build_secret_injects_total` | Counter | Secrets injected |
| `build_containers_active` | Gauge | Currently running containers |
| `build_cache_hits_total` | Counter | Layer cache hits |
| `build_skipped_total` | Counter | Non-code diffs skipped |
| `probe_total` | Counter | Probes executed |
| `probe_missing_secrets_total` | Counter | Missing secrets detected |
| `hitl_confirmations_total` | CounterVec | HITL outcomes |
| `secret_resolutions_total` | CounterVec | Resolution results |
| `artifacts_collected_total` | Counter | Artifacts collected |
| `artifact_bytes_total` | Counter | Artifact bytes |
| `workspace_injections_total` | Counter | Workspace injections |
| `build_complexity_total` | CounterVec | Complexity labels |
| `build_complexity_score` | Histogram | Raw score (0–1) |
| `build_failure_probability` | Histogram | Failure probability |
| `build_fast_path_total` | Counter | Fast-path builds |
| `build_fingerprint_hits_total` | Counter | Fingerprint matches |
| `build_config_limits_applied_total` | Counter | Config overrides |
| `audit_events_total` | CounterVec | Audit events by action |
| `log_redactions_total` | Counter | Log redaction operations |
| `secret_leak_detections_total` | Counter | Verbatim secret leaks |
| `ownership_check_failures_total` | Counter | Failed ownership checks |

## Build Complexity Scoring

Five-dimension weighted scoring (0.0–1.0):

| Signal | Weight | Source |
|--------|--------|--------|
| File count | 0.15 | len(WorkspaceFS) |
| Build system | 0.20 | Gradle/CMake = high, Go = low |
| Secret count | 0.15 | len(SecretRefs) |
| Pre-commands | 0.10 | len(PreCommands) |
| Workspace size | 0.10 | Sum of file bytes |
| Duration (post-build) | 0.30 | Actual execution time |

Classification: trivial (<0.10), simple (<0.25), moderate (<0.50), complex (<0.75), extreme (≥0.75).

Resource hints scale memory/CPU/timeout based on label.

## Implementation Phases — Completion Status

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| 1 | BuilderAgent role + workspace hardening | ✅ Done | `44a871f` |
| 2 | Repo-scoped secrets in user keychain | ✅ Done | `bc61284` |
| 3 | Pre-build environment probe | ✅ Done | `e58286a` |
| 4 | Build router + secret resolution + HITL | ✅ Done | `bec1324` |
| 5 | Build lifecycle persistence + status/logs | ✅ Done | `dea3640` |
| 6 | Build failure analysis + retry loop | ✅ Done | `40be3b4` |
| 7 | Dependency layer caching + smart skip | ✅ Done | `59fe092` |
| 8 | Build artifact collection + workspace injection | ✅ Done | `c0de03c` |
| 9 | Build complexity scoring | ✅ Done | `69f1d80` |
| 10 | Caching & performance — fingerprinting, fast-path | ✅ Done | `71e5a2e` |
| 11 | Data lockdown — log redaction, audit, secure cleanup | ✅ Done | `58bf075` |
| 12 | Monitoring & alerting (Prometheus) | ✅ Done | Incremental across phases |
| 13 | Documentation + rollout | 🔄 In Progress | This file |

## Gaps & Remaining Work

### Completed but Not in Original Plan

- Build-config fingerprinting with SHA-256 (Phase 10)
- Fast-path detection skipping dependency install on fingerprint match (Phase 10)
- Config-driven server-side limits from `rtserverprops.xml` (Phase 10)
- Log redaction engine with 11 regex patterns + exact-match secret replacement (Phase 11)
- `SecureCleanupWorkspace` with zero-before-delete semantics (Phase 11)
- `logs_redacted` flag in API response (Phase 11)
- Health endpoint with runtime status + config limits (Phase 10)

### Gaps — Items From Plan Not Yet Implemented

| Item | Plan Reference | Status | Priority |
|------|---------------|--------|----------|
| `swarm_audit_events` DB migration | Phase 11 | ❌ Missing — audit.go references table but no migration exists | High |
| `GET /sandbox/audit` endpoint | Phase 11 | ❌ Missing — `go_client.py` has `sandbox_audit_events()` method but no Go handler | High |
| Builder image Dockerfiles | Phase 5 | ❌ Missing — `mono/deploy/docker/builder-images/` not created | Medium |
| Seccomp profile | Phase 9 | ❌ Missing — `mono/deploy/docker/security/seccomp-builder.json` not created | Medium |
| Build validation tab (web UI) | Phase 7 | ❌ Missing — no `build-validation-tab.tsx`, no "build" ContentTab | Low (UI) |
| Repo build-secrets UI endpoints | Phase 2 | ❌ Missing — `PUT/GET/DELETE /api/v1/repos/{repoID}/build-secrets` user-facing CRUD not implemented | Medium |
| `agents_config.py` sandbox parsing | Phase 11 | ❌ Missing — Python reads env var only, doesn't parse XML `<sandbox>` element | Low |
| `SANDBOX.md` spec document | Phase 13 | ❌ Missing — spec for repo-level build instructions format | Low |
| Integration tests with mock Docker | Phase 13 | ⚠️ Partial — unit tests use MockRuntime, no full E2E with Docker daemon mock | Low |

### Code Quality Notes

- Total Go sandbox code: **4,056 lines** (14 files)
- Total Go test code: **3,605 lines** (10 files)
- Test-to-code ratio: **0.89** (excellent)
- Python builder: **845 lines**
- Python client methods: **12 sandbox endpoints**
- Prometheus metrics: **22 counters/histograms/gauges**
- All code compiles clean (`go vet`, `go build`)
- All tests pass (`go test ./tests/sandbox/`)

## SANDBOX.md Spec (for repo authors)

Repos can include a `SANDBOX.md` or `BUILD.md` at their root to provide
explicit build instructions.  The builder parses YAML front-matter and
markdown sections:

```markdown
---
build_system: gradle
base_image: eclipse-temurin:17-jdk-jammy
timeout: 300
memory: 4g
cpu: 4
---

# Build Instructions

## Pre-build
- `chmod +x gradlew`
- `export JAVA_HOME=/opt/java/openjdk`

## Build
`./gradlew build -x test`

## Test
`./gradlew test`

## Required Secrets
- SONAR_TOKEN
- ARTIFACTORY_URL
```

When present, the builder uses these instructions directly instead of
inferring from file patterns.
