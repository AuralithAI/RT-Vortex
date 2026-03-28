package api

import (
	"encoding/json"
	"fmt"
)

// ── GitHub Webhook Payloads ─────────────────────────────────────────────────

// GitHubPRPayload represents the relevant fields from a GitHub pull_request event.
type GitHubPRPayload struct {
	Action      string `json:"action"` // opened, synchronize, closed, reopened
	PullRequest struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	Repository struct {
		ID       int    `json:"id"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func parseGitHubPRPayload(body []byte) (*GitHubPRPayload, error) {
	var p GitHubPRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshal GitHub PR payload: %w", err)
	}
	return &p, nil
}

// ── GitLab Webhook Payloads ─────────────────────────────────────────────────

// GitLabMRPayload represents the relevant fields from a GitLab Merge Request Hook event.
type GitLabMRPayload struct {
	ObjectKind       string `json:"object_kind"` // merge_request
	ObjectAttributes struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		State        string `json:"state"`
		Action       string `json:"action"` // open, close, reopen, update, merge
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
		AuthorID int `json:"author_id"`
	} `json:"object_attributes"`
	Project struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	User struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"user"`
}

func parseGitLabMRPayload(body []byte) (*GitLabMRPayload, error) {
	var p GitLabMRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshal GitLab MR payload: %w", err)
	}
	return &p, nil
}

// ── Bitbucket Webhook Payloads ──────────────────────────────────────────────

// BitbucketPRPayload represents the relevant fields from a Bitbucket pullrequest event.
type BitbucketPRPayload struct {
	PullRequest struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			DisplayName string `json:"display_name"`
			UUID        string `json:"uuid"`
		} `json:"author"`
		Source struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
			Commit struct {
				Hash string `json:"hash"`
			} `json:"commit"`
		} `json:"source"`
		Destination struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
			Commit struct {
				Hash string `json:"hash"`
			} `json:"commit"`
		} `json:"destination"`
	} `json:"pullrequest"`
	Repository struct {
		UUID     string `json:"uuid"`
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		Owner    struct {
			DisplayName string `json:"display_name"`
			UUID        string `json:"uuid"`
		} `json:"owner"`
	} `json:"repository"`
	Actor struct {
		DisplayName string `json:"display_name"`
		UUID        string `json:"uuid"`
	} `json:"actor"`
}

func parseBitbucketPRPayload(body []byte) (*BitbucketPRPayload, error) {
	var p BitbucketPRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshal Bitbucket PR payload: %w", err)
	}
	return &p, nil
}

// ── Azure DevOps Webhook Payloads ───────────────────────────────────────────

// AzureDevOpsPRPayload represents relevant fields from an Azure DevOps service hook event.
type AzureDevOpsPRPayload struct {
	EventType string `json:"eventType"` // git.pullrequest.created, git.pullrequest.updated
	Resource  struct {
		PullRequestID int    `json:"pullRequestId"`
		Title         string `json:"title"`
		Status        string `json:"status"`
		CreatedBy     struct {
			DisplayName string `json:"displayName"`
			UniqueName  string `json:"uniqueName"`
		} `json:"createdBy"`
		SourceRefName         string `json:"sourceRefName"` // refs/heads/feature-branch
		TargetRefName         string `json:"targetRefName"` // refs/heads/main
		LastMergeSourceCommit struct {
			CommitID string `json:"commitId"`
		} `json:"lastMergeSourceCommit"`
		Repository struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"repository"`
	} `json:"resource"`
}

func parseAzureDevOpsPRPayload(body []byte) (*AzureDevOpsPRPayload, error) {
	var p AzureDevOpsPRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshal Azure DevOps PR payload: %w", err)
	}
	return &p, nil
}
