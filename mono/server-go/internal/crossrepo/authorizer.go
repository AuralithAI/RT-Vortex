// Package crossrepo implements the Cross-Repo Observatory authorization
// and linking subsystem.
//
// The Authorizer is the single source of truth for all cross-repo access
// decisions. Every handler, swarm task, chat parser, and graph renderer
// that needs to access data from a linked repo MUST call Authorizer.Authorize()
// before proceeding.
//
// Authorization is a three-layer matrix check:
//
//  1. Org ceiling policy — org.Settings["cross_repo_enabled"] must be true.
//     If the org has cross-repo disabled, all requests are denied.
//
//  2. Link share profile — the repo_links row must exist and its share_profile
//     must permit the requested action. (e.g., "metadata" profile allows
//     ActionCrossRepoMetadata but NOT ActionCrossRepoSearch.)
//
//  3. User access — the user must have at least "member" role in the source
//     repo's org. For "full" profile actions (file_read, search), the user
//     must also have explicit access to the target repo (via repo_members or
//     org membership).
//
// The most-restrictive-wins policy is enforced at every layer.
package crossrepo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── Well-known denial reasons ───────────────────────────────────────────────

var (
	ErrCrossRepoDisabled    = errors.New("cross-repo features are disabled for this organization")
	ErrLinkNotFound         = errors.New("no link exists between these repositories")
	ErrLinkPaused           = errors.New("cross-repo link is paused (share_profile=none)")
	ErrActionNotPermitted   = errors.New("share profile does not permit this action")
	ErrUserNotInOrg         = errors.New("user is not a member of the organization")
	ErrUserNoTargetAccess   = errors.New("user does not have access to the target repository")
	ErrRepoNotInOrg         = errors.New("repository does not belong to the specified organization")
	ErrMaxLinksReached      = errors.New("organization has reached the maximum number of cross-repo links")
	ErrInvalidShareProfile  = errors.New("invalid share profile value")
)

// ── Decision ────────────────────────────────────────────────────────────────

// Decision is the result of an authorization check.
type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"` // human-readable denial reason
}

// Allow returns a permissive decision.
func Allow() Decision { return Decision{Allowed: true} }

// Deny returns a denial decision with the given reason.
func Deny(reason string) Decision { return Decision{Allowed: false, Reason: reason} }

// ── Authorizer ──────────────────────────────────────────────────────────────

// Authorizer is the centralized cross-repo authorization service.
// It is safe for concurrent use.
type Authorizer struct {
	orgRepo      *store.OrgRepository
	repoRepo     *store.RepositoryRepo
	repoLinkRepo *store.RepoLinkRepo
	orgMember    *store.OrgRepository   // reuse for GetMember
	repoMember   *store.RepoMemberRepo
}

// NewAuthorizer creates a new cross-repo Authorizer.
func NewAuthorizer(
	orgRepo *store.OrgRepository,
	repoRepo *store.RepositoryRepo,
	repoLinkRepo *store.RepoLinkRepo,
	repoMemberRepo *store.RepoMemberRepo,
) *Authorizer {
	return &Authorizer{
		orgRepo:      orgRepo,
		repoRepo:     repoRepo,
		repoLinkRepo: repoLinkRepo,
		orgMember:    orgRepo,
		repoMember:   repoMemberRepo,
	}
}

// Authorize checks whether the given user is allowed to perform the specified
// cross-repo action from sourceRepoID against targetRepoID.
//
// This is the ONLY function that should be called to make cross-repo access
// decisions. All other code paths (handlers, swarm, chat) delegate here.
func (a *Authorizer) Authorize(ctx context.Context, userID, sourceRepoID, targetRepoID uuid.UUID, action string) Decision {
	// ── Layer 1: Load both repos and verify they're in the same org ─────
	sourceRepo, err := a.repoRepo.GetByID(ctx, sourceRepoID)
	if err != nil {
		slog.Warn("crossrepo.authorize: source repo not found",
			"source_repo_id", sourceRepoID, "error", err)
		return Deny("source repository not found")
	}

	targetRepo, err := a.repoRepo.GetByID(ctx, targetRepoID)
	if err != nil {
		slog.Warn("crossrepo.authorize: target repo not found",
			"target_repo_id", targetRepoID, "error", err)
		return Deny("target repository not found")
	}

	if sourceRepo.OrgID != targetRepo.OrgID {
		return Deny("repositories belong to different organizations")
	}

	orgID := sourceRepo.OrgID

	// ── Layer 2: Check org ceiling policy ───────────────────────────────
	org, err := a.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		slog.Warn("crossrepo.authorize: org not found", "org_id", orgID, "error", err)
		return Deny("organization not found")
	}

	if !a.IsOrgCrossRepoEnabled(org) {
		return Deny(ErrCrossRepoDisabled.Error())
	}

	// ── Layer 3: Check the link exists and profile permits the action ───
	link, err := a.repoLinkRepo.GetByRepos(ctx, orgID, sourceRepoID, targetRepoID)
	if errors.Is(err, store.ErrNotFound) {
		return Deny(ErrLinkNotFound.Error())
	}
	if err != nil {
		slog.Error("crossrepo.authorize: db error loading link",
			"source", sourceRepoID, "target", targetRepoID, "error", err)
		return Deny("internal error checking repo link")
	}

	if !link.IsActive() {
		return Deny(ErrLinkPaused.Error())
	}

	if !link.AllowsAction(action) {
		return Deny(fmt.Sprintf("%s: profile=%q does not permit action=%q",
			ErrActionNotPermitted.Error(), link.ShareProfile, action))
	}

	// ── Layer 4: Check user is a member of the org ──────────────────────
	member, err := a.orgMember.GetMember(ctx, orgID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return Deny(ErrUserNotInOrg.Error())
	}
	if err != nil {
		slog.Error("crossrepo.authorize: db error checking org membership",
			"org_id", orgID, "user_id", userID, "error", err)
		return Deny("internal error checking org membership")
	}

	// Viewers can only access metadata-level actions.
	if member.Role == "viewer" && action != model.ActionCrossRepoMetadata && action != model.ActionCrossRepoGraphView {
		return Deny("viewer role can only access metadata and graph views")
	}

	// ── Layer 5: For full-access actions, verify user has target repo access ─
	if RequiresTargetRepoAccess(action) {
		if !a.userHasTargetAccess(ctx, orgID, targetRepoID, userID, member.Role) {
			return Deny(ErrUserNoTargetAccess.Error())
		}
	}

	return Allow()
}

// AuthorizeLink checks whether a user is allowed to CREATE or MODIFY a link.
// Requires org admin or owner role.
func (a *Authorizer) AuthorizeLink(ctx context.Context, userID, orgID uuid.UUID) Decision {
	// Check org ceiling policy.
	org, err := a.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		return Deny("organization not found")
	}

	if !a.IsOrgCrossRepoEnabled(org) {
		return Deny(ErrCrossRepoDisabled.Error())
	}

	// Only org owner or admin can manage links.
	member, err := a.orgMember.GetMember(ctx, orgID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return Deny(ErrUserNotInOrg.Error())
	}
	if err != nil {
		return Deny("internal error checking membership")
	}

	switch member.Role {
	case "owner", "admin":
		return Allow()
	default:
		return Deny("only org owners and admins can manage cross-repo links")
	}
}

// CheckOrgLinkBudget verifies the org hasn't exceeded its max_linked_repos setting.
func (a *Authorizer) CheckOrgLinkBudget(ctx context.Context, orgID uuid.UUID) Decision {
	org, err := a.orgRepo.GetByID(ctx, orgID)
	if err != nil {
		return Deny("organization not found")
	}

	maxLinks := a.GetOrgMaxLinks(org)
	if maxLinks <= 0 {
		return Allow() // no limit
	}

	count, err := a.repoLinkRepo.CountByOrg(ctx, orgID)
	if err != nil {
		slog.Error("crossrepo.budget: db error", "org_id", orgID, "error", err)
		return Deny("internal error checking link budget")
	}

	if count >= maxLinks {
		return Deny(fmt.Sprintf("%s: %d/%d links used",
			ErrMaxLinksReached.Error(), count, maxLinks))
	}

	return Allow()
}

// ── Internal helpers ────────────────────────────────────────────────────────

// IsOrgCrossRepoEnabled checks the org's Settings JSONB for cross_repo_enabled.
// Defaults to false if not set (opt-in model).
func (a *Authorizer) IsOrgCrossRepoEnabled(org *model.Organization) bool {
	if org.Settings == nil {
		return false
	}
	enabled, ok := org.Settings["cross_repo_enabled"]
	if !ok {
		return false
	}
	b, ok := enabled.(bool)
	return ok && b
}

// GetOrgMaxLinks reads the max_linked_repos setting from org Settings.
// Returns 0 (no limit) if not set.
func (a *Authorizer) GetOrgMaxLinks(org *model.Organization) int {
	if org.Settings == nil {
		return 0
	}
	v, ok := org.Settings["max_linked_repos"]
	if !ok {
		return 0
	}
	// JSONB numbers come through as float64.
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// RequiresTargetRepoAccess returns true if the action needs explicit access
// to the target repo (beyond just org membership).
func RequiresTargetRepoAccess(action string) bool {
	switch action {
	case model.ActionCrossRepoSearch, model.ActionCrossRepoFileRead:
		return true
	default:
		// Metadata, symbols, graph view, and chat mention don't require
		// explicit target repo membership — org membership + link profile suffice.
		return false
	}
}

// userHasTargetAccess checks if the user can access the target repo.
// Org owners and admins always have access. Others need explicit repo_members entry.
func (a *Authorizer) userHasTargetAccess(ctx context.Context, orgID, targetRepoID, userID uuid.UUID, orgRole string) bool {
	// Org owners and admins have implicit access to all repos in the org.
	switch orgRole {
	case "owner", "admin":
		return true
	}

	// Check explicit repo_members entry.
	repos, err := a.repoMember.ListReposByUser(ctx, userID)
	if err != nil {
		slog.Error("crossrepo.authorize: error listing user repos",
			"user_id", userID, "error", err)
		return false
	}

	for _, rid := range repos {
		if rid == targetRepoID {
			return true
		}
	}

	return false
}
