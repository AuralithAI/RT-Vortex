package sandbox

import "testing"

func TestDetectBuildSystem_Go(t *testing.T) {
	info := DetectBuildSystem([]string{"go.mod", "main.go", "internal/server/server.go"})
	if info == nil {
		t.Fatal("expected go build system, got nil")
	}
	if info.Name != "go" {
		t.Errorf("Name = %q, want %q", info.Name, "go")
	}
	if info.DefaultCommand != "go build ./..." {
		t.Errorf("DefaultCommand = %q, want %q", info.DefaultCommand, "go build ./...")
	}
}

func TestDetectBuildSystem_GradlePriority(t *testing.T) {
	// Gradle should win over Makefile because it appears first in rules.
	info := DetectBuildSystem([]string{"build.gradle.kts", "Makefile", "src/main.java"})
	if info == nil {
		t.Fatal("expected gradle, got nil")
	}
	if info.Name != "gradle" {
		t.Errorf("Name = %q, want %q", info.Name, "gradle")
	}
}

func TestDetectBuildSystem_CustomSandbox(t *testing.T) {
	info := DetectBuildSystem([]string{"SANDBOX.md", "go.mod"})
	if info == nil {
		t.Fatal("expected custom, got nil")
	}
	if info.Name != "custom" {
		t.Errorf("Name = %q, want %q (SANDBOX.md should take priority)", info.Name, "custom")
	}
}

func TestDetectBuildSystem_Unknown(t *testing.T) {
	info := DetectBuildSystem([]string{"README.md", "LICENSE"})
	if info != nil {
		t.Errorf("expected nil for unknown project, got %+v", info)
	}
}

func TestDetectBuildSystem_NestedPath(t *testing.T) {
	// path.Base("services/api/package.json") == "package.json"
	info := DetectBuildSystem([]string{"services/api/package.json"})
	if info == nil {
		t.Fatal("expected node, got nil")
	}
	if info.Name != "node" {
		t.Errorf("Name = %q, want %q", info.Name, "node")
	}
}

func TestAffectsBuildSystem(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		affects bool
	}{
		{"go.mod change", []string{"go.mod"}, true},
		{"nested Dockerfile", []string{"deploy/Dockerfile"}, true},
		{"source only", []string{"internal/sandbox/sandbox.go"}, false},
		{"rtvortex build config", []string{".rtvortex/build.yml"}, true},
		{"requirements.txt", []string{"src/app.py", "requirements.txt"}, true},
		{"empty", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AffectsBuildSystem(tt.files)
			if got != tt.affects {
				t.Errorf("AffectsBuildSystem(%v) = %v, want %v", tt.files, got, tt.affects)
			}
		})
	}
}
