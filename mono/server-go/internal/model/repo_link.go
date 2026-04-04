package model

import (
	"time"

	"github.com/google/uuid"
)

// ── Share Profile Constants ─────────────────────────────────────────────────

// ShareProfile controls the scope of data exposure from a target repo
// to users operating in the source repo context.
const (
	// ShareFull exposes all indexed data: search results, symbols, file content.
	ShareFull = "full"

	// ShareSymbols exposes only exported symbols, function signatures, and types.
	ShareSymbols = "symbols"

	// ShareMetadata exposes only the repo manifest: language, build system, dependencies.
	ShareMetadata = "metadata"

	// ShareNone means the link exists but sharing is paused (soft-disable).
	ShareNone = "none"
)

// ValidShareProfiles is the set of allowed share_profile values.
var ValidShareProfiles = map[string]bool{
	ShareFull:     true,
	ShareSymbols:  true,
	ShareMetadata: true,
	ShareNone:     true,
}

// ── Cross-Repo Action Constants ─────────────────────────────────────────────

// CrossRepoAction represents an operation that requires cross-repo authorization.
const (
	ActionCrossRepoSearch     = "cross_repo.search"      // federated search across linked repos
	ActionCrossRepoSymbols    = "cross_repo.symbols"     // read exported symbols from target
	ActionCrossRepoMetadata   = "cross_repo.metadata"    // read repo manifest / dependency graph
	ActionCrossRepoFileRead   = "cross_repo.file_read"   // read file content from target
	ActionCrossRepoGraphView  = "cross_repo.graph_view"  // view the cross-repo dependency graph
	ActionCrossRepoChatMention = "cross_repo.chat_mention" // mention target repo in chat (@repo)
)

// ── RepoLink ────────────────────────────────────────────────────────────────

// RepoLink represents a directed relationship between two repositories
// within the same organization. The share_profile controls what data from
// the target repo is visible to users operating in the source repo.
type RepoLink struct {
	ID           uuid.UUID `json:"id" db:"id"`
	OrgID        uuid.UUID `json:"org_id" db:"org_id"`
	SourceRepoID uuid.UUID `json:"source_repo_id" db:"source_repo_id"`
	TargetRepoID uuid.UUID `json:"target_repo_id" db:"target_repo_id"`
	ShareProfile string    `json:"share_profile" db:"share_profile"` // full, symbols, metadata, none
	Label        string    `json:"label,omitempty" db:"label"`
	CreatedBy    uuid.UUID `json:"created_by" db:"created_by"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// IsActive returns true if the link is not paused (share_profile != "none").
func (l *RepoLink) IsActive() bool {
	return l.ShareProfile != ShareNone
}

// AllowsAction returns true if the share profile permits the given action.
func (l *RepoLink) AllowsAction(action string) bool {
	switch l.ShareProfile {
	case ShareFull:
		return true // all actions allowed
	case ShareSymbols:
		switch action {
		case ActionCrossRepoSymbols, ActionCrossRepoMetadata, ActionCrossRepoGraphView, ActionCrossRepoChatMention:
			return true
		default:
			return false
		}
	case ShareMetadata:
		switch action {
		case ActionCrossRepoMetadata, ActionCrossRepoGraphView:
			return true
		default:
			return false
		}
	case ShareNone:
		return false
	default:
		return false
	}
}

// RepoLinkEvent represents an audit entry for a cross-repo link mutation.
type RepoLinkEvent struct {
	ID           uuid.UUID              `json:"id" db:"id"`
	LinkID       *uuid.UUID             `json:"link_id,omitempty" db:"link_id"`
	OrgID        uuid.UUID              `json:"org_id" db:"org_id"`
	SourceRepoID uuid.UUID              `json:"source_repo_id" db:"source_repo_id"`
	TargetRepoID uuid.UUID              `json:"target_repo_id" db:"target_repo_id"`
	Action       string                 `json:"action" db:"action"`
	ActorID      *uuid.UUID             `json:"actor_id,omitempty" db:"actor_id"`
	Metadata     map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
	CreatedAt    time.Time              `json:"created_at" db:"created_at"`
}

// RepoLinkWithNames is a view model that includes human-readable repo names
// for display in the UI (avoids N+1 queries in list endpoints).
type RepoLinkWithNames struct {
	RepoLink
	SourceRepoName string `json:"source_repo_name"`
	TargetRepoName string `json:"target_repo_name"`
}
