package sandbox

import (
	"path"
	"strings"
)

// ── Build System Detection ──────────────────────────────────────────────────

// BuildSystemInfo describes a detected build system.
type BuildSystemInfo struct {
	Name           string // "gradle", "maven", "cmake", "python", "node", "go", "rust", "make", "custom"
	DefaultCommand string // default build command
	DefaultImage   string // default Docker base image
}

// buildSystemRules maps filename → build system info, ordered by priority.
// The first match wins when multiple build files are present.
var buildSystemRules = []struct {
	filename string
	info     BuildSystemInfo
}{
	{"SANDBOX.md", BuildSystemInfo{"custom", "", ""}},
	{".rtvortex/build.yml", BuildSystemInfo{"custom", "", ""}},
	{"BUILD.md", BuildSystemInfo{"custom", "", ""}},
	{"build.gradle.kts", BuildSystemInfo{"gradle", "./gradlew build", "rtvortex/builder-jvm:17"}},
	{"build.gradle", BuildSystemInfo{"gradle", "./gradlew build", "rtvortex/builder-jvm:17"}},
	{"pom.xml", BuildSystemInfo{"maven", "mvn package -q", "rtvortex/builder-jvm:17"}},
	{"CMakeLists.txt", BuildSystemInfo{"cmake", "cmake --build build/", "rtvortex/builder-cpp:latest"}},
	{"Cargo.toml", BuildSystemInfo{"rust", "cargo build", "rtvortex/builder-rust:latest"}},
	{"go.mod", BuildSystemInfo{"go", "go build ./...", "rtvortex/builder-go:1.24"}},
	{"pyproject.toml", BuildSystemInfo{"python", "pip install -e . && pytest", "rtvortex/builder-python:3.12"}},
	{"setup.py", BuildSystemInfo{"python", "pip install -e . && pytest", "rtvortex/builder-python:3.12"}},
	{"package.json", BuildSystemInfo{"node", "npm ci && npm run build", "rtvortex/builder-node:20"}},
	{"Makefile", BuildSystemInfo{"make", "make", "rtvortex/builder-general:latest"}},
}

// DetectBuildSystem examines a list of file paths (relative to repo root)
// and returns the first matching build system.  Returns nil if no known
// build system is detected.
func DetectBuildSystem(filePaths []string) *BuildSystemInfo {
	// Build a set of basenames + full paths for fast lookup.
	nameSet := make(map[string]struct{}, len(filePaths))
	for _, fp := range filePaths {
		nameSet[fp] = struct{}{}
		nameSet[path.Base(fp)] = struct{}{}
	}

	for _, rule := range buildSystemRules {
		if _, ok := nameSet[rule.filename]; ok {
			info := rule.info // copy
			return &info
		}
	}
	return nil
}

// AffectsBuildSystem returns true if any of the given file paths match a
// build-related filename pattern.
func AffectsBuildSystem(changedFiles []string) bool {
	buildFiles := map[string]struct{}{
		"Makefile": {}, "CMakeLists.txt": {}, "build.gradle": {},
		"build.gradle.kts": {}, "pom.xml": {}, "pyproject.toml": {},
		"setup.py": {}, "setup.cfg": {}, "package.json": {},
		"Dockerfile": {}, "docker-compose.yml": {}, "docker-compose.yaml": {},
		"go.mod": {}, "go.sum": {}, "Cargo.toml": {}, "Cargo.lock": {},
		"meson.build": {}, "BUILD.bazel": {}, "WORKSPACE": {},
		"Gemfile": {}, "requirements.txt": {},
		"SANDBOX.md": {}, "BUILD.md": {},
	}

	for _, fp := range changedFiles {
		basename := fp
		if idx := strings.LastIndex(fp, "/"); idx >= 0 {
			basename = fp[idx+1:]
		}
		if _, ok := buildFiles[basename]; ok {
			return true
		}
		// Full-path checks.
		if fp == ".rtvortex/build.yml" {
			return true
		}
	}
	return false
}
