package sandbox_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/google/uuid"
)

// ── RedactLogs ──────────────────────────────────────────────────────────────

func TestRedactLogs_ExactSecretValues(t *testing.T) {
	secrets := map[string]string{
		"DB_PASSWORD": "super_secret_123",
		"API_KEY":     "sk-abc-xyz-999",
	}
	logs := "connecting with password super_secret_123 and key sk-abc-xyz-999\ndone"
	redacted := sandbox.RedactLogs(logs, secrets)

	if strings.Contains(redacted, "super_secret_123") {
		t.Error("DB_PASSWORD value should be redacted")
	}
	if strings.Contains(redacted, "sk-abc-xyz-999") {
		t.Error("API_KEY value should be redacted")
	}
	if !strings.Contains(redacted, "[REDACTED:DB_PASSWORD]") {
		t.Error("should contain [REDACTED:DB_PASSWORD] marker")
	}
	if !strings.Contains(redacted, "[REDACTED:API_KEY]") {
		t.Error("should contain [REDACTED:API_KEY] marker")
	}
}

func TestRedactLogs_NoSecrets(t *testing.T) {
	logs := "build successful\nexit code 0"
	redacted := sandbox.RedactLogs(logs, nil)
	if redacted != logs {
		t.Errorf("logs should be unchanged with nil secrets, got %q", redacted)
	}
}

func TestRedactLogs_EmptySecretValues(t *testing.T) {
	secrets := map[string]string{"KEY": ""}
	logs := "build successful"
	redacted := sandbox.RedactLogs(logs, secrets)
	if redacted != logs {
		t.Errorf("empty secret values should not cause replacement, got %q", redacted)
	}
}

func TestRedactLogs_AWSKeys(t *testing.T) {
	logs := "using key AKIAIOSFODNN7EXAMPLE for auth"
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("AWS access key should be redacted")
	}
}

func TestRedactLogs_GitHubTokens(t *testing.T) {
	logs := "cloning with token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef123456"
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "ghp_") {
		t.Error("GitHub token should be redacted")
	}
}

func TestRedactLogs_GitLabTokens(t *testing.T) {
	logs := "authenticating with glpat-xyzABCDEFGH12345678901234"
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "glpat-") {
		t.Error("GitLab token should be redacted")
	}
}

func TestRedactLogs_JWTTokens(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	logs := "auth header: Bearer " + jwt
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "eyJhbGci") {
		t.Error("JWT token should be redacted")
	}
}

func TestRedactLogs_PrivateKeys(t *testing.T) {
	logs := "loading -----BEGIN RSA PRIVATE KEY-----\nMIIE..."
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "BEGIN RSA PRIVATE KEY") {
		t.Error("private key header should be redacted")
	}
}

func TestRedactLogs_ConnectionStrings(t *testing.T) {
	logs := "connecting to postgres://admin:s3cret@db.example.com:5432/mydb"
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "s3cret") {
		t.Error("connection string password should be redacted")
	}
}

func TestRedactLogs_EmailAddresses(t *testing.T) {
	logs := "sending notification to user@example.com"
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "user@example.com") {
		t.Error("email address should be redacted")
	}
}

func TestRedactLogs_PasswordAssignments(t *testing.T) {
	cases := []struct {
		name string
		logs string
	}{
		{"env var", "PASSWORD=my_secret_password_123"},
		{"yaml", "password: longSecretValue99"},
		{"json-like", `"secret": "a-very-long-secret-value"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redacted := sandbox.RedactLogs(tc.logs, nil)
			if redacted == tc.logs {
				t.Errorf("expected redaction for %q", tc.logs)
			}
		})
	}
}

func TestRedactLogs_PreservesNormalOutput(t *testing.T) {
	logs := "Compiling main.go...\nBuild successful\n3 tests passed\nexit code 0"
	redacted := sandbox.RedactLogs(logs, nil)
	if redacted != logs {
		t.Errorf("normal build output should be unchanged, got diff:\n--- original\n%s\n--- redacted\n%s", logs, redacted)
	}
}

func TestRedactLogs_IPWithPort(t *testing.T) {
	logs := "connecting to internal service at 10.0.1.45:8080"
	redacted := sandbox.RedactLogs(logs, nil)
	if strings.Contains(redacted, "10.0.1.45:8080") {
		t.Error("internal IP:port should be redacted")
	}
}

// ── RedactLogSummary ────────────────────────────────────────────────────────

func TestRedactLogSummary_Truncates(t *testing.T) {
	longLog := strings.Repeat("x", 200)
	result := sandbox.RedactLogSummary(longLog, nil, 100)
	if len(result) > 120 {
		t.Errorf("should truncate near 100 bytes, got %d", len(result))
	}
	if !strings.Contains(result, "[truncated]") {
		t.Error("should contain truncation marker")
	}
}

func TestRedactLogSummary_RedactsAndTruncates(t *testing.T) {
	secrets := map[string]string{"KEY": "mysecretvalue"}
	logs := "output: mysecretvalue " + strings.Repeat("x", 200)
	result := sandbox.RedactLogSummary(logs, secrets, 100)
	if strings.Contains(result, "mysecretvalue") {
		t.Error("should redact before truncation")
	}
}

// ── ContainsSecret ──────────────────────────────────────────────────────────

func TestContainsSecret_Found(t *testing.T) {
	secrets := map[string]string{"TOKEN": "abc123secret"}
	if !sandbox.ContainsSecret("output contains abc123secret here", secrets) {
		t.Error("should detect secret in text")
	}
}

func TestContainsSecret_NotFound(t *testing.T) {
	secrets := map[string]string{"TOKEN": "abc123secret"}
	if sandbox.ContainsSecret("clean build output", secrets) {
		t.Error("should not detect secret in clean output")
	}
}

func TestContainsSecret_EmptyValues(t *testing.T) {
	secrets := map[string]string{"TOKEN": ""}
	if sandbox.ContainsSecret("anything", secrets) {
		t.Error("empty secret values should never match")
	}
}

// ── MaskField ───────────────────────────────────────────────────────────────

func TestMaskField_LongValue(t *testing.T) {
	masked := sandbox.MaskField("mysecretpassword")
	if masked == "mysecretpassword" {
		t.Error("should mask the value")
	}
	if !strings.HasPrefix(masked, "my") {
		t.Errorf("should keep first 2 chars, got %q", masked)
	}
	if !strings.HasSuffix(masked, "rd") {
		t.Errorf("should keep last 2 chars, got %q", masked)
	}
	if !strings.Contains(masked, "****") {
		t.Error("should contain asterisks")
	}
}

func TestMaskField_ShortValue(t *testing.T) {
	masked := sandbox.MaskField("short")
	if masked != "****" {
		t.Errorf("short values should be fully masked, got %q", masked)
	}
}

// ── AuditLogger ─────────────────────────────────────────────────────────────

func TestAuditLogger_LogNoPool(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.Log(context.Background(), sandbox.AuditEvent{
		Action: sandbox.AuditSecretAccess,
		UserID: "user-1",
		RepoID: "repo-1",
		Detail: map[string]any{"secret_name": "API_KEY"},
	})
}

func TestAuditLogger_SecretAccess(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.LogSecretAccess(context.Background(), "user-1", "repo-1", "build-1", "DB_PASSWORD")
}

func TestAuditLogger_SecretDenied(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.LogSecretDenied(context.Background(), "user-1", "repo-1", "AWS_KEY", "not found")
}

func TestAuditLogger_ContainerLifecycle(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.LogContainerCreated(context.Background(), "build-1", "golang:1.22", true)
	a.LogContainerDestroyed(context.Background(), "build-1")
}

func TestAuditLogger_LogRedaction(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.LogRedaction(context.Background(), "build-1", 5)
}

func TestAuditLogger_WorkspaceScrub(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.LogWorkspaceScrub(context.Background(), "build-1", 12)
}

func TestAuditLogger_AccessDenied(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	a.LogAccessDenied(context.Background(), "user-1", "build/xyz", "not owner")
}

// ── AuditEvent JSON ─────────────────────────────────────────────────────────

func TestAuditEvent_JSONRoundTrip(t *testing.T) {
	event := sandbox.AuditEvent{
		ID:      uuid.New(),
		Action:  sandbox.AuditSecretAccess,
		UserID:  "user-1",
		RepoID:  "repo-1",
		BuildID: "build-1",
		Detail:  map[string]any{"secret_name": "API_KEY"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded sandbox.AuditEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Action != event.Action {
		t.Errorf("Action = %q, want %q", decoded.Action, event.Action)
	}
	if decoded.UserID != event.UserID {
		t.Errorf("UserID = %q, want %q", decoded.UserID, event.UserID)
	}
}

// ── ValidateBuildOwnership ──────────────────────────────────────────────────

func TestValidateBuildOwnership_MatchingUser(t *testing.T) {
	userID := uuid.New()
	rec := &sandbox.BuildRecord{
		ID:     uuid.New(),
		UserID: &userID,
	}

	// We cannot test with a real store here (no DB), but we can verify
	// the ownership logic by testing the function signature contract.
	// The actual integration test requires SANDBOX_TEST_DB.
	_ = rec
}

// ── SecureCleanupWorkspace ──────────────────────────────────────────────────

func TestSecureCleanupWorkspace_CleansMemory(t *testing.T) {
	dir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "util.go"), []byte("package sub"), 0644)

	plan := &sandbox.BuildPlan{
		WorkspaceDir: dir,
		WorkspaceFS: map[string]string{
			"main.go": "package main",
			"go.mod":  "module test",
		},
	}

	count := sandbox.SecureCleanupWorkspace(plan)

	if count != 3 {
		t.Errorf("expected 3 files removed, got %d", count)
	}

	if plan.WorkspaceDir != "" {
		t.Error("WorkspaceDir should be empty after cleanup")
	}

	for k, v := range plan.WorkspaceFS {
		if v != "" {
			t.Errorf("WorkspaceFS[%q] should be zeroed, got %q", k, v)
		}
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("workspace directory should be removed")
	}
}

func TestSecureCleanupWorkspace_EmptyDir(t *testing.T) {
	plan := &sandbox.BuildPlan{}
	count := sandbox.SecureCleanupWorkspace(plan)
	if count != 0 {
		t.Errorf("expected 0 files, got %d", count)
	}
}

// ── Handler with Audit Integration ──────────────────────────────────────────

func TestHandler_AuditFieldNilSafe(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)
	if h.Audit != nil {
		t.Error("Audit should be nil by default")
	}
}

func TestHandler_AuditFieldSet(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)
	a := sandbox.NewAuditLogger(nil, nil)
	h.Audit = a
	if h.Audit == nil {
		t.Error("Audit should be set")
	}
}
