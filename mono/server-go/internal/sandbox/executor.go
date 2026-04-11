package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── DockerRuntime ───────────────────────────────────────────────────────────

// DockerRuntime implements ContainerRuntime by shelling out to the `docker`
// CLI.  This is the Day-1 implementation — much lighter than the full Docker
// SDK while still providing all the security flags we need.
//
// Lifecycle (exec-based):
//
//	Create  → validates the plan, stores it in memory, returns plan.ID as
//	          the "container ID".  No Docker call yet.
//	Start   → no-op (the actual `docker run` happens in Wait).
//	Wait    → builds the full `docker run` command with security flags,
//	          env-var injection, memory/CPU limits, and timeout context.
//	          Blocks until the container exits or the deadline fires.
//	Destroy → `docker rm -f <id>` (best-effort cleanup).
//
// Security constraints applied to every container:
//   - Non-root user (--user 1000:1000)
//   - Network disabled (--network=none)
//   - No new privileges (--security-opt=no-new-privileges:true)
//   - Read-only root FS (--read-only) when SandboxMode=true
//   - Writable /tmp via tmpfs for compilers that need scratch space
//   - Memory + CPU limits from the BuildPlan
//   - Auto-remove on exit (--rm)
//
// Secret injection:
//
//	Secrets are passed ONLY as --env flags to `docker run`.  They exist
//	only in the container process environment and are destroyed when the
//	container exits.  Secret values are NEVER written to disk, logs, or
//	any persistent storage.  The command args logged by slog deliberately
//	replace secret values with "***".
type DockerRuntime struct {
	logger *slog.Logger

	mu    sync.Mutex
	plans map[string]*BuildPlan // containerID → plan (kept until Destroy)
}

// NewDockerRuntime creates a Docker-backed container runtime.
func NewDockerRuntime(logger *slog.Logger) *DockerRuntime {
	if logger == nil {
		logger = slog.Default()
	}
	return &DockerRuntime{
		logger: logger,
		plans:  make(map[string]*BuildPlan),
	}
}

// HealthCheck verifies Docker is available by running `docker info`.
func (d *DockerRuntime) HealthCheck(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: docker not available: %v (%s)", ErrRuntimeUnavailable, err, strings.TrimSpace(string(out)))
	}
	d.logger.Info("sandbox: docker runtime healthy", "version", strings.TrimSpace(string(out)))
	return nil
}

// Create validates the build plan and stores it for later use by Wait.
// Returns plan.ID as the container ID.  No Docker calls happen here.
func (d *DockerRuntime) Create(ctx context.Context, plan *BuildPlan) (string, error) {
	if plan.BaseImage == "" {
		return "", fmt.Errorf("sandbox: base image is required")
	}
	if plan.Command == "" {
		return "", fmt.Errorf("sandbox: build command is required")
	}

	containerID := plan.ID.String()

	d.mu.Lock()
	d.plans[containerID] = plan
	d.mu.Unlock()

	d.logger.Info("sandbox: container prepared",
		"container_id", containerID,
		"image", plan.BaseImage,
		"command", plan.Command,
		"sandbox_mode", plan.SandboxMode,
		"secret_count", len(plan.EnvVars),
	)
	return containerID, nil
}

// Start is a no-op for the exec-based backend — execution happens in Wait.
func (d *DockerRuntime) Start(ctx context.Context, containerID string) error {
	return nil
}

// buildDockerArgs constructs the full `docker run` argument list from a
// BuildPlan.  This is separated for testability and for safe logging
// (the returned redactedArgs replaces secret values with "***").
func (d *DockerRuntime) buildDockerArgs(plan *BuildPlan) (args []string, redactedArgs []string) {
	args = []string{
		"run", "--rm",
		"--name", "rtvortex-build-" + plan.ID.String()[:8],

		// Security: non-root, no privilege escalation, no network.
		"--user", "1000:1000",
		"--network", "none",
		"--security-opt", "no-new-privileges:true",

		// Resource limits.
		"--memory", plan.MemoryLimit,
		"--cpus", plan.CPULimit,

		// Writable /tmp for compilers (even if root FS is read-only).
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=512m",
	}

	if plan.SandboxMode {
		args = append(args, "--read-only")
	}

	// Copy for safe logging before we add secret env vars.
	redactedArgs = make([]string, len(args))
	copy(redactedArgs, args)

	// Inject secrets as env vars — values only appear in args, NEVER in logs.
	for k, v := range plan.EnvVars {
		args = append(args, "--env", k+"="+v)
		redactedArgs = append(redactedArgs, "--env", k+"=***")
	}

	// Pre-commands: chain them with && before the actual build command.
	fullCmd := plan.Command
	if len(plan.PreCommands) > 0 {
		fullCmd = strings.Join(plan.PreCommands, " && ") + " && " + plan.Command
	}

	// Image and command.
	args = append(args, plan.BaseImage, "sh", "-c", fullCmd)
	redactedArgs = append(redactedArgs, plan.BaseImage, "sh", "-c", fullCmd)

	return args, redactedArgs
}

// Wait runs `docker run` and blocks until the container exits or the
// timeout is reached.
func (d *DockerRuntime) Wait(ctx context.Context, containerID string, timeout time.Duration) (*BuildResult, error) {
	d.mu.Lock()
	plan, ok := d.plans[containerID]
	d.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("sandbox: unknown container %q (was Create called?)", containerID)
	}

	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	args, redactedArgs := d.buildDockerArgs(plan)

	d.logger.Info("sandbox: executing docker run",
		"container_id", containerID,
		"args", redactedArgs,
		"timeout", timeout.String(),
	)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, "docker", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	// Merge stdout+stderr for the log (stderr often has compiler warnings).
	combinedLogs := stdout.String()
	if stderr.Len() > 0 {
		combinedLogs += "\n--- stderr ---\n" + stderr.String()
	}

	// Truncate logs to 64KB to avoid blowing up the DB / JSON responses.
	const maxLogBytes = 64 * 1024
	if len(combinedLogs) > maxLogBytes {
		combinedLogs = combinedLogs[:maxLogBytes] + "\n... [truncated]"
	}

	result := &BuildResult{
		Logs:       combinedLogs,
		Duration:   duration,
		SecretRefs: plan.SecretRefs,
	}

	if runCtx.Err() == context.DeadlineExceeded {
		result.ExitCode = -1
		result.Logs += "\n\n⏰ Build timed out after " + timeout.String()
		d.logger.Warn("sandbox: build timed out",
			"container_id", containerID,
			"timeout", timeout.String(),
		)
		return result, ErrBuildTimeout
	}

	if err != nil {
		// Try to extract the real exit code from the ExitError.
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		d.logger.Info("sandbox: build finished with error",
			"container_id", containerID,
			"exit_code", result.ExitCode,
			"duration", duration.String(),
		)
	} else {
		result.ExitCode = 0
		d.logger.Info("sandbox: build succeeded",
			"container_id", containerID,
			"duration", duration.String(),
		)
	}

	return result, nil
}

// StreamLogs attaches to a running container's stdout/stderr.
// For the exec-based backend, logs are captured in Wait() instead.
// This method is here for future Docker SDK / K8s backends.
func (d *DockerRuntime) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", containerID)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to attach logs: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: failed to start log stream: %w", err)
	}
	return pipe, nil
}

// Destroy forcefully removes a container and cleans up the stored plan.
func (d *DockerRuntime) Destroy(ctx context.Context, containerID string) error {
	// Remove the plan from memory.
	d.mu.Lock()
	delete(d.plans, containerID)
	d.mu.Unlock()

	// Best-effort container removal (may already be gone due to --rm).
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", "rtvortex-build-"+containerID[:8])
	_ = cmd.Run()
	return nil
}

// ── MockRuntime ─────────────────────────────────────────────────────────────

// MockRuntime implements ContainerRuntime for testing without Docker.
// It records all calls and returns configurable results.
type MockRuntime struct {
	HealthErr  error
	CreateErr  error
	StartErr   error
	WaitResult *BuildResult
	WaitErr    error
	DestroyErr error

	// Recorded calls for assertions.
	CreatedPlans []*BuildPlan
	StartedIDs   []string
	DestroyedIDs []string
}

// NewMockRuntime creates a mock runtime that returns success by default.
func NewMockRuntime() *MockRuntime {
	return &MockRuntime{
		WaitResult: &BuildResult{
			ExitCode: 0,
			Logs:     "BUILD SUCCESSFUL",
			Duration: 2 * time.Second,
		},
	}
}

func (m *MockRuntime) HealthCheck(ctx context.Context) error {
	return m.HealthErr
}

func (m *MockRuntime) Create(ctx context.Context, plan *BuildPlan) (string, error) {
	if m.CreateErr != nil {
		return "", m.CreateErr
	}
	m.CreatedPlans = append(m.CreatedPlans, plan)
	return plan.ID.String(), nil
}

func (m *MockRuntime) Start(ctx context.Context, containerID string) error {
	m.StartedIDs = append(m.StartedIDs, containerID)
	return m.StartErr
}

func (m *MockRuntime) Wait(ctx context.Context, containerID string, timeout time.Duration) (*BuildResult, error) {
	if m.WaitErr != nil {
		return nil, m.WaitErr
	}
	return m.WaitResult, nil
}

func (m *MockRuntime) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("mock: not implemented")
}

func (m *MockRuntime) Destroy(ctx context.Context, containerID string) error {
	m.DestroyedIDs = append(m.DestroyedIDs, containerID)
	return m.DestroyErr
}

// Compile-time interface checks.
var _ ContainerRuntime = (*DockerRuntime)(nil)
var _ ContainerRuntime = (*MockRuntime)(nil)
