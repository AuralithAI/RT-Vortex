package sandbox

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Plan Generator ──────────────────────────────────────────────────────────

// PlanOptions configures build plan generation.
type PlanOptions struct {
	TaskID       uuid.UUID
	RepoID       string
	RepoFiles    []string      // list of files in the repository root
	ChangedFiles []string      // list of files modified by the diffs
	SecretNames  []string      // secret names available for this repo
	SandboxMode  bool          // true = read-only workspace
	Timeout      time.Duration // 0 = use DefaultTimeout
	MemoryLimit  string        // "" = use DefaultMemoryLimit
	CPULimit     string        // "" = use DefaultCPULimit
}

// GeneratePlan creates a BuildPlan from repository analysis.
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

	if plan.Timeout == 0 {
		plan.Timeout = DefaultTimeout
	}
	if plan.MemoryLimit == "" {
		plan.MemoryLimit = DefaultMemoryLimit
	}
	if plan.CPULimit == "" {
		plan.CPULimit = DefaultCPULimit
	}

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

// ── Environment Probe ───────────────────────────────────────────────────────

// ProbeOptions configures the pre-build environment probe.
type ProbeOptions struct {
	TaskID       uuid.UUID
	RepoID       string
	RepoFiles    []string // all files in the repo
	ChangedFiles []string // files modified by the diffs
	SecretNames  []string // secret names the user has stored for this repo
	FileContents map[string]string // filename → content for env-var scanning
}

// ProbeResult is the output of the pre-build environment probe.
type ProbeResult struct {
	BuildSystem    string            `json:"build_system"`
	BuildCommand   string            `json:"build_command"`
	BaseImage      string            `json:"base_image"`
	DetectedEnvs   []DetectedEnvVar  `json:"detected_envs"`
	MatchedSecrets []string          `json:"matched_secrets"`
	MissingSecrets []string          `json:"missing_secrets"`
	WellKnownEnvs  map[string]string `json:"well_known_envs"`
	Recommendations []string         `json:"recommendations"`
	Ready          bool              `json:"ready"`
}

// DetectedEnvVar records an env-var reference found in source code.
type DetectedEnvVar struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Kind   string `json:"kind"` // "explicit", "dockerfile", "cmake"
}

// wellKnownDefaults are env vars with safe default values that don't require secrets.
var wellKnownDefaults = map[string]string{
	"JAVA_HOME":        "/usr/lib/jvm/java-17",
	"CMAKE_PREFIX_PATH": "/usr/local",
	"GOPATH":           "/go",
	"GOROOT":           "/usr/local/go",
	"PYTHONPATH":       "",
	"NODE_PATH":        "",
	"PATH":             "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	"HOME":             "/home/builder",
	"LANG":             "C.UTF-8",
	"CI":               "true",
}

// envScanPatterns maps file extensions to env-var extraction patterns.
var envScanPatterns = map[string][]string{
	".py":          {"os.getenv(", "os.environ[", "os.environ.get("},
	".js":          {"process.env."},
	".ts":          {"process.env."},
	".java":        {"System.getenv(", "System.getProperty("},
	".go":          {"os.Getenv(", "viper.Get("},
	".c":           {"getenv("},
	".cpp":         {"getenv(", "std::getenv("},
	".h":           {"getenv("},
	".rs":          {"env::var(", "env::var_os("},
	".rb":          {"ENV[", "ENV.fetch("},
	"Dockerfile":   {"ENV ", "ARG "},
	"docker-compose.yml": {"environment:"},
	"docker-compose.yaml": {"environment:"},
	"CMakeLists.txt": {"$ENV{", "set(CMAKE_"},
	".env.example":  {"="},
}

// RunProbe performs a pre-build environment probe: detects the build system,
// scans file contents for env-var references, and cross-references with
// available secrets.
func RunProbe(ctx context.Context, opts ProbeOptions) *ProbeResult {
	result := &ProbeResult{
		DetectedEnvs:   make([]DetectedEnvVar, 0),
		MatchedSecrets: make([]string, 0),
		MissingSecrets: make([]string, 0),
		WellKnownEnvs:  make(map[string]string),
		Recommendations: make([]string, 0),
	}

	// Detect build system.
	bs := DetectBuildSystem(opts.RepoFiles)
	if bs != nil {
		result.BuildSystem = bs.Name
		result.BuildCommand = bs.DefaultCommand
		result.BaseImage = bs.DefaultImage
	} else {
		result.BuildSystem = "unknown"
		result.BuildCommand = "echo 'No build system detected'"
		result.BaseImage = "rtvortex/builder-general:latest"
		result.Recommendations = append(result.Recommendations,
			"No build system detected. Add a Makefile, build.gradle, go.mod, or SANDBOX.md to define the build.")
	}

	// Scan file contents for env-var references.
	envSeen := make(map[string]struct{})
	for filename, content := range opts.FileContents {
		ext := path.Ext(filename)
		base := path.Base(filename)

		patterns, ok := envScanPatterns[ext]
		if !ok {
			patterns = envScanPatterns[base]
		}
		if len(patterns) == 0 {
			continue
		}

		lines := strings.Split(content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			for _, pattern := range patterns {
				idx := strings.Index(trimmed, pattern)
				if idx < 0 {
					continue
				}
				envName := extractEnvName(trimmed, idx+len(pattern), base)
				if envName == "" {
					continue
				}
				if _, seen := envSeen[envName]; seen {
					continue
				}
				envSeen[envName] = struct{}{}

				kind := "explicit"
				if base == "Dockerfile" {
					kind = "dockerfile"
				} else if base == "CMakeLists.txt" {
					kind = "cmake"
				} else if strings.HasSuffix(base, ".env.example") {
					kind = "dotenv"
				}

				result.DetectedEnvs = append(result.DetectedEnvs, DetectedEnvVar{
					Name: envName,
					File: filename,
					Kind: kind,
				})
			}
		}
	}

	// Cross-reference detected env vars with available secrets.
	secretSet := make(map[string]struct{}, len(opts.SecretNames))
	for _, s := range opts.SecretNames {
		secretSet[strings.ToUpper(s)] = struct{}{}
	}

	for _, ev := range result.DetectedEnvs {
		upper := strings.ToUpper(ev.Name)
		if _, ok := secretSet[upper]; ok {
			result.MatchedSecrets = append(result.MatchedSecrets, ev.Name)
		} else if _, ok := wellKnownDefaults[upper]; ok {
			result.WellKnownEnvs[ev.Name] = wellKnownDefaults[upper]
		} else {
			result.MissingSecrets = append(result.MissingSecrets, ev.Name)
		}
	}

	// Determine readiness.
	result.Ready = len(result.MissingSecrets) == 0 && result.BuildSystem != "unknown"

	if len(result.MissingSecrets) > 0 {
		result.Recommendations = append(result.Recommendations,
			"Missing secrets detected. Add them via the Build Secrets UI or the build will run without these values.")
	}

	if AffectsBuildSystem(opts.ChangedFiles) {
		result.Recommendations = append(result.Recommendations,
			"Build configuration files were modified. Full build validation is recommended.")
	}

	return result
}

// extractEnvName pulls the env-var name from a source line starting after a pattern match.
func extractEnvName(line string, offset int, basename string) string {
	if offset >= len(line) {
		return ""
	}

	rest := line[offset:]

	// Dockerfile: "ENV FOO=bar" or "ARG FOO=bar" — name is the first token.
	if basename == "Dockerfile" {
		rest = strings.TrimSpace(rest)
		name := extractToken(rest)
		if eqIdx := strings.Index(name, "="); eqIdx > 0 {
			name = name[:eqIdx]
		}
		return sanitiseEnvName(name)
	}

	// .env.example: "FOO_BAR=value" — name is everything before '='.
	if strings.HasSuffix(basename, ".env.example") {
		// We matched '=' so the name is the text before offset.
		before := strings.TrimSpace(line[:offset])
		return sanitiseEnvName(before)
	}

	// Strip leading quote or bracket: getenv("FOO"), os.environ["FOO"], ENV["FOO"]
	rest = strings.TrimLeft(rest, `"'[`)
	name := extractToken(rest)
	return sanitiseEnvName(name)
}

// extractToken reads an identifier-like token (A-Z, a-z, 0-9, _) from the start of s.
func extractToken(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteRune(ch)
		} else {
			break
		}
	}
	return b.String()
}

// sanitiseEnvName returns a cleaned env-var name, or "" if it's too short or suspicious.
func sanitiseEnvName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || len(s) > 128 {
		return ""
	}
	// Must start with a letter or underscore.
	if ch := s[0]; !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_') {
		return ""
	}
	return s
}
