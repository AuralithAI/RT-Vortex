package sandbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/google/uuid"
)

// ── MockRuntime lifecycle ───────────────────────────────────────────────────

func TestMockRuntime_FullLifecycle(t *testing.T) {
	rt := sandbox.NewMockRuntime()
	ctx := context.Background()

	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		TaskID:      uuid.New(),
		RepoID:      "test-repo",
		BuildSystem: "go",
		Command:     "go build ./...",
		BaseImage:   "golang:1.22",
		MemoryLimit: sandbox.DefaultMemoryLimit,
		CPULimit:    sandbox.DefaultCPULimit,
		Timeout:     30 * time.Second,
		EnvVars:     map[string]string{"GOPROXY": "off"},
		SecretRefs:  []string{"GOPROXY"},
	}

	if err := rt.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}

	cid, err := rt.Create(ctx, plan)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cid != plan.ID.String() {
		t.Errorf("Create returned %q, want %q", cid, plan.ID.String())
	}
	if len(rt.CreatedPlans) != 1 {
		t.Fatalf("expected 1 created plan, got %d", len(rt.CreatedPlans))
	}

	if err := rt.Start(ctx, cid); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(rt.StartedIDs) != 1 || rt.StartedIDs[0] != cid {
		t.Errorf("StartedIDs = %v, want [%s]", rt.StartedIDs, cid)
	}

	result, err := rt.Wait(ctx, cid, plan.Timeout)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !result.Success() {
		t.Error("Success() = false, want true")
	}

	if err := rt.Destroy(ctx, cid); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(rt.DestroyedIDs) != 1 || rt.DestroyedIDs[0] != cid {
		t.Errorf("DestroyedIDs = %v, want [%s]", rt.DestroyedIDs, cid)
	}
}

func TestMockRuntime_CreateError(t *testing.T) {
	rt := sandbox.NewMockRuntime()
	rt.CreateErr = sandbox.ErrRuntimeUnavailable

	_, err := rt.Create(context.Background(), &sandbox.BuildPlan{ID: uuid.New()})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMockRuntime_WaitError(t *testing.T) {
	rt := sandbox.NewMockRuntime()
	rt.WaitErr = sandbox.ErrBuildTimeout

	_, err := rt.Wait(context.Background(), "abc", time.Minute)
	if err != sandbox.ErrBuildTimeout {
		t.Errorf("Wait error = %v, want ErrBuildTimeout", err)
	}
}

// ── DockerRuntime unit tests (no Docker required) ───────────────────────────

func TestDockerRuntime_CreateValidation(t *testing.T) {
	rt := sandbox.NewDockerRuntime(nil)
	ctx := context.Background()

	// Missing image.
	_, err := rt.Create(ctx, &sandbox.BuildPlan{
		ID:      uuid.New(),
		Command: "make",
	})
	if err == nil {
		t.Error("expected error for missing base image")
	}

	// Missing command.
	_, err = rt.Create(ctx, &sandbox.BuildPlan{
		ID:        uuid.New(),
		BaseImage: "ubuntu:22.04",
	})
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestDockerRuntime_CreateStoresPlan(t *testing.T) {
	rt := sandbox.NewDockerRuntime(nil)
	ctx := context.Background()

	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BaseImage:   "golang:1.22",
		Command:     "go build",
		MemoryLimit: "1g",
		CPULimit:    "1",
	}

	cid, err := rt.Create(ctx, plan)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	stored := rt.GetPlan(cid)
	if stored == nil {
		t.Fatal("plan not stored after Create")
	}
	if stored.BaseImage != "golang:1.22" {
		t.Errorf("stored image = %q, want golang:1.22", stored.BaseImage)
	}
}

func TestDockerRuntime_WaitUnknownContainer(t *testing.T) {
	rt := sandbox.NewDockerRuntime(nil)
	_, err := rt.Wait(context.Background(), "nonexistent", time.Minute)
	if err == nil {
		t.Error("expected error for unknown container")
	}
}

func TestDockerRuntime_BuildDockerArgs(t *testing.T) {
	rt := sandbox.NewDockerRuntime(nil)
	planID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

	plan := &sandbox.BuildPlan{
		ID:          planID,
		BaseImage:   "node:20",
		Command:     "npm test",
		PreCommands: []string{"npm ci"},
		MemoryLimit: "4g",
		CPULimit:    "2",
		SandboxMode: true,
		EnvVars:     map[string]string{"NPM_TOKEN": "secret123"},
		SecretRefs:  []string{"NPM_TOKEN"},
	}

	args, redacted := rt.BuildDockerArgs(plan)

	// Check container name includes plan ID prefix.
	found := false
	for _, a := range args {
		if a == "rtvortex-build-aaaaaaaa" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected container name with plan ID prefix in args")
	}

	// Check security flags are present.
	assertContains(t, args, "--network", "none")
	assertContains(t, args, "--user", "1000:1000")
	assertContains(t, args, "--security-opt", "no-new-privileges:true")
	assertContains(t, args, "--memory", "4g")
	assertContains(t, args, "--cpus", "2")
	assertContains(t, args, "--read-only")

	// Check secret is in real args but redacted in logged args.
	secretInArgs := false
	for _, a := range args {
		if a == "NPM_TOKEN=secret123" {
			secretInArgs = true
			break
		}
	}
	if !secretInArgs {
		t.Error("secret value not found in real args")
	}

	secretInRedacted := false
	for _, a := range redacted {
		if a == "NPM_TOKEN=secret123" {
			secretInRedacted = true
			break
		}
	}
	if secretInRedacted {
		t.Error("secret value LEAKED into redacted args — must be replaced with ***")
	}

	// Check pre-commands are chained.
	lastArg := args[len(args)-1]
	if lastArg != "npm ci && npm test" {
		t.Errorf("final command = %q, want %q", lastArg, "npm ci && npm test")
	}
}

func TestDockerRuntime_DestroyCleansPlan(t *testing.T) {
	rt := sandbox.NewDockerRuntime(nil)
	ctx := context.Background()

	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BaseImage:   "alpine",
		Command:     "echo ok",
		MemoryLimit: "512m",
		CPULimit:    "1",
	}

	cid, _ := rt.Create(ctx, plan)

	if rt.PlanCount() != 1 {
		t.Fatal("plan should exist after Create")
	}

	_ = rt.Destroy(ctx, cid)

	if rt.PlanCount() != 0 {
		t.Error("plan should be removed after Destroy")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func assertContains(t *testing.T, args []string, values ...string) {
	t.Helper()
	for i, a := range args {
		if a == values[0] {
			if len(values) == 1 {
				return
			}
			if i+1 < len(args) && args[i+1] == values[1] {
				return
			}
		}
	}
	t.Errorf("args %v missing %v", args, values)
}
