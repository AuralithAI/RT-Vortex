package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// BuildFingerprint captures a content-addressable hash of a repo's build
// configuration files.  When the fingerprint is unchanged between builds,
// the dependency-install step can be skipped (fast-path execution).
type BuildFingerprint struct {
	Hash        string   `json:"hash"`
	BuildSystem string   `json:"build_system"`
	Files       []string `json:"files"`
	FastPath    bool     `json:"fast_path"`
}

// buildConfigFiles maps build systems to the files that define the
// dependency graph.  Changes to these files invalidate the fast path.
var buildConfigFiles = map[string][]string{
	"go":     {"go.mod", "go.sum"},
	"node":   {"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml"},
	"python": {"requirements.txt", "pyproject.toml", "setup.py", "setup.cfg", "Pipfile", "Pipfile.lock", "poetry.lock"},
	"gradle": {"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties"},
	"maven":  {"pom.xml"},
	"rust":   {"Cargo.toml", "Cargo.lock"},
	"cmake":  {"CMakeLists.txt", "conanfile.txt", "conanfile.py", "vcpkg.json"},
	"make":   {"Makefile"},
}

// ComputeBuildFingerprint hashes the content of build-config files from the
// workspace to produce a deterministic fingerprint.  If the workspace does
// not contain any recognised config files, Hash is empty and FastPath is
// false.
func ComputeBuildFingerprint(buildSystem string, workspace map[string]string) *BuildFingerprint {
	fp := &BuildFingerprint{BuildSystem: buildSystem}

	configNames := buildConfigFiles[strings.ToLower(buildSystem)]
	if len(configNames) == 0 {
		return fp
	}

	var matched []string
	for _, name := range configNames {
		for wsPath := range workspace {
			base := baseName(wsPath)
			if strings.EqualFold(base, name) {
				matched = append(matched, wsPath)
			}
		}
	}

	if len(matched) == 0 {
		return fp
	}

	sort.Strings(matched)
	fp.Files = matched

	h := sha256.New()
	h.Write([]byte(buildSystem))
	h.Write([]byte{0})
	for _, path := range matched {
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write([]byte(workspace[path]))
		h.Write([]byte{0})
	}

	fp.Hash = hex.EncodeToString(h.Sum(nil))
	return fp
}

// IsFastPath returns true when the current fingerprint matches a previous
// one, meaning dependency files are unchanged and only source code was
// modified.  The caller can skip the dep-install phase.
func IsFastPath(current, previous *BuildFingerprint) bool {
	if current == nil || previous == nil {
		return false
	}
	if current.Hash == "" || previous.Hash == "" {
		return false
	}
	return current.Hash == previous.Hash && current.BuildSystem == previous.BuildSystem
}

// AnnotateFastPath sets the FastPath flag on the fingerprint by comparing
// it to the previous build's fingerprint for the same repo.
func AnnotateFastPath(current, previous *BuildFingerprint) {
	if current != nil {
		current.FastPath = IsFastPath(current, previous)
	}
}

// FastPathCommand returns a modified build command that skips dependency
// installation when the fast path is active.  If fastPath is false the
// original command is returned unchanged.
func FastPathCommand(buildSystem, command string, fastPath bool) string {
	if !fastPath {
		return command
	}

	switch strings.ToLower(buildSystem) {
	case "go":
		// go build already caches — no change needed.
		return command
	case "node":
		return rewriteNodeFastPath(command)
	case "python":
		return rewritePythonFastPath(command)
	case "gradle":
		if !strings.Contains(command, "--no-rebuild") {
			return command + " --build-cache"
		}
		return command
	case "maven":
		if !strings.Contains(command, "-o") {
			return command + " -o"
		}
		return command
	case "rust":
		return command
	default:
		return command
	}
}

// rewriteNodeFastPath replaces `npm ci && npm run build` with just
// `npm run build` when deps are unchanged.
func rewriteNodeFastPath(command string) string {
	parts := strings.Split(command, "&&")
	var kept []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if isDepInstallCommand(trimmed) {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return command
	}
	return strings.Join(kept, "&&")
}

// rewritePythonFastPath removes pip install steps.
func rewritePythonFastPath(command string) string {
	parts := strings.Split(command, "&&")
	var kept []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if isDepInstallCommand(trimmed) {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return command
	}
	return strings.Join(kept, "&&")
}

// isDepInstallCommand returns true if the command is a dependency install
// step that can be skipped on the fast path.
func isDepInstallCommand(cmd string) bool {
	depPrefixes := []string{
		"npm ci",
		"npm install",
		"yarn install",
		"yarn --frozen-lockfile",
		"pnpm install",
		"pip install",
		"pip3 install",
		"python -m pip install",
		"python3 -m pip install",
		"bundle install",
		"composer install",
	}
	lower := strings.ToLower(cmd)
	for _, prefix := range depPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// ImageTag generates a deterministic Docker image tag for caching built
// layers of a repo's dependencies.  Format:
//
//	rtvortex/cache:<repo-hash-prefix>-<fingerprint-prefix>
func ImageTag(repoID string, fingerprint *BuildFingerprint) string {
	if fingerprint == nil || fingerprint.Hash == "" {
		return ""
	}

	repoHash := sha256.Sum256([]byte(repoID))
	repoPrefix := hex.EncodeToString(repoHash[:4])
	fpPrefix := fingerprint.Hash[:12]

	return "rtvortex/cache:" + repoPrefix + "-" + fpPrefix
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
