// Package sandbox implements the ephemeral container build system for
// the RTVortex Agent Swarm.
//
// The sandbox validates that agent-produced diffs actually compile by
// running the project's build command inside an ephemeral Docker container.
// Secrets are resolved and injected ONLY at container runtime and NEVER
// persisted to disk or logs.
//
// Architecture:
//
//	┌───────────────────────────────────────────────────────┐
//	│  ContainerRuntime interface                           │
//	│                                                       │
//	│  Implementations:                                     │
//	│    ├── DockerRuntime      (Day 1 — exec docker run)   │
//	│    ├── FirecrackerRuntime (Future — microVM)          │
//	│    └── K8sRuntime         (Future — K8s Job/Pod)      │
//	│    └── MockRuntime        (Testing)                   │
//	└───────────────────────────────────────────────────────┘
package sandbox

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// ── BuildPlan ───────────────────────────────────────────────────────────────

// BuildPlan describes everything needed to execute a sandboxed build.
type BuildPlan struct {
	ID           uuid.UUID                `json:"id"`
	TaskID       uuid.UUID                `json:"task_id"`
	RepoID       string                   `json:"repo_id"`
	BuildSystem  string                   `json:"build_system"` // "gradle", "cmake", "python", etc.
	Command      string                   `json:"command"`      // e.g. "gradle build"
	PreCommands  []string                 `json:"pre_commands"` // e.g. ["chmod +x gradlew"]
	EnvVars      map[string]string        `json:"-"`            // populated at runtime from secrets — NEVER serialised
	SecretRefs   []string                 `json:"secret_refs"`  // key names only — ["JAVA_HOME", "DB_URL"]
	BaseImage    string                   `json:"base_image"`   // e.g. "rtvortex/builder-jvm:17"
	SandboxMode  bool                     `json:"sandbox_mode"` // read-only workspace mount
	Timeout      time.Duration            `json:"timeout"`      // default 10 min
	MemoryLimit  string                   `json:"memory_limit"` // e.g. "2g"
	CPULimit     string                   `json:"cpu_limit"`    // e.g. "2"
	Cache        *CacheConfig             `json:"-"`            // dependency layer cache volume (nil = no cache)
	WorkspaceFS  map[string]string        `json:"-"`            // file path → content to inject into /workspace
	WorkspaceDir string                   `json:"-"`            // host temp dir with workspace files (set by PrepareWorkspace)
	ArtifactCfg  *ArtifactCollectorConfig `json:"-"`            // paths to collect after build (nil = defaults)
}

// DefaultTimeout is the default build timeout.
const DefaultTimeout = 10 * time.Minute

// DefaultMemoryLimit is the default container memory limit.
const DefaultMemoryLimit = "2g"

// DefaultCPULimit is the default container CPU limit.
const DefaultCPULimit = "2"

// MaxRetries is the maximum number of build retry attempts.
const MaxRetries = 2

// ── BuildResult ─────────────────────────────────────────────────────────────

// BuildResult captures the outcome of a sandboxed build execution.
type BuildResult struct {
	ExitCode   int           `json:"exit_code"`
	Logs       string        `json:"logs"`
	Duration   time.Duration `json:"duration"`
	SecretRefs []string      `json:"secret_refs"` // names of secrets that were injected
}

// Success returns true if the build exited with code 0.
func (r *BuildResult) Success() bool {
	return r.ExitCode == 0
}

// ── ContainerRuntime ────────────────────────────────────────────────────────

// ContainerRuntime abstracts the container lifecycle so that Docker, Firecracker
// microVMs, and Kubernetes pods can be swapped in transparently.
//
// Design principle: secrets are injected ONLY at container creation time as
// environment variables.  They exist only in the container process memory and
// are destroyed when the container exits.  The runtime MUST NOT persist secret
// values to logs, disk, or any external system.
type ContainerRuntime interface {
	// Create provisions a container from the build plan and returns a
	// container ID.  Secrets are injected as env vars at this point.
	Create(ctx context.Context, plan *BuildPlan) (string, error)

	// Start begins execution of the container.
	Start(ctx context.Context, containerID string) error

	// Wait blocks until the container exits or the timeout is reached.
	Wait(ctx context.Context, containerID string, timeout time.Duration) (*BuildResult, error)

	// StreamLogs returns a reader for live stdout/stderr from the container.
	StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error)

	// Destroy forcefully removes the container and cleans up resources.
	Destroy(ctx context.Context, containerID string) error

	// HealthCheck verifies the runtime backend is available.
	HealthCheck(ctx context.Context) error
}

// ── Errors ──────────────────────────────────────────────────────────────────

// ErrBuildTimeout is returned when a build exceeds its timeout.
var ErrBuildTimeout = fmt.Errorf("sandbox: build timed out")

// ErrRuntimeUnavailable is returned when the container runtime is not healthy.
var ErrRuntimeUnavailable = fmt.Errorf("sandbox: container runtime unavailable")
