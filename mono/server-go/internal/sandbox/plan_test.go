package sandbox

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestGeneratePlan_Defaults(t *testing.T) {
	plan := GeneratePlan(context.Background(), PlanOptions{
		TaskID:    uuid.New(),
		RepoID:    "test-repo",
		RepoFiles: []string{"go.mod", "main.go"},
	})
	if plan.BuildSystem != "go" {
		t.Errorf("BuildSystem = %q, want %q", plan.BuildSystem, "go")
	}
	if plan.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", plan.Timeout, DefaultTimeout)
	}
	if plan.MemoryLimit != DefaultMemoryLimit {
		t.Errorf("MemoryLimit = %q, want %q", plan.MemoryLimit, DefaultMemoryLimit)
	}
}

func TestRunProbe_DetectsEnvVars(t *testing.T) {
	result := RunProbe(context.Background(), ProbeOptions{
		TaskID:    uuid.New(),
		RepoID:    "test-repo",
		RepoFiles: []string{"go.mod", "main.go", "cmd/server.go"},
		FileContents: map[string]string{
			"cmd/server.go": `package main

import "os"

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	port := os.Getenv("PORT")
	home := os.Getenv("HOME")
	_ = dbURL + port + home
}`,
		},
		SecretNames: []string{"DATABASE_URL"},
	})

	if result.BuildSystem != "go" {
		t.Errorf("BuildSystem = %q, want %q", result.BuildSystem, "go")
	}

	if len(result.DetectedEnvs) != 3 {
		t.Fatalf("detected %d env vars, want 3", len(result.DetectedEnvs))
	}

	// DATABASE_URL should be matched.
	found := false
	for _, s := range result.MatchedSecrets {
		if s == "DATABASE_URL" {
			found = true
		}
	}
	if !found {
		t.Error("DATABASE_URL should be in matched secrets")
	}

	// HOME should be well-known.
	if _, ok := result.WellKnownEnvs["HOME"]; !ok {
		t.Error("HOME should be in well-known envs")
	}

	// PORT should be missing.
	found = false
	for _, s := range result.MissingSecrets {
		if s == "PORT" {
			found = true
		}
	}
	if !found {
		t.Error("PORT should be in missing secrets")
	}

	if result.Ready {
		t.Error("should not be ready with missing secrets")
	}
}

func TestRunProbe_AllSecretsMatched(t *testing.T) {
	result := RunProbe(context.Background(), ProbeOptions{
		TaskID:    uuid.New(),
		RepoID:    "test-repo",
		RepoFiles: []string{"package.json", "src/index.js"},
		FileContents: map[string]string{
			"src/index.js": `const api = process.env.API_KEY;`,
		},
		SecretNames: []string{"API_KEY"},
	})

	if !result.Ready {
		t.Error("should be ready when all secrets are matched")
	}
	if len(result.MissingSecrets) != 0 {
		t.Errorf("expected 0 missing secrets, got %d", len(result.MissingSecrets))
	}
}

func TestRunProbe_Dockerfile(t *testing.T) {
	result := RunProbe(context.Background(), ProbeOptions{
		TaskID:    uuid.New(),
		RepoID:    "test-repo",
		RepoFiles: []string{"Dockerfile", "app.py"},
		FileContents: map[string]string{
			"Dockerfile": `FROM python:3.12
ENV APP_PORT=8080
ARG BUILD_TOKEN
RUN pip install -r requirements.txt
`,
		},
		SecretNames: []string{"BUILD_TOKEN"},
	})

	nameSet := make(map[string]struct{})
	for _, ev := range result.DetectedEnvs {
		nameSet[ev.Name] = struct{}{}
		if ev.Kind != "dockerfile" {
			t.Errorf("env %q kind = %q, want dockerfile", ev.Name, ev.Kind)
		}
	}

	if _, ok := nameSet["APP_PORT"]; !ok {
		t.Error("expected APP_PORT in detected envs")
	}
	if _, ok := nameSet["BUILD_TOKEN"]; !ok {
		t.Error("expected BUILD_TOKEN in detected envs")
	}
}

func TestRunProbe_Python(t *testing.T) {
	result := RunProbe(context.Background(), ProbeOptions{
		TaskID:    uuid.New(),
		RepoID:    "test-repo",
		RepoFiles: []string{"pyproject.toml", "app.py"},
		FileContents: map[string]string{
			"app.py": `import os
db = os.environ.get("DB_HOST")
key = os.environ["SECRET_KEY"]
`,
		},
		SecretNames: []string{},
	})

	if result.BuildSystem != "python" {
		t.Errorf("BuildSystem = %q, want python", result.BuildSystem)
	}

	if len(result.DetectedEnvs) != 2 {
		t.Errorf("detected %d envs, want 2", len(result.DetectedEnvs))
	}
}

func TestRunProbe_UnknownBuildSystem(t *testing.T) {
	result := RunProbe(context.Background(), ProbeOptions{
		TaskID:       uuid.New(),
		RepoID:       "test-repo",
		RepoFiles:    []string{"README.md"},
		FileContents: map[string]string{},
	})

	if result.BuildSystem != "unknown" {
		t.Errorf("BuildSystem = %q, want unknown", result.BuildSystem)
	}
	if result.Ready {
		t.Error("unknown build system should not be ready")
	}
	if len(result.Recommendations) == 0 {
		t.Error("should have at least one recommendation for unknown build system")
	}
}

func TestRunProbe_CaseInsensitiveSecretMatch(t *testing.T) {
	result := RunProbe(context.Background(), ProbeOptions{
		TaskID:    uuid.New(),
		RepoID:    "test-repo",
		RepoFiles: []string{"go.mod", "main.go"},
		FileContents: map[string]string{
			"main.go": `package main
import "os"
func main() { os.Getenv("my_api_key") }`,
		},
		SecretNames: []string{"MY_API_KEY"},
	})

	if len(result.MatchedSecrets) != 1 {
		t.Errorf("expected 1 matched secret, got %d", len(result.MatchedSecrets))
	}
}

func TestExtractEnvName(t *testing.T) {
	tests := []struct {
		line     string
		offset   int
		basename string
		want     string
	}{
		{`os.Getenv("FOO_BAR")`, 10, "main.go", "FOO_BAR"},
		{`os.environ["DB_URL"]`, 12, "app.py", "DB_URL"},
		{`process.env.API_KEY`, 12, "index.js", "API_KEY"},
		{`ENV MY_VAR=value`, 4, "Dockerfile", "MY_VAR"},
		{`ARG BUILD_TOKEN`, 4, "Dockerfile", "BUILD_TOKEN"},
		{`os.Getenv("")`, 10, "main.go", ""},
	}

	for _, tt := range tests {
		got := extractEnvName(tt.line, tt.offset, tt.basename)
		if got != tt.want {
			t.Errorf("extractEnvName(%q, %d, %q) = %q, want %q",
				tt.line, tt.offset, tt.basename, got, tt.want)
		}
	}
}

func TestSanitiseEnvName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"FOO_BAR", "FOO_BAR"},
		{"a", ""},       // too short
		{"123ABC", ""},  // starts with digit
		{"_PRIVATE", "_PRIVATE"},
		{"", ""},
	}

	for _, tt := range tests {
		got := sanitiseEnvName(tt.input)
		if got != tt.want {
			t.Errorf("sanitiseEnvName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
