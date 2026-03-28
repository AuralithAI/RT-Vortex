package api_test

import (
	"encoding/json"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/api"
)

// ── GitHub Webhook Payload Parsing ──────────────────────────────────────────

func TestParseGitHubPRPayload_Valid(t *testing.T) {
	payload := `{
		"action": "opened",
		"number": 42,
		"pull_request": {
			"number": 42,
			"title": "Add feature X",
			"user": {"login": "octocat"},
			"head": {"ref": "feature-x", "sha": "abc123"},
			"base": {"ref": "main", "sha": "def456"}
		},
		"repository": {
			"id": 12345,
			"full_name": "owner/repo",
			"clone_url": "https://github.com/owner/repo.git"
		}
	}`

	var result api.GitHubPRPayload
	err := json.Unmarshal([]byte(payload), &result)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if result.Action != "opened" {
		t.Errorf("expected action 'opened', got %s", result.Action)
	}
	if result.PullRequest.Number != 42 {
		t.Errorf("expected PR number 42, got %d", result.PullRequest.Number)
	}
	if result.PullRequest.Title != "Add feature X" {
		t.Errorf("expected title 'Add feature X', got %s", result.PullRequest.Title)
	}
	if result.Repository.ID != 12345 {
		t.Errorf("expected repo ID 12345, got %d", result.Repository.ID)
	}
	if result.PullRequest.Head.Ref != "feature-x" {
		t.Errorf("expected head ref 'feature-x', got %s", result.PullRequest.Head.Ref)
	}
}

func TestParseGitHubPRPayload_Synchronize(t *testing.T) {
	payload := `{
		"action": "synchronize",
		"pull_request": {"number": 99, "title": "Update"},
		"repository": {"id": 1}
	}`

	var result api.GitHubPRPayload
	err := json.Unmarshal([]byte(payload), &result)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if result.Action != "synchronize" {
		t.Errorf("expected action 'synchronize', got %s", result.Action)
	}
}

// ── GitLab Webhook Payload Parsing ──────────────────────────────────────────

func TestParseGitLabMRPayload_Valid(t *testing.T) {
	payload := `{
		"object_kind": "merge_request",
		"project": {"id": 555, "path_with_namespace": "group/project"},
		"object_attributes": {
			"iid": 7,
			"title": "Fix bug",
			"action": "open",
			"source_branch": "fix-bug",
			"target_branch": "main",
			"author_id": 1
		}
	}`

	var result api.GitLabMRPayload
	err := json.Unmarshal([]byte(payload), &result)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if result.Project.ID != 555 {
		t.Errorf("expected project ID 555, got %d", result.Project.ID)
	}
	if result.ObjectAttributes.IID != 7 {
		t.Errorf("expected IID 7, got %d", result.ObjectAttributes.IID)
	}
	if result.ObjectAttributes.Action != "open" {
		t.Errorf("expected action 'open', got %s", result.ObjectAttributes.Action)
	}
}

// ── Bitbucket Webhook Payload Parsing ───────────────────────────────────────

func TestParseBitbucketPRPayload_Valid(t *testing.T) {
	payload := `{
		"pullrequest": {
			"id": 33,
			"title": "Add tests",
			"author": {"display_name": "dev"},
			"source": {"branch": {"name": "add-tests"}},
			"destination": {"branch": {"name": "main"}}
		},
		"repository": {"uuid": "{repo-uuid-123}", "full_name": "team/repo"}
	}`

	var result api.BitbucketPRPayload
	err := json.Unmarshal([]byte(payload), &result)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if result.PullRequest.ID != 33 {
		t.Errorf("expected PR ID 33, got %d", result.PullRequest.ID)
	}
	if result.Repository.UUID != "{repo-uuid-123}" {
		t.Errorf("expected UUID '{repo-uuid-123}', got %s", result.Repository.UUID)
	}
}

// ── Azure DevOps Webhook Payload Parsing ────────────────────────────────────

func TestParseAzureDevOpsPRPayload_Valid(t *testing.T) {
	payload := `{
		"eventType": "git.pullrequest.created",
		"resource": {
			"pullRequestId": 101,
			"title": "New feature",
			"createdBy": {"displayName": "dev"},
			"sourceRefName": "refs/heads/feature",
			"targetRefName": "refs/heads/main",
			"repository": {"id": "az-repo-id-123", "name": "myrepo"}
		}
	}`

	var result api.AzureDevOpsPRPayload
	err := json.Unmarshal([]byte(payload), &result)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if result.EventType != "git.pullrequest.created" {
		t.Errorf("expected event git.pullrequest.created, got %s", result.EventType)
	}
	if result.Resource.PullRequestID != 101 {
		t.Errorf("expected PR ID 101, got %d", result.Resource.PullRequestID)
	}
	if result.Resource.Repository.ID != "az-repo-id-123" {
		t.Errorf("expected repo ID az-repo-id-123, got %s", result.Resource.Repository.ID)
	}
}

// ── Empty / Malformed Payloads ──────────────────────────────────────────────

func TestParseGitHubPRPayload_Empty(t *testing.T) {
	var result api.GitHubPRPayload
	err := json.Unmarshal([]byte(`{}`), &result)
	if err != nil {
		t.Fatalf("expected no error for empty JSON, got %v", err)
	}
	if result.Action != "" {
		t.Errorf("expected empty action, got %s", result.Action)
	}
}

func TestParseGitHubPRPayload_Invalid(t *testing.T) {
	var result api.GitHubPRPayload
	err := json.Unmarshal([]byte(`not json`), &result)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
