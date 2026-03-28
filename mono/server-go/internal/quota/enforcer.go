// Package quota provides plan-based rate limiting and usage enforcement.
//
// Each organization plan (free, pro, enterprise) has predefined limits on
// reviews per day, repositories, members, and tokens per day.
package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Plan Limits ─────────────────────────────────────────────────────────────

// PlanLimits defines the resource limits for a plan tier.
type PlanLimits struct {
	ReviewsPerDay   int   `json:"reviews_per_day"`
	ReposPerOrg     int   `json:"repos_per_org"`
	MembersPerOrg   int   `json:"members_per_org"`
	TokensPerDay    int64 `json:"tokens_per_day"`
	MaxFileSizeKB   int   `json:"max_file_size_kb"`
	IndexingEnabled bool  `json:"indexing_enabled"`
}

// DefaultLimits returns the limits for each plan tier.
var DefaultLimits = map[string]PlanLimits{
	"free": {
		ReviewsPerDay:   10,
		ReposPerOrg:     5,
		MembersPerOrg:   5,
		TokensPerDay:    100_000,
		MaxFileSizeKB:   256,
		IndexingEnabled: false,
	},
	"pro": {
		ReviewsPerDay:   100,
		ReposPerOrg:     50,
		MembersPerOrg:   50,
		TokensPerDay:    1_000_000,
		MaxFileSizeKB:   512,
		IndexingEnabled: true,
	},
	"enterprise": {
		ReviewsPerDay:   -1, // unlimited
		ReposPerOrg:     -1,
		MembersPerOrg:   -1,
		TokensPerDay:    -1,
		MaxFileSizeKB:   1024,
		IndexingEnabled: true,
	},
}

// ── Quota Usage ─────────────────────────────────────────────────────────────

// Usage represents the current usage counters for an organization.
type Usage struct {
	ReviewsToday int   `json:"reviews_today"`
	TokensToday  int64 `json:"tokens_today"`
	RepoCount    int   `json:"repo_count"`
	MemberCount  int   `json:"member_count"`
}

// ── Quota Check Results ─────────────────────────────────────────────────────

// CheckResult represents the outcome of a quota check.
type CheckResult struct {
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason,omitempty"`
	Limit     int    `json:"limit"`
	Current   int    `json:"current"`
	Remaining int    `json:"remaining"`
}

// ── Enforcement Errors ──────────────────────────────────────────────────────

// QuotaExceededError is returned when a quota limit is reached.
type QuotaExceededError struct {
	Resource string `json:"resource"`
	Limit    int    `json:"limit"`
	Current  int    `json:"current"`
	Plan     string `json:"plan"`
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded: %s limit is %d on %s plan (current: %d)", e.Resource, e.Limit, e.Plan, e.Current)
}

// ── Enforcer ────────────────────────────────────────────────────────────────

// Enforcer checks and enforces plan-based quotas.
type Enforcer struct {
	pool *pgxpool.Pool
}

// NewEnforcer creates a quota enforcer with database access.
func NewEnforcer(pool *pgxpool.Pool) *Enforcer {
	return &Enforcer{pool: pool}
}

// GetLimits returns the limits for a given plan.
func GetLimits(plan string) PlanLimits {
	if limits, ok := DefaultLimits[plan]; ok {
		return limits
	}
	return DefaultLimits["free"]
}

// CheckReviewQuota checks whether the organization can trigger another review.
func (e *Enforcer) CheckReviewQuota(ctx context.Context, orgID uuid.UUID, plan string) (*CheckResult, error) {
	limits := GetLimits(plan)
	if limits.ReviewsPerDay < 0 {
		return &CheckResult{Allowed: true, Limit: -1, Current: 0, Remaining: -1}, nil
	}

	today := time.Now().UTC().Format("2006-01-02")
	var count int
	err := e.pool.QueryRow(ctx,
		`SELECT COALESCE(reviews_count, 0) FROM usage_daily
		 WHERE org_id = $1 AND date = $2`, orgID, today,
	).Scan(&count)
	if err != nil {
		count = 0 // no entry = no usage today
	}

	remaining := limits.ReviewsPerDay - count
	if remaining < 0 {
		remaining = 0
	}
	if count >= limits.ReviewsPerDay {
		return &CheckResult{
			Allowed: false, Limit: limits.ReviewsPerDay, Current: count, Remaining: 0,
			Reason: fmt.Sprintf("daily review limit of %d reached on %s plan", limits.ReviewsPerDay, plan),
		}, nil
	}
	return &CheckResult{Allowed: true, Limit: limits.ReviewsPerDay, Current: count, Remaining: remaining}, nil
}

// CheckRepoQuota checks whether the organization can add another repository.
func (e *Enforcer) CheckRepoQuota(ctx context.Context, orgID uuid.UUID, plan string) (*CheckResult, error) {
	limits := GetLimits(plan)
	if limits.ReposPerOrg < 0 {
		return &CheckResult{Allowed: true, Limit: -1, Current: 0, Remaining: -1}, nil
	}

	var count int
	err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repositories WHERE org_id = $1`, orgID,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count repos: %w", err)
	}

	remaining := limits.ReposPerOrg - count
	if remaining < 0 {
		remaining = 0
	}
	if count >= limits.ReposPerOrg {
		return &CheckResult{
			Allowed: false, Limit: limits.ReposPerOrg, Current: count, Remaining: 0,
			Reason: fmt.Sprintf("repository limit of %d reached on %s plan", limits.ReposPerOrg, plan),
		}, nil
	}
	return &CheckResult{Allowed: true, Limit: limits.ReposPerOrg, Current: count, Remaining: remaining}, nil
}

// CheckMemberQuota checks whether the organization can add another member.
func (e *Enforcer) CheckMemberQuota(ctx context.Context, orgID uuid.UUID, plan string) (*CheckResult, error) {
	limits := GetLimits(plan)
	if limits.MembersPerOrg < 0 {
		return &CheckResult{Allowed: true, Limit: -1, Current: 0, Remaining: -1}, nil
	}

	var count int
	err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org_id = $1`, orgID,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count members: %w", err)
	}

	remaining := limits.MembersPerOrg - count
	if remaining < 0 {
		remaining = 0
	}
	if count >= limits.MembersPerOrg {
		return &CheckResult{
			Allowed: false, Limit: limits.MembersPerOrg, Current: count, Remaining: 0,
			Reason: fmt.Sprintf("member limit of %d reached on %s plan", limits.MembersPerOrg, plan),
		}, nil
	}
	return &CheckResult{Allowed: true, Limit: limits.MembersPerOrg, Current: count, Remaining: remaining}, nil
}

// CheckIndexingAllowed checks if indexing is available on the plan.
func (e *Enforcer) CheckIndexingAllowed(plan string) *CheckResult {
	limits := GetLimits(plan)
	if !limits.IndexingEnabled {
		return &CheckResult{
			Allowed: false, Limit: 0, Current: 0, Remaining: 0,
			Reason: fmt.Sprintf("indexing is not available on %s plan", plan),
		}
	}
	return &CheckResult{Allowed: true, Limit: 1, Current: 0, Remaining: 1}
}

// IncrementUsage increments the daily usage counters.
func (e *Enforcer) IncrementUsage(ctx context.Context, orgID uuid.UUID, reviews int, tokens int64) error {
	today := time.Now().UTC().Format("2006-01-02")
	_, err := e.pool.Exec(ctx,
		`INSERT INTO usage_daily (org_id, date, reviews_count, tokens_used)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (org_id, date) DO UPDATE
		 SET reviews_count = usage_daily.reviews_count + $3,
		     tokens_used   = usage_daily.tokens_used + $4`,
		orgID, today, reviews, tokens,
	)
	if err != nil {
		return fmt.Errorf("increment usage: %w", err)
	}
	return nil
}

// GetUsage returns the current usage for an organization.
func (e *Enforcer) GetUsage(ctx context.Context, orgID uuid.UUID) (*Usage, error) {
	usage := &Usage{}

	today := time.Now().UTC().Format("2006-01-02")
	err := e.pool.QueryRow(ctx,
		`SELECT COALESCE(reviews_count, 0), COALESCE(tokens_used, 0) FROM usage_daily
		 WHERE org_id = $1 AND date = $2`, orgID, today,
	).Scan(&usage.ReviewsToday, &usage.TokensToday)
	if err != nil {
		// No usage today
		usage.ReviewsToday = 0
		usage.TokensToday = 0
	}

	_ = e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repositories WHERE org_id = $1`, orgID,
	).Scan(&usage.RepoCount)

	_ = e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org_id = $1`, orgID,
	).Scan(&usage.MemberCount)

	return usage, nil
}
