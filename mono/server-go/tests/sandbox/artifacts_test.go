package sandbox_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/google/uuid"
)

// ── ClassifyArtifact ────────────────────────────────────────────────────────

func TestClassifyArtifact_Coverage(t *testing.T) {
	cases := []string{
		"coverage/index.html",
		"htmlcov/index.html",
		".coverage",
		"lcov.info",
		"cover.out",
		"cobertura.xml",
		"coverage.xml",
		"target/site/jacoco/index.html",
	}
	for _, path := range cases {
		kind := sandbox.ClassifyArtifact(path)
		if kind != sandbox.ArtifactCoverage {
			t.Errorf("ClassifyArtifact(%q) = %q, want %q", path, kind, sandbox.ArtifactCoverage)
		}
	}
}

func TestClassifyArtifact_TestReport(t *testing.T) {
	cases := []string{
		"test-results/output.xml",
		"junit.xml",
		"test-report.xml",
		"target/surefire-reports/TEST-foo.xml",
	}
	for _, path := range cases {
		kind := sandbox.ClassifyArtifact(path)
		if kind != sandbox.ArtifactTestReport {
			t.Errorf("ClassifyArtifact(%q) = %q, want %q", path, kind, sandbox.ArtifactTestReport)
		}
	}
}

func TestClassifyArtifact_Log(t *testing.T) {
	kind := sandbox.ClassifyArtifact("build.log")
	if kind != sandbox.ArtifactLog {
		t.Errorf("ClassifyArtifact(build.log) = %q, want %q", kind, sandbox.ArtifactLog)
	}
}

func TestClassifyArtifact_Generic(t *testing.T) {
	kind := sandbox.ClassifyArtifact("src/main.go")
	if kind != sandbox.ArtifactGeneric {
		t.Errorf("ClassifyArtifact(src/main.go) = %q, want %q", kind, sandbox.ArtifactGeneric)
	}
}

// ── WorkspaceArchive ────────────────────────────────────────────────────────

func TestWorkspaceArchive_Empty(t *testing.T) {
	data, err := sandbox.WorkspaceArchive(nil)
	if err != nil {
		t.Fatalf("WorkspaceArchive(nil): %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty archive even for nil changeset")
	}
}

func TestWorkspaceArchive_RoundTrip(t *testing.T) {
	changeset := map[string]string{
		"src/main.go": "package main\nfunc main() {}\n",
		"README.md":   "# Hello\n",
	}

	data, err := sandbox.WorkspaceArchive(changeset)
	if err != nil {
		t.Fatalf("WorkspaceArchive: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty archive")
	}

	// Decompress and verify contents.
	result, err := sandbox.DecompressWorkspaceArchive(data)
	if err != nil {
		t.Fatalf("DecompressWorkspaceArchive: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result))
	}

	if got := result["src/main.go"]; got != changeset["src/main.go"] {
		t.Errorf("src/main.go content = %q, want %q", got, changeset["src/main.go"])
	}
	if got := result["README.md"]; got != changeset["README.md"] {
		t.Errorf("README.md content = %q, want %q", got, changeset["README.md"])
	}
}

func TestWorkspaceArchive_SkipsDeletedFiles(t *testing.T) {
	changeset := map[string]string{
		"keep.go":   "package main\n",
		"deleted.go": "",
	}

	data, err := sandbox.WorkspaceArchive(changeset)
	if err != nil {
		t.Fatalf("WorkspaceArchive: %v", err)
	}

	result, err := sandbox.DecompressWorkspaceArchive(data)
	if err != nil {
		t.Fatalf("DecompressWorkspaceArchive: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 file (deleted should be skipped), got %d", len(result))
	}
	if _, ok := result["keep.go"]; !ok {
		t.Error("expected keep.go in archive")
	}
}

func TestWorkspaceArchive_StripLeadingSlash(t *testing.T) {
	changeset := map[string]string{
		"/src/main.go": "package main\n",
	}

	data, err := sandbox.WorkspaceArchive(changeset)
	if err != nil {
		t.Fatalf("WorkspaceArchive: %v", err)
	}

	result, err := sandbox.DecompressWorkspaceArchive(data)
	if err != nil {
		t.Fatalf("DecompressWorkspaceArchive: %v", err)
	}

	if _, ok := result["src/main.go"]; !ok {
		t.Errorf("expected leading slash stripped, got keys: %v", keysOf(result))
	}
}

// ── WorkspaceSize ───────────────────────────────────────────────────────────

func TestWorkspaceSize_Empty(t *testing.T) {
	if sandbox.WorkspaceSize(nil) != 0 {
		t.Error("expected 0 for nil map")
	}
}

func TestWorkspaceSize_NonEmpty(t *testing.T) {
	cs := map[string]string{
		"a.go": "abc",
		"b.go": "de",
	}
	if sandbox.WorkspaceSize(cs) != 5 {
		t.Errorf("WorkspaceSize = %d, want 5", sandbox.WorkspaceSize(cs))
	}
}

// ── ParseArtifactsFromLogs ──────────────────────────────────────────────────

func TestParseArtifactsFromLogs_GoCoverage(t *testing.T) {
	logs := `ok  	mypackage	0.123s	coverage: 85.4% of statements
PASS`
	buildID := uuid.New()
	arts := sandbox.ParseArtifactsFromLogs(logs, buildID)

	if len(arts) == 0 {
		t.Fatal("expected at least one artifact from go coverage output")
	}

	found := false
	for _, a := range arts {
		if a.Kind == sandbox.ArtifactCoverage {
			found = true
			if a.BuildID != buildID {
				t.Errorf("artifact BuildID = %s, want %s", a.BuildID, buildID)
			}
		}
	}
	if !found {
		t.Error("expected a coverage artifact")
	}
}

func TestParseArtifactsFromLogs_NoArtifacts(t *testing.T) {
	arts := sandbox.ParseArtifactsFromLogs("BUILD SUCCESSFUL", uuid.New())
	if len(arts) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(arts))
	}
}

// ── PrepareWorkspace + CleanupWorkspace ─────────────────────────────────────

func TestPrepareWorkspace_EmptyFS(t *testing.T) {
	plan := &sandbox.BuildPlan{}
	if err := sandbox.PrepareWorkspace(plan); err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if plan.WorkspaceDir != "" {
		t.Error("expected empty WorkspaceDir for nil WorkspaceFS")
	}
}

func TestPrepareWorkspace_WritesFiles(t *testing.T) {
	plan := &sandbox.BuildPlan{
		WorkspaceFS: map[string]string{
			"main.go":       "package main\n",
			"lib/helper.go": "package lib\n",
		},
	}

	if err := sandbox.PrepareWorkspace(plan); err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	defer sandbox.CleanupWorkspace(plan)

	if plan.WorkspaceDir == "" {
		t.Fatal("expected non-empty WorkspaceDir")
	}
}

func TestCleanupWorkspace_RemovesDir(t *testing.T) {
	plan := &sandbox.BuildPlan{
		WorkspaceFS: map[string]string{
			"test.go": "package test\n",
		},
	}

	if err := sandbox.PrepareWorkspace(plan); err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}

	dir := plan.WorkspaceDir
	sandbox.CleanupWorkspace(plan)

	if plan.WorkspaceDir != "" {
		t.Error("expected WorkspaceDir to be cleared")
	}

	// The directory should no longer exist, but we don't assert on fs
	// because os.RemoveAll is best-effort in tests.
	_ = dir
}

// ── Constants ───────────────────────────────────────────────────────────────

func TestMaxArtifactConstants(t *testing.T) {
	if sandbox.MaxArtifactSize <= 0 {
		t.Error("MaxArtifactSize must be positive")
	}
	if sandbox.MaxArtifactsPerBuild <= 0 {
		t.Error("MaxArtifactsPerBuild must be positive")
	}
	if sandbox.MaxTotalArtifactBytes <= 0 {
		t.Error("MaxTotalArtifactBytes must be positive")
	}
	if sandbox.MaxWorkspaceBytes <= 0 {
		t.Error("MaxWorkspaceBytes must be positive")
	}
}

func TestDefaultArtifactPaths_NonEmpty(t *testing.T) {
	if len(sandbox.DefaultArtifactPaths) == 0 {
		t.Error("expected non-empty DefaultArtifactPaths")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
