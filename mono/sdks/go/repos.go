package rtvortex

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// RepoClient provides methods for the /api/v1/repos endpoints.
type RepoClient struct {
	c *Client
}

// Repository represents a registered repository.
type Repository struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Owner         string    `json:"owner"`
	Platform      string    `json:"platform"`
	CloneURL      string    `json:"clone_url"`
	DefaultBranch string    `json:"default_branch"`
	OrgID         uuid.UUID `json:"org_id"`
	IsActive      bool      `json:"is_active"`
}

// List returns repositories for the authenticated user.
func (r *RepoClient) List(ctx context.Context) ([]Repository, error) {
	var repos []Repository
	err := r.c.do(ctx, http.MethodGet, "/api/v1/repos", nil, &repos)
	return repos, err
}

// Get retrieves a repository by ID.
func (r *RepoClient) Get(ctx context.Context, id uuid.UUID) (*Repository, error) {
	var repo Repository
	err := r.c.do(ctx, http.MethodGet, "/api/v1/repos/"+id.String(), nil, &repo)
	return &repo, err
}

// TriggerIndex starts a re-index of the repository.
func (r *RepoClient) TriggerIndex(ctx context.Context, id uuid.UUID) error {
	return r.c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/index", id), nil, nil)
}

// ReviewClient provides methods for the /api/v1/reviews endpoints.
type ReviewClient struct {
	c *Client
}

// Review represents a code review.
type Review struct {
	ID          uuid.UUID `json:"id"`
	RepoID      uuid.UUID `json:"repo_id"`
	PRNumber    int       `json:"pr_number"`
	PRTitle     string    `json:"pr_title"`
	Status      string    `json:"status"`
	TriggeredBy uuid.UUID `json:"triggered_by"`
}

// List returns reviews.
func (rv *ReviewClient) List(ctx context.Context) ([]Review, error) {
	var reviews []Review
	err := rv.c.do(ctx, http.MethodGet, "/api/v1/reviews", nil, &reviews)
	return reviews, err
}

// Get retrieves a review by ID.
func (rv *ReviewClient) Get(ctx context.Context, id uuid.UUID) (*Review, error) {
	var review Review
	err := rv.c.do(ctx, http.MethodGet, "/api/v1/reviews/"+id.String(), nil, &review)
	return &review, err
}
