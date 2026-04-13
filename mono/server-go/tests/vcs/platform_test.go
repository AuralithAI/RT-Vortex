package vcs_test

import (
	"context"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// ── Mock Platform ───────────────────────────────────────────────────────────

type mockPlatform struct {
	platformType vcs.PlatformType
}

func (m *mockPlatform) Type() vcs.PlatformType { return m.platformType }
func (m *mockPlatform) GetPullRequest(_ context.Context, _, _ string, _ int) (*vcs.PullRequest, error) {
	return nil, nil
}
func (m *mockPlatform) GetPullRequestDiff(_ context.Context, _, _ string, _ int) ([]vcs.DiffFile, error) {
	return nil, nil
}
func (m *mockPlatform) PostReviewComment(_ context.Context, _, _ string, _ int, _ *vcs.ReviewCommentRequest) error {
	return nil
}
func (m *mockPlatform) PostReviewSummary(_ context.Context, _, _ string, _ int, _ string) error {
	return nil
}
func (m *mockPlatform) GetFileContent(_ context.Context, _, _, _, _ string) ([]byte, error) {
	return nil, nil
}
func (m *mockPlatform) ListDirectory(_ context.Context, _, _, _, _ string) ([]vcs.DirEntry, error) {
	return nil, nil
}
func (m *mockPlatform) ValidateWebhookSignature(_ []byte, _ string) bool {
	return true
}
func (m *mockPlatform) ListOpenPullRequests(_ context.Context, _, _ string, _ int) ([]vcs.PullRequest, error) {
	return nil, nil
}
func (m *mockPlatform) CreateBranch(_ context.Context, _, _ string, _ *vcs.CreateBranchRequest) error {
	return nil
}
func (m *mockPlatform) CreateOrUpdateFile(_ context.Context, _, _, _ string, _ *vcs.FileCommit) (string, error) {
	return "", nil
}
func (m *mockPlatform) CreatePullRequest(_ context.Context, _, _ string, _ *vcs.CreatePullRequestRequest) (*vcs.PullRequest, error) {
	return nil, nil
}
func (m *mockPlatform) GetDefaultBranch(_ context.Context, _, _ string) (string, error) {
	return "main", nil
}
func (m *mockPlatform) GetBranchSHA(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockPlatform) GetCombinedStatus(_ context.Context, _, _, _ string) (*vcs.CombinedStatus, error) {
	return nil, nil
}

// ── Registry Tests ──────────────────────────────────────────────────────────

func TestNewPlatformRegistry(t *testing.T) {
	r := vcs.NewPlatformRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.List()) != 0 {
		t.Errorf("expected 0 platforms, got %d", len(r.List()))
	}
}

func TestPlatformRegistry_Register_And_Get(t *testing.T) {
	r := vcs.NewPlatformRegistry()
	r.Register(&mockPlatform{platformType: vcs.PlatformGitHub})

	p, ok := r.Get(vcs.PlatformGitHub)
	if !ok {
		t.Fatal("expected to find GitHub platform")
	}
	if p.Type() != vcs.PlatformGitHub {
		t.Errorf("expected github, got %s", p.Type())
	}
}

func TestPlatformRegistry_Get_NotFound(t *testing.T) {
	r := vcs.NewPlatformRegistry()
	_, ok := r.Get(vcs.PlatformGitLab)
	if ok {
		t.Error("expected Get to return false for unregistered platform")
	}
}

func TestPlatformRegistry_Register_Multiple(t *testing.T) {
	r := vcs.NewPlatformRegistry()
	r.Register(&mockPlatform{platformType: vcs.PlatformGitHub})
	r.Register(&mockPlatform{platformType: vcs.PlatformGitLab})
	r.Register(&mockPlatform{platformType: vcs.PlatformBitbucket})

	types := r.List()
	if len(types) != 3 {
		t.Fatalf("expected 3 platforms, got %d", len(types))
	}
}

func TestPlatformRegistry_Register_Overwrite(t *testing.T) {
	r := vcs.NewPlatformRegistry()
	r.Register(&mockPlatform{platformType: vcs.PlatformGitHub})
	r.Register(&mockPlatform{platformType: vcs.PlatformGitHub}) // overwrite

	types := r.List()
	if len(types) != 1 {
		t.Errorf("expected 1 platform after overwrite, got %d", len(types))
	}
}

// ── Platform Type Constants ─────────────────────────────────────────────────

func TestPlatformTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    vcs.PlatformType
		expected string
	}{
		{"GitHub", vcs.PlatformGitHub, "github"},
		{"GitLab", vcs.PlatformGitLab, "gitlab"},
		{"Bitbucket", vcs.PlatformBitbucket, "bitbucket"},
		{"AzureDevOps", vcs.PlatformAzureDevOps, "azure_devops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.value)
			}
		})
	}
}

// ── Error Constants ─────────────────────────────────────────────────────────

func TestVCSErrorConstants(t *testing.T) {
	if vcs.ErrPlatformNotFound == nil {
		t.Error("expected non-nil ErrPlatformNotFound")
	}
	if vcs.ErrUnauthorized == nil {
		t.Error("expected non-nil ErrUnauthorized")
	}
	if vcs.ErrRepoNotFound == nil {
		t.Error("expected non-nil ErrRepoNotFound")
	}
	if vcs.ErrPRNotFound == nil {
		t.Error("expected non-nil ErrPRNotFound")
	}
}

// ── DiffFile / PullRequest structs ──────────────────────────────────────────

func TestDiffFile_Fields(t *testing.T) {
	df := vcs.DiffFile{
		Filename:  "main.go",
		Status:    "modified",
		Additions: 10,
		Deletions: 5,
		Patch:     "@@ -1,5 +1,10 @@\n+new line",
	}
	if df.Filename != "main.go" {
		t.Error("expected main.go")
	}
	if df.Status != "modified" {
		t.Error("expected modified")
	}
	if df.Additions != 10 || df.Deletions != 5 {
		t.Error("wrong additions/deletions")
	}
}

func TestPullRequest_Fields(t *testing.T) {
	pr := vcs.PullRequest{
		Number:       42,
		Title:        "Feature X",
		Author:       "dev",
		SourceBranch: "feature-x",
		TargetBranch: "main",
		State:        "open",
		HeadSHA:      "abc123",
		BaseSHA:      "def456",
	}
	if pr.Number != 42 {
		t.Error("wrong number")
	}
	if pr.Title != "Feature X" {
		t.Error("wrong title")
	}
	if pr.State != "open" {
		t.Error("wrong state")
	}
}
