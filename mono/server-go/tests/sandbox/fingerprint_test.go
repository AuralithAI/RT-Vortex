package sandbox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
)

// ── ComputeBuildFingerprint ─────────────────────────────────────────────────

func TestComputeBuildFingerprint_GoModule(t *testing.T) {
	ws := map[string]string{
		"go.mod": "module example.com/app\ngo 1.22\n",
		"go.sum": "golang.org/x/text v0.15.0 h1:abc\n",
		"main.go": "package main\nfunc main() {}\n",
	}
	fp := sandbox.ComputeBuildFingerprint("go", ws)

	if fp.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if fp.BuildSystem != "go" {
		t.Errorf("BuildSystem = %q, want %q", fp.BuildSystem, "go")
	}
	if len(fp.Files) != 2 {
		t.Errorf("Files = %d, want 2 (go.mod, go.sum)", len(fp.Files))
	}
	if fp.FastPath {
		t.Error("FastPath should default to false")
	}
}

func TestComputeBuildFingerprint_NodeProject(t *testing.T) {
	ws := map[string]string{
		"package.json":      `{"name":"app","version":"1.0.0"}`,
		"package-lock.json": `{"lockfileVersion":3}`,
		"src/index.js":      "console.log('hi')",
	}
	fp := sandbox.ComputeBuildFingerprint("node", ws)

	if fp.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(fp.Files) != 2 {
		t.Errorf("Files = %d, want 2 (package.json, package-lock.json)", len(fp.Files))
	}
}

func TestComputeBuildFingerprint_PythonProject(t *testing.T) {
	ws := map[string]string{
		"requirements.txt": "flask==3.0.0\n",
		"pyproject.toml":   "[project]\nname = \"app\"\n",
		"app.py":           "print('hello')",
	}
	fp := sandbox.ComputeBuildFingerprint("python", ws)

	if fp.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(fp.Files) != 2 {
		t.Errorf("Files = %d, want 2", len(fp.Files))
	}
}

func TestComputeBuildFingerprint_UnknownBuildSystem(t *testing.T) {
	ws := map[string]string{
		"main.c": "#include <stdio.h>\n",
	}
	fp := sandbox.ComputeBuildFingerprint("unknown", ws)

	if fp.Hash != "" {
		t.Errorf("expected empty hash for unknown build system, got %q", fp.Hash)
	}
	if len(fp.Files) != 0 {
		t.Errorf("Files = %d, want 0", len(fp.Files))
	}
}

func TestComputeBuildFingerprint_EmptyWorkspace(t *testing.T) {
	fp := sandbox.ComputeBuildFingerprint("go", map[string]string{})

	if fp.Hash != "" {
		t.Errorf("expected empty hash for empty workspace, got %q", fp.Hash)
	}
}

func TestComputeBuildFingerprint_Deterministic(t *testing.T) {
	ws := map[string]string{
		"go.mod": "module example.com/app\ngo 1.22\n",
		"go.sum": "h1:abc\n",
	}

	fp1 := sandbox.ComputeBuildFingerprint("go", ws)
	fp2 := sandbox.ComputeBuildFingerprint("go", ws)

	if fp1.Hash != fp2.Hash {
		t.Errorf("hashes should be deterministic: %q != %q", fp1.Hash, fp2.Hash)
	}
}

func TestComputeBuildFingerprint_ContentChange(t *testing.T) {
	ws1 := map[string]string{"go.mod": "module a\n", "go.sum": "v1\n"}
	ws2 := map[string]string{"go.mod": "module a\n", "go.sum": "v2\n"}

	fp1 := sandbox.ComputeBuildFingerprint("go", ws1)
	fp2 := sandbox.ComputeBuildFingerprint("go", ws2)

	if fp1.Hash == fp2.Hash {
		t.Error("hashes should differ when go.sum content changes")
	}
}

func TestComputeBuildFingerprint_SubdirectoryMatch(t *testing.T) {
	ws := map[string]string{
		"frontend/package.json":      `{"name":"ui"}`,
		"frontend/package-lock.json": `{}`,
	}
	fp := sandbox.ComputeBuildFingerprint("node", ws)

	if fp.Hash == "" {
		t.Fatal("expected non-empty hash for subdirectory config files")
	}
	if len(fp.Files) != 2 {
		t.Errorf("Files = %d, want 2", len(fp.Files))
	}
}

// ── IsFastPath ──────────────────────────────────────────────────────────────

func TestIsFastPath_MatchingHashes(t *testing.T) {
	a := &sandbox.BuildFingerprint{Hash: "abc123", BuildSystem: "go"}
	b := &sandbox.BuildFingerprint{Hash: "abc123", BuildSystem: "go"}

	if !sandbox.IsFastPath(a, b) {
		t.Error("expected fast path when hashes match")
	}
}

func TestIsFastPath_DifferentHashes(t *testing.T) {
	a := &sandbox.BuildFingerprint{Hash: "abc123", BuildSystem: "go"}
	b := &sandbox.BuildFingerprint{Hash: "def456", BuildSystem: "go"}

	if sandbox.IsFastPath(a, b) {
		t.Error("should not be fast path with different hashes")
	}
}

func TestIsFastPath_DifferentBuildSystems(t *testing.T) {
	a := &sandbox.BuildFingerprint{Hash: "abc123", BuildSystem: "go"}
	b := &sandbox.BuildFingerprint{Hash: "abc123", BuildSystem: "node"}

	if sandbox.IsFastPath(a, b) {
		t.Error("should not be fast path with different build systems")
	}
}

func TestIsFastPath_NilInputs(t *testing.T) {
	a := &sandbox.BuildFingerprint{Hash: "abc", BuildSystem: "go"}

	if sandbox.IsFastPath(nil, a) {
		t.Error("should not be fast path with nil current")
	}
	if sandbox.IsFastPath(a, nil) {
		t.Error("should not be fast path with nil previous")
	}
	if sandbox.IsFastPath(nil, nil) {
		t.Error("should not be fast path with both nil")
	}
}

func TestIsFastPath_EmptyHash(t *testing.T) {
	a := &sandbox.BuildFingerprint{Hash: "", BuildSystem: "go"}
	b := &sandbox.BuildFingerprint{Hash: "abc", BuildSystem: "go"}

	if sandbox.IsFastPath(a, b) {
		t.Error("should not be fast path with empty current hash")
	}
	if sandbox.IsFastPath(b, a) {
		t.Error("should not be fast path with empty previous hash")
	}
}

// ── AnnotateFastPath ────────────────────────────────────────────────────────

func TestAnnotateFastPath_Sets(t *testing.T) {
	current := &sandbox.BuildFingerprint{Hash: "x", BuildSystem: "go"}
	previous := &sandbox.BuildFingerprint{Hash: "x", BuildSystem: "go"}

	sandbox.AnnotateFastPath(current, previous)
	if !current.FastPath {
		t.Error("FastPath should be true after annotation with matching hash")
	}
}

func TestAnnotateFastPath_Clears(t *testing.T) {
	current := &sandbox.BuildFingerprint{Hash: "x", BuildSystem: "go"}
	previous := &sandbox.BuildFingerprint{Hash: "y", BuildSystem: "go"}

	sandbox.AnnotateFastPath(current, previous)
	if current.FastPath {
		t.Error("FastPath should be false after annotation with different hash")
	}
}

func TestAnnotateFastPath_NilSafe(t *testing.T) {
	sandbox.AnnotateFastPath(nil, nil)
	sandbox.AnnotateFastPath(nil, &sandbox.BuildFingerprint{Hash: "x"})
}

// ── FastPathCommand ─────────────────────────────────────────────────────────

func TestFastPathCommand_Disabled(t *testing.T) {
	cmd := "npm ci && npm run build"
	result := sandbox.FastPathCommand("node", cmd, false)
	if result != cmd {
		t.Errorf("should return original command when fast path disabled, got %q", result)
	}
}

func TestFastPathCommand_NodeStripsInstall(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"npm ci then build", "npm ci && npm run build", " npm run build"},
		{"npm install then test", "npm install && npm test", " npm test"},
		{"yarn install then build", "yarn install && yarn build", " yarn build"},
		{"pnpm install then start", "pnpm install && pnpm start", " pnpm start"},
		{"only install", "npm ci", "npm ci"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sandbox.FastPathCommand("node", tc.cmd, true)
			if got != tc.want {
				t.Errorf("FastPathCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestFastPathCommand_PythonStripsInstall(t *testing.T) {
	cmd := "pip install -r requirements.txt && python manage.py test"
	got := sandbox.FastPathCommand("python", cmd, true)
	if strings.Contains(got, "pip install") {
		t.Errorf("should strip pip install from fast-path command: %q", got)
	}
	if !strings.Contains(got, "python manage.py test") {
		t.Errorf("should keep test command: %q", got)
	}
}

func TestFastPathCommand_GradleAddsBuildCache(t *testing.T) {
	cmd := "./gradlew build"
	got := sandbox.FastPathCommand("gradle", cmd, true)
	if !strings.Contains(got, "--build-cache") {
		t.Errorf("should add --build-cache for gradle: %q", got)
	}
}

func TestFastPathCommand_MavenAddsOffline(t *testing.T) {
	cmd := "mvn package"
	got := sandbox.FastPathCommand("maven", cmd, true)
	if !strings.Contains(got, "-o") {
		t.Errorf("should add -o for maven: %q", got)
	}
}

func TestFastPathCommand_GoUnchanged(t *testing.T) {
	cmd := "go build ./..."
	got := sandbox.FastPathCommand("go", cmd, true)
	if got != cmd {
		t.Errorf("go command should be unchanged, got %q", got)
	}
}

func TestFastPathCommand_RustUnchanged(t *testing.T) {
	cmd := "cargo build --release"
	got := sandbox.FastPathCommand("rust", cmd, true)
	if got != cmd {
		t.Errorf("rust command should be unchanged, got %q", got)
	}
}

// ── IsDepInstallCommand (via FastPathCommand) ───────────────────────────────

func TestIsDepInstallCommand_Recognised(t *testing.T) {
	cmds := []string{
		"npm ci",
		"npm install",
		"yarn install",
		"yarn --frozen-lockfile",
		"pnpm install",
		"pip install -r requirements.txt",
		"pip3 install flask",
		"python -m pip install .",
		"python3 -m pip install -e .",
		"bundle install",
		"composer install",
	}
	for _, cmd := range cmds {
		combined := cmd + " && echo done"
		got := sandbox.FastPathCommand("node", combined, true)
		if strings.Contains(strings.ToLower(got), strings.ToLower(cmd)) {
			t.Errorf("dep install command %q should be stripped", cmd)
		}
	}
}

// ── ImageTag ────────────────────────────────────────────────────────────────

func TestImageTag_ValidFingerprint(t *testing.T) {
	fp := &sandbox.BuildFingerprint{
		Hash:        "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		BuildSystem: "go",
	}
	tag := sandbox.ImageTag("repo-123", fp)

	if tag == "" {
		t.Fatal("expected non-empty tag")
	}
	if !strings.HasPrefix(tag, "rtvortex/cache:") {
		t.Errorf("tag should start with rtvortex/cache:, got %q", tag)
	}
	parts := strings.SplitN(tag, ":", 2)
	if len(parts) != 2 || len(parts[1]) == 0 {
		t.Errorf("tag should have repo-prefix-fp-prefix suffix, got %q", tag)
	}
	if !strings.Contains(parts[1], "-") {
		t.Errorf("tag suffix should have dash separator: %q", parts[1])
	}
}

func TestImageTag_NilFingerprint(t *testing.T) {
	tag := sandbox.ImageTag("repo-123", nil)
	if tag != "" {
		t.Errorf("expected empty tag for nil fingerprint, got %q", tag)
	}
}

func TestImageTag_EmptyHash(t *testing.T) {
	fp := &sandbox.BuildFingerprint{Hash: "", BuildSystem: "go"}
	tag := sandbox.ImageTag("repo-123", fp)
	if tag != "" {
		t.Errorf("expected empty tag for empty hash, got %q", tag)
	}
}

func TestImageTag_Deterministic(t *testing.T) {
	fp := &sandbox.BuildFingerprint{
		Hash:        "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		BuildSystem: "go",
	}
	tag1 := sandbox.ImageTag("repo-123", fp)
	tag2 := sandbox.ImageTag("repo-123", fp)
	if tag1 != tag2 {
		t.Errorf("tags should be deterministic: %q != %q", tag1, tag2)
	}
}

func TestImageTag_DifferentRepos(t *testing.T) {
	fp := &sandbox.BuildFingerprint{
		Hash:        "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		BuildSystem: "go",
	}
	tag1 := sandbox.ImageTag("repo-A", fp)
	tag2 := sandbox.ImageTag("repo-B", fp)
	if tag1 == tag2 {
		t.Error("different repos should produce different tags")
	}
}

// ── Fingerprint JSON round-trip ─────────────────────────────────────────────

func TestBuildFingerprint_JSONRoundTrip(t *testing.T) {
	fp := &sandbox.BuildFingerprint{
		Hash:        "abc123",
		BuildSystem: "node",
		Files:       []string{"package.json", "package-lock.json"},
		FastPath:    true,
	}

	data, err := json.Marshal(fp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded sandbox.BuildFingerprint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Hash != fp.Hash {
		t.Errorf("Hash = %q, want %q", decoded.Hash, fp.Hash)
	}
	if decoded.BuildSystem != fp.BuildSystem {
		t.Errorf("BuildSystem = %q, want %q", decoded.BuildSystem, fp.BuildSystem)
	}
	if decoded.FastPath != fp.FastPath {
		t.Errorf("FastPath = %v, want %v", decoded.FastPath, fp.FastPath)
	}
	if len(decoded.Files) != len(fp.Files) {
		t.Errorf("Files len = %d, want %d", len(decoded.Files), len(fp.Files))
	}
}

// ── SandboxLimits ───────────────────────────────────────────────────────────

func TestApplyLimits_ClampsValues(t *testing.T) {
	h := &sandbox.Handler{}
	h.Limits = &sandbox.SandboxLimits{
		MaxTimeoutSec:  300,
		MaxMemoryMB:    1024,
		MaxCPU:         2,
		MaxRetries:     1,
		DefaultSandbox: true,
	}

	timeout, mem, cpu, sbx := h.ApplyLimits(600, "4g", "4", false)

	if timeout != 300 {
		t.Errorf("timeout = %d, want 300 (clamped)", timeout)
	}
	if mem != "1g" {
		t.Errorf("memory = %q, want %q (clamped)", mem, "1g")
	}
	if cpu != "2" {
		t.Errorf("cpu = %q, want %q (clamped)", cpu, "2")
	}
	if !sbx {
		t.Error("sandbox should be forced true by DefaultSandbox")
	}
}

func TestApplyLimits_WithinBounds(t *testing.T) {
	h := &sandbox.Handler{}
	h.Limits = &sandbox.SandboxLimits{
		MaxTimeoutSec: 600,
		MaxMemoryMB:   4096,
		MaxCPU:        4,
	}

	timeout, mem, cpu, sbx := h.ApplyLimits(300, "2g", "2", true)

	if timeout != 300 {
		t.Errorf("timeout = %d, want 300 (within bounds)", timeout)
	}
	if mem != "2g" {
		t.Errorf("memory = %q, want %q (within bounds)", mem, "2g")
	}
	if cpu != "2" {
		t.Errorf("cpu = %q, want %q (within bounds)", cpu, "2")
	}
	if !sbx {
		t.Error("sandbox should remain true")
	}
}

func TestApplyLimits_NilLimits(t *testing.T) {
	h := &sandbox.Handler{}

	timeout, mem, cpu, sbx := h.ApplyLimits(999, "8g", "8", false)

	if timeout != 999 {
		t.Errorf("timeout = %d, want 999 (no limits)", timeout)
	}
	if mem != "8g" {
		t.Errorf("memory = %q, want %q", mem, "8g")
	}
	if cpu != "8" {
		t.Errorf("cpu = %q, want %q", cpu, "8")
	}
	if sbx {
		t.Error("sandbox should remain false with nil limits")
	}
}

// ── HandleHealth ────────────────────────────────────────────────────────────

func TestHandleHealth_NoRuntime(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sandbox/health", nil)
	rr := httptest.NewRecorder()

	h.HandleHealth(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body["runtime_healthy"] != false {
		t.Error("runtime_healthy should be false with nil runtime")
	}
}

func TestHandleHealth_WithLimits(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)
	h.Limits = &sandbox.SandboxLimits{
		MaxTimeoutSec:  300,
		MaxMemoryMB:    1024,
		MaxCPU:         2,
		MaxRetries:     1,
		DefaultSandbox: true,
	}

	req := httptest.NewRequest(http.MethodGet, "/sandbox/health", nil)
	rr := httptest.NewRecorder()

	h.HandleHealth(rr, req)

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	limits, ok := body["config_limits"].(map[string]any)
	if !ok {
		t.Fatal("expected config_limits in response")
	}
	if limits["max_timeout_sec"] != float64(300) {
		t.Errorf("max_timeout_sec = %v, want 300", limits["max_timeout_sec"])
	}
	if limits["max_memory_mb"] != float64(1024) {
		t.Errorf("max_memory_mb = %v, want 1024", limits["max_memory_mb"])
	}
}

func TestHandleHealth_Defaults(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sandbox/health", nil)
	rr := httptest.NewRecorder()

	h.HandleHealth(rr, req)

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	defaults, ok := body["defaults"].(map[string]any)
	if !ok {
		t.Fatal("expected defaults in response")
	}
	if defaults["max_retries"] != float64(sandbox.MaxRetries) {
		t.Errorf("max_retries = %v, want %d", defaults["max_retries"], sandbox.MaxRetries)
	}
}
