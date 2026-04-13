package sandbox

import (
	"path"
	"strings"
)

// skipExtensions are file extensions that never require a build validation.
var skipExtensions = map[string]struct{}{
	".md":            {},
	".txt":           {},
	".rst":           {},
	".adoc":          {},
	".png":           {},
	".jpg":           {},
	".jpeg":          {},
	".gif":           {},
	".svg":           {},
	".ico":           {},
	".webp":          {},
	".bmp":           {},
	".pdf":           {},
	".doc":           {},
	".docx":          {},
	".csv":           {},
	".json":          {},
	".yaml":          {},
	".yml":           {},
	".toml":          {},
	".ini":           {},
	".cfg":           {},
	".env":           {},
	".gitignore":     {},
	".gitattributes": {},
	".editorconfig":  {},
	".prettierrc":    {},
	".eslintignore":  {},
	".dockerignore":  {},
	".mailmap":       {},
}

// skipBasenames are exact filenames that never require a build.
var skipBasenames = map[string]struct{}{
	"LICENSE":             {},
	"LICENSE.md":          {},
	"LICENSE.txt":         {},
	"LICENCE":             {},
	"LICENCE.md":          {},
	"CHANGELOG.md":        {},
	"CHANGELOG":           {},
	"CONTRIBUTING.md":     {},
	"CODE_OF_CONDUCT.md":  {},
	"CODEOWNERS":          {},
	"AUTHORS":             {},
	"AUTHORS.md":          {},
	"SECURITY.md":         {},
	"FUNDING.yml":         {},
	".github/FUNDING.yml": {},
	"NOTICE":              {},
	"NOTICE.md":           {},
}

// skipPrefixes are directory prefixes whose files never require a build.
var skipPrefixes = []string{
	"docs/",
	"doc/",
	".github/",
	".vscode/",
	".idea/",
	"assets/",
	"images/",
	"img/",
	"screenshots/",
}

// alwaysBuildBasenames are files that always require a build even if the
// extension would normally be skippable (e.g. package.json is .json but
// affects the build).
var alwaysBuildBasenames = map[string]struct{}{
	"package.json":        {},
	"package-lock.json":   {},
	"tsconfig.json":       {},
	"tsconfig.build.json": {},
	"composer.json":       {},
	"Pipfile":             {},
	"Pipfile.lock":        {},
	"pyproject.toml":      {},
	"setup.cfg":           {},
	"Cargo.toml":          {},
	"Cargo.lock":          {},
	"go.mod":              {},
	"go.sum":              {},
	"pom.xml":             {},
	"build.gradle":        {},
	"build.gradle.kts":    {},
	"settings.gradle":     {},
	"settings.gradle.kts": {},
	"CMakeLists.txt":      {},
	"Makefile":            {},
	"Dockerfile":          {},
	"docker-compose.yml":  {},
	"docker-compose.yaml": {},
	"meson.build":         {},
	"BUILD.bazel":         {},
	"WORKSPACE":           {},
	"Gemfile":             {},
	"Gemfile.lock":        {},
	"requirements.txt":    {},
	"SANDBOX.md":          {},
	"BUILD.md":            {},
	".rtvortex/build.yml": {},
}

// CanSkipBuild returns true if ALL changed files are non-code files
// (documentation, images, metadata) that cannot affect a build.
// An empty list is NOT skippable (we err on the side of building).
func CanSkipBuild(changedFiles []string) bool {
	if len(changedFiles) == 0 {
		return false
	}

	for _, fp := range changedFiles {
		if !isSkippableFile(fp) {
			return false
		}
	}
	return true
}

// SkipReason returns a human-readable reason why the build was skipped,
// or "" if it should not be skipped.
func SkipReason(changedFiles []string) string {
	if !CanSkipBuild(changedFiles) {
		return ""
	}
	return "all changed files are non-code (docs, images, or metadata) — build skipped"
}

func isSkippableFile(fp string) bool {
	basename := path.Base(fp)

	// Check always-build overrides first.
	if _, ok := alwaysBuildBasenames[basename]; ok {
		return false
	}
	// Also check full path for nested overrides like ".rtvortex/build.yml".
	if _, ok := alwaysBuildBasenames[fp]; ok {
		return false
	}

	// Check exact basenames.
	if _, ok := skipBasenames[basename]; ok {
		return true
	}
	if _, ok := skipBasenames[fp]; ok {
		return true
	}

	// Check directory prefixes.
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(fp, prefix) {
			return true
		}
	}

	// Check extension.
	ext := strings.ToLower(path.Ext(basename))
	if ext == "" {
		return false
	}
	_, ok := skipExtensions[ext]
	return ok
}
