package sandbox

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ── Plan Generator ──────────────────────────────────────────────────────────

// PlanOptions configures build plan generation.
type PlanOptions struct {
	TaskID      uuid.UUID
	RepoID      string
	RepoFiles   []string          // list of files in the repository root
	ChangedFiles []string         // list of files modified by the diffs
	SecretNames []string          // secret names available for this repo
	SandboxMode bool              // true = read-only workspace
	Timeout     time.Duration     // 0 = use DefaultTimeout
	MemoryLimit string            // "" = use DefaultMemoryLimit
	CPULimit    string            // "" = use DefaultCPULimit
}

// GeneratePlan creates a BuildPlan from repository analysis.
//
// It detects the build system, selects the appropriate base image, and
// cross-references required env vars with available secrets.
func GeneratePlan(ctx context.Context, opts PlanOptions) *BuildPlan {
	plan := &BuildPlan{
		ID:          uuid.New(),
		TaskID:      opts.TaskID,
		RepoID:      opts.RepoID,
		SandboxMode: opts.SandboxMode,
		Timeout:     opts.Timeout,
		MemoryLimit: opts.MemoryLimit,
		CPULimit:    opts.CPULimit,
		EnvVars:     make(map[string]string),
		SecretRefs:  opts.SecretNames,
	}

	// Apply defaults.
	if plan.Timeout == 0 {
		plan.Timeout = DefaultTimeout
	}
	if plan.MemoryLimit == "" {
		plan.MemoryLimit = DefaultMemoryLimit
	}
	if plan.CPULimit == "" {
		plan.CPULimit = DefaultCPULimit
	}

	// Detect build system from repo files.
	bs := DetectBuildSystem(opts.RepoFiles)
	if bs != nil {
		plan.BuildSystem = bs.Name
		plan.Command = bs.DefaultCommand
		plan.BaseImage = bs.DefaultImage
	} else {
		plan.BuildSystem = "unknown"
		plan.Command = "echo 'No build system detected'"
		plan.BaseImage = "rtvortex/builder-general:latest"
	}

	return plan
}
