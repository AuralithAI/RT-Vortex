package sandbox_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
)

// ── CanSkipBuild ────────────────────────────────────────────────────────────

func TestCanSkipBuild_DocsOnly(t *testing.T) {
	files := []string{"README.md", "docs/guide.md", "CHANGELOG.md"}
	if !sandbox.CanSkipBuild(files) {
		t.Error("expected skip for docs-only changes")
	}
}

func TestCanSkipBuild_ImagesOnly(t *testing.T) {
	files := []string{"assets/logo.png", "screenshots/demo.jpg", "img/icon.svg"}
	if !sandbox.CanSkipBuild(files) {
		t.Error("expected skip for image-only changes")
	}
}

func TestCanSkipBuild_MixedWithCode(t *testing.T) {
	files := []string{"README.md", "src/main.go"}
	if sandbox.CanSkipBuild(files) {
		t.Error("should NOT skip when code files are present")
	}
}

func TestCanSkipBuild_BuildFile(t *testing.T) {
	files := []string{"README.md", "go.mod"}
	if sandbox.CanSkipBuild(files) {
		t.Error("should NOT skip when build files are present (go.mod)")
	}
}

func TestCanSkipBuild_PackageJSON(t *testing.T) {
	files := []string{"package.json"}
	if sandbox.CanSkipBuild(files) {
		t.Error("should NOT skip — package.json is always-build")
	}
}

func TestCanSkipBuild_Empty(t *testing.T) {
	if sandbox.CanSkipBuild(nil) {
		t.Error("should NOT skip empty list")
	}
	if sandbox.CanSkipBuild([]string{}) {
		t.Error("should NOT skip empty list")
	}
}

func TestCanSkipBuild_GitHubDir(t *testing.T) {
	files := []string{".github/workflows/ci.yml", ".github/FUNDING.yml"}
	if !sandbox.CanSkipBuild(files) {
		t.Error("expected skip for .github-only changes")
	}
}

func TestCanSkipBuild_License(t *testing.T) {
	if !sandbox.CanSkipBuild([]string{"LICENSE"}) {
		t.Error("expected skip for LICENSE")
	}
}

func TestCanSkipBuild_Dockerfile(t *testing.T) {
	files := []string{"Dockerfile"}
	if sandbox.CanSkipBuild(files) {
		t.Error("should NOT skip — Dockerfile is always-build")
	}
}

func TestCanSkipBuild_PythonSource(t *testing.T) {
	files := []string{"src/app.py"}
	if sandbox.CanSkipBuild(files) {
		t.Error("should NOT skip .py files")
	}
}

func TestSkipReason_NonEmpty(t *testing.T) {
	reason := sandbox.SkipReason([]string{"README.md"})
	if reason == "" {
		t.Error("expected non-empty skip reason")
	}
}

func TestSkipReason_Empty(t *testing.T) {
	reason := sandbox.SkipReason([]string{"main.go"})
	if reason != "" {
		t.Errorf("expected empty skip reason, got %q", reason)
	}
}

// ── ResolveCacheConfig ──────────────────────────────────────────────────────

func TestResolveCacheConfig_Go(t *testing.T) {
	cfg := sandbox.ResolveCacheConfig("repo-123", "go")
	if cfg == nil {
		t.Fatal("expected cache config for go")
	}
	if cfg.ContainerPath != "/go/pkg/mod" {
		t.Errorf("ContainerPath = %q, want /go/pkg/mod", cfg.ContainerPath)
	}
	if cfg.VolumeName == "" {
		t.Error("expected non-empty volume name")
	}
}

func TestResolveCacheConfig_Node(t *testing.T) {
	cfg := sandbox.ResolveCacheConfig("repo-456", "node")
	if cfg == nil {
		t.Fatal("expected cache config for node")
	}
	if cfg.ContainerPath != "/home/builder/.npm" {
		t.Errorf("ContainerPath = %q, want /home/builder/.npm", cfg.ContainerPath)
	}
}

func TestResolveCacheConfig_Python(t *testing.T) {
	cfg := sandbox.ResolveCacheConfig("repo-789", "python")
	if cfg == nil {
		t.Fatal("expected cache config for python")
	}
	if cfg.ContainerPath != "/home/builder/.cache/pip" {
		t.Errorf("ContainerPath = %q, want /home/builder/.cache/pip", cfg.ContainerPath)
	}
}

func TestResolveCacheConfig_Unknown(t *testing.T) {
	cfg := sandbox.ResolveCacheConfig("repo-000", "unknown")
	if cfg != nil {
		t.Error("expected nil cache config for unknown build system")
	}
}

func TestResolveCacheConfig_Make(t *testing.T) {
	cfg := sandbox.ResolveCacheConfig("repo-000", "make")
	if cfg != nil {
		t.Error("expected nil cache config for make")
	}
}

func TestResolveCacheConfig_Deterministic(t *testing.T) {
	a := sandbox.ResolveCacheConfig("repo-123", "go")
	b := sandbox.ResolveCacheConfig("repo-123", "go")
	if a.VolumeName != b.VolumeName {
		t.Errorf("cache volume names differ: %q vs %q", a.VolumeName, b.VolumeName)
	}
}

func TestResolveCacheConfig_DifferentRepos(t *testing.T) {
	a := sandbox.ResolveCacheConfig("repo-aaa", "go")
	b := sandbox.ResolveCacheConfig("repo-bbb", "go")
	if a.VolumeName == b.VolumeName {
		t.Error("different repos should produce different volume names")
	}
}

func TestCacheDockerArgs_Nil(t *testing.T) {
	args := sandbox.CacheDockerArgs(nil)
	if len(args) != 0 {
		t.Errorf("expected empty args for nil config, got %v", args)
	}
}

func TestCacheDockerArgs_Valid(t *testing.T) {
	cfg := sandbox.ResolveCacheConfig("repo-123", "go")
	args := sandbox.CacheDockerArgs(cfg)
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "-v" {
		t.Errorf("args[0] = %q, want -v", args[0])
	}
}
