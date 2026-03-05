package quota_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/quota"
)

// ── Plan Limits Tests ───────────────────────────────────────────────────────

func TestGetLimits_KnownPlans(t *testing.T) {
	plans := []string{"free", "pro", "enterprise"}
	for _, plan := range plans {
		limits := quota.GetLimits(plan)
		if plan == "free" {
			if limits.ReviewsPerDay != 10 {
				t.Errorf("free plan reviews_per_day = %d, want 10", limits.ReviewsPerDay)
			}
			if limits.ReposPerOrg != 5 {
				t.Errorf("free plan repos_per_org = %d, want 5", limits.ReposPerOrg)
			}
			if limits.MembersPerOrg != 5 {
				t.Errorf("free plan members_per_org = %d, want 5", limits.MembersPerOrg)
			}
			if limits.TokensPerDay != 100_000 {
				t.Errorf("free plan tokens_per_day = %d, want 100000", limits.TokensPerDay)
			}
			if limits.IndexingEnabled {
				t.Error("free plan should not have indexing enabled")
			}
		}
		if plan == "pro" {
			if limits.ReviewsPerDay != 100 {
				t.Errorf("pro plan reviews_per_day = %d, want 100", limits.ReviewsPerDay)
			}
			if limits.ReposPerOrg != 50 {
				t.Errorf("pro plan repos_per_org = %d, want 50", limits.ReposPerOrg)
			}
			if !limits.IndexingEnabled {
				t.Error("pro plan should have indexing enabled")
			}
		}
		if plan == "enterprise" {
			if limits.ReviewsPerDay != -1 {
				t.Errorf("enterprise plan reviews_per_day = %d, want -1 (unlimited)", limits.ReviewsPerDay)
			}
			if limits.ReposPerOrg != -1 {
				t.Errorf("enterprise plan repos_per_org = %d, want -1 (unlimited)", limits.ReposPerOrg)
			}
			if limits.MembersPerOrg != -1 {
				t.Errorf("enterprise plan members_per_org = %d, want -1 (unlimited)", limits.MembersPerOrg)
			}
			if limits.TokensPerDay != -1 {
				t.Errorf("enterprise plan tokens_per_day = %d, want -1 (unlimited)", limits.TokensPerDay)
			}
			if !limits.IndexingEnabled {
				t.Error("enterprise plan should have indexing enabled")
			}
		}
	}
}

func TestGetLimits_UnknownPlan(t *testing.T) {
	limits := quota.GetLimits("nonexistent")
	freeLimits := quota.GetLimits("free")
	if limits.ReviewsPerDay != freeLimits.ReviewsPerDay {
		t.Error("unknown plan should default to free plan limits")
	}
}

func TestCheckIndexingAllowed(t *testing.T) {
	enforcer := quota.NewEnforcer(nil) // nil pool is fine — no DB call

	// Free plan: indexing disabled.
	result := enforcer.CheckIndexingAllowed("free")
	if result.Allowed {
		t.Error("free plan should not allow indexing")
	}
	if result.Reason == "" {
		t.Error("expected reason for denied indexing")
	}

	// Pro plan: indexing enabled.
	result = enforcer.CheckIndexingAllowed("pro")
	if !result.Allowed {
		t.Error("pro plan should allow indexing")
	}

	// Enterprise plan: indexing enabled.
	result = enforcer.CheckIndexingAllowed("enterprise")
	if !result.Allowed {
		t.Error("enterprise plan should allow indexing")
	}
}

func TestDefaultLimits_Completeness(t *testing.T) {
	expectedPlans := []string{"free", "pro", "enterprise"}
	for _, plan := range expectedPlans {
		if _, ok := quota.DefaultLimits[plan]; !ok {
			t.Errorf("missing default limits for plan %q", plan)
		}
	}
}

func TestPlanLimits_FreeIsMoreRestrictiveThanPro(t *testing.T) {
	free := quota.GetLimits("free")
	pro := quota.GetLimits("pro")

	if free.ReviewsPerDay >= pro.ReviewsPerDay {
		t.Error("free plan should have fewer reviews than pro")
	}
	if free.ReposPerOrg >= pro.ReposPerOrg {
		t.Error("free plan should have fewer repos than pro")
	}
	if free.MembersPerOrg >= pro.MembersPerOrg {
		t.Error("free plan should have fewer members than pro")
	}
	if free.TokensPerDay >= pro.TokensPerDay {
		t.Error("free plan should have fewer tokens than pro")
	}
	if free.MaxFileSizeKB >= pro.MaxFileSizeKB {
		t.Error("free plan should have smaller max file size than pro")
	}
}

func TestCheckResult_Structure(t *testing.T) {
	result := &quota.CheckResult{
		Allowed:   false,
		Reason:    "limit reached",
		Limit:     10,
		Current:   10,
		Remaining: 0,
	}
	if result.Allowed {
		t.Error("expected not allowed")
	}
	if result.Remaining != 0 {
		t.Errorf("expected 0 remaining, got %d", result.Remaining)
	}
}

func TestQuotaExceededError(t *testing.T) {
	err := &quota.QuotaExceededError{
		Resource: "reviews",
		Limit:    10,
		Current:  10,
		Plan:     "free",
	}
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should return non-empty string")
	}
	expected := "quota exceeded: reviews limit is 10 on free plan (current: 10)"
	if msg != expected {
		t.Errorf("got %q, want %q", msg, expected)
	}
}

func TestNewEnforcer_NilPool(t *testing.T) {
	// Should not panic with nil pool.
	enforcer := quota.NewEnforcer(nil)
	if enforcer == nil {
		t.Error("NewEnforcer should return non-nil enforcer even with nil pool")
	}
}
