package swarm

// ── Dynamic Team Formation ─────────────────────────────────────────
//
// The team formation engine analyses a task's plan document, computes a
// multi-dimensional complexity score, queries role-based ELO tiers, and
// produces an optimal team composition.  The pipeline:
//
//   1. Extract input signals from the plan (file count, step count, language
//      spread, test coverage, cross-package changes, etc.)
//   2. Compute a normalised complexity score (0.0–1.0)
//   3. Map the score to a complexity label (trivial/small/medium/large/critical)
//   4. Select recommended roles and team size based on complexity + signal flags
//   5. Query role-based ELO to retrieve each role's tier + score for the repo
//   6. Adjust recommendations (prefer expert-tier roles, add training slots
//      for restricted-tier roles)
//   7. Return a TeamFormation struct that's stored on the task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Constants ───────────────────────────────────────────────────────────────
//
// Note: ComplexitySmall, ComplexityMedium, ComplexityLarge are already defined
// in task_manager.go as type TaskComplexity.
// TierStandard, TierExpert, TierRestricted are already defined in elo_auto_tier.go.
// We add only the labels that don't exist yet.

const (
	// Additional complexity labels not in task_manager.go.
	FormationComplexityTrivial   = "trivial"
	FormationComplexityCritical  = "critical"

	// Complexity label strings for matching (use string values of existing constants).
	formationLabelSmall  = "small"
	formationLabelMedium = "medium"
	formationLabelLarge  = "large"

	StrategyELOWeighted = "elo_weighted"
	StrategyStatic      = "static"
)

// Complexity thresholds (normalised 0.0–1.0).
const (
	ThresholdTrivial = 0.15
	ThresholdSmall   = 0.35
	ThresholdMedium  = 0.60
	ThresholdLarge   = 0.80
	// ≥ 0.80 = critical
)

// ── Types ───────────────────────────────────────────────────────────────────

// ComplexitySignals are raw inputs extracted from the plan document.
type ComplexitySignals struct {
	FileCount         int      `json:"file_count"`
	StepCount         int      `json:"step_count"`
	DescriptionLength int      `json:"description_length"`
	LanguageCount     int      `json:"language_count"`
	TestFiles         int      `json:"test_files"`
	HasMigrations     bool     `json:"has_migrations"`
	CrossPackage      bool     `json:"cross_package"`
	HasAPIChanges     bool     `json:"has_api_changes"`
	HasSecurityImpact bool     `json:"has_security_impact"`
	HasUIChanges      bool     `json:"has_ui_changes"`
	Languages         []string `json:"languages,omitempty"`
}

// RoleELOInfo is the ELO summary for a role in the context of this repo.
type RoleELOInfo struct {
	ELO  float64 `json:"elo"`
	Tier string  `json:"tier"`
}

// TeamFormation is the full recommendation stored on the task.
type TeamFormation struct {
	ComplexityScore  float64                `json:"complexity_score"`
	ComplexityLabel  string                 `json:"complexity_label"`
	InputSignals     ComplexitySignals      `json:"input_signals"`
	RecommendedRoles []string               `json:"recommended_roles"`
	RoleELOs         map[string]RoleELOInfo `json:"role_elos"`
	TeamSize         int                    `json:"team_size"` // includes orchestrator
	Reasoning        string                 `json:"reasoning"`
	Strategy         string                 `json:"strategy"`
	CreatedAt        time.Time              `json:"created_at"`
}

// TeamRecommendRequest is the input for POST /internal/swarm/team-recommend.
type TeamRecommendRequest struct {
	TaskID string          `json:"task_id"`
	RepoID string          `json:"repo_id"`
	Plan   json.RawMessage `json:"plan,omitempty"`
	// Override signals from the Python side (optional).
	Signals *ComplexitySignals `json:"signals,omitempty"`
}

// ── Team Formation Service ──────────────────────────────────────────────────

// TeamFormationService computes complexity and recommends teams.
type TeamFormationService struct {
	db      *pgxpool.Pool
	roleELO *RoleELOService
}

// NewTeamFormationService creates a new instance.
func NewTeamFormationService(db *pgxpool.Pool, roleELO *RoleELOService) *TeamFormationService {
	return &TeamFormationService{db: db, roleELO: roleELO}
}

// RecommendTeam analyses a plan and returns a TeamFormation.
func (s *TeamFormationService) RecommendTeam(
	ctx context.Context,
	repoID string,
	plan json.RawMessage,
	overrideSignals *ComplexitySignals,
) (*TeamFormation, error) {
	// 1. Extract signals from the plan.
	var signals ComplexitySignals
	if overrideSignals != nil {
		signals = *overrideSignals
	} else {
		signals = ExtractSignalsFromPlan(plan)
	}

	// 2. Compute normalised complexity score.
	score := ComputeComplexityScore(signals)

	// 3. Map to label.
	label := FormationComplexityLabel(score)

	// 4. Determine recommended roles.
	roles := RecommendRoles(label, signals)

	// 5. Query ELO tiers for each role.
	roleELOs := make(map[string]RoleELOInfo)
	if s.roleELO != nil {
		for _, role := range roles {
			tier, elo := s.roleELO.TierForRole(ctx, role, repoID)
			roleELOs[role] = RoleELOInfo{ELO: elo, Tier: tier}
		}
	}

	// 6. ELO-aware adjustments.
	roles, reasoning := s.eloAdjustments(roles, roleELOs, label, signals)

	// 7. Build result.
	teamSize := len(roles) + 1 // +1 for orchestrator
	formation := &TeamFormation{
		ComplexityScore:  math.Round(score*1000) / 1000,
		ComplexityLabel:  label,
		InputSignals:     signals,
		RecommendedRoles: roles,
		RoleELOs:         roleELOs,
		TeamSize:         teamSize,
		Reasoning:        reasoning,
		Strategy:         StrategyELOWeighted,
		CreatedAt:        time.Now().UTC(),
	}

	return formation, nil
}

// StoreFormation persists the team formation on the task row.
func (s *TeamFormationService) StoreFormation(
	ctx context.Context,
	taskID uuid.UUID,
	formation *TeamFormation,
) error {
	data, err := json.Marshal(formation)
	if err != nil {
		return fmt.Errorf("marshalling team formation: %w", err)
	}
	_, err = s.db.Exec(ctx, `
		UPDATE swarm_tasks SET team_formation = $1, updated_at = NOW()
		WHERE id = $2`,
		data, taskID,
	)
	if err != nil {
		return fmt.Errorf("storing team formation for task %s: %w", taskID, err)
	}
	return nil
}

// ── Signal Extraction ───────────────────────────────────────────────────────

// ExtractSignalsFromPlan parses the plan JSON and extracts complexity signals.
func ExtractSignalsFromPlan(planJSON json.RawMessage) ComplexitySignals {
	if len(planJSON) == 0 {
		return ComplexitySignals{}
	}

	var plan struct {
		Summary             string            `json:"summary"`
		Steps               []json.RawMessage `json:"steps"`
		AffectedFiles       []string          `json:"affected_files"`
		EstimatedComplexity string            `json:"estimated_complexity"`
		AgentsNeeded        json.RawMessage   `json:"agents_needed"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		slog.Debug("team formation: cannot parse plan", "error", err)
		return ComplexitySignals{}
	}

	signals := ComplexitySignals{
		FileCount:         len(plan.AffectedFiles),
		StepCount:         len(plan.Steps),
		DescriptionLength: len(plan.Summary),
	}

	// Analyse file extensions for language spread + special flags.
	langSet := make(map[string]bool)
	for _, f := range plan.AffectedFiles {
		ext := formationFileExtension(f)
		lang := formationExtensionToLanguage(ext)
		if lang != "" {
			langSet[lang] = true
		}

		lower := strings.ToLower(f)

		// Detect test files.
		if formationIsTestFile(lower) {
			signals.TestFiles++
		}

		// Detect migrations.
		if strings.Contains(lower, "migration") || strings.Contains(lower, "migrate") {
			signals.HasMigrations = true
		}

		// Detect API changes.
		if strings.Contains(lower, "handler") || strings.Contains(lower, "route") ||
			strings.Contains(lower, "endpoint") || strings.Contains(lower, "api") ||
			strings.Contains(lower, "proto") || strings.Contains(lower, "schema") {
			signals.HasAPIChanges = true
		}

		// Detect UI changes.
		if strings.Contains(lower, "component") || strings.Contains(lower, "page.tsx") ||
			strings.Contains(lower, ".css") || strings.Contains(lower, ".scss") ||
			strings.Contains(lower, "/web/") || strings.Contains(lower, "/ui/") {
			signals.HasUIChanges = true
		}
	}

	// Detect cross-package changes by counting distinct top-level directories.
	topDirs := make(map[string]bool)
	for _, f := range plan.AffectedFiles {
		parts := strings.SplitN(f, "/", 3)
		if len(parts) >= 2 {
			topDirs[parts[0]+"/"+parts[1]] = true
		}
	}
	signals.CrossPackage = len(topDirs) >= 3

	// Detect security impact from summary keywords.
	summaryLower := strings.ToLower(plan.Summary)
	securityKeywords := []string{"auth", "security", "permission", "rbac", "jwt", "token", "encrypt", "credential", "secret", "vulnerability", "cve"}
	for _, kw := range securityKeywords {
		if strings.Contains(summaryLower, kw) {
			signals.HasSecurityImpact = true
			break
		}
	}

	// Populate language info.
	for lang := range langSet {
		signals.Languages = append(signals.Languages, lang)
	}
	sort.Strings(signals.Languages)
	signals.LanguageCount = len(signals.Languages)

	return signals
}

// ── Complexity Score ────────────────────────────────────────────────────────

// ComputeComplexityScore calculates a normalised 0.0–1.0 complexity score.
// Uses a weighted sum of normalised input dimensions.
func ComputeComplexityScore(s ComplexitySignals) float64 {
	// Normalise each dimension to 0.0–1.0 using sigmoid-like functions.
	fileDim := formationSigmoid(float64(s.FileCount), 8)           // 50% at 8 files
	stepDim := formationSigmoid(float64(s.StepCount), 6)           // 50% at 6 steps
	descDim := formationSigmoid(float64(s.DescriptionLength), 300) // 50% at 300 chars
	langDim := formationSigmoid(float64(s.LanguageCount), 2)       // 50% at 2 languages

	// Binary flags contribute fixed bumps.
	var flagScore float64
	if s.HasMigrations {
		flagScore += 0.15
	}
	if s.CrossPackage {
		flagScore += 0.12
	}
	if s.HasAPIChanges {
		flagScore += 0.10
	}
	if s.HasSecurityImpact {
		flagScore += 0.10
	}
	if s.HasUIChanges {
		flagScore += 0.05
	}
	if s.TestFiles > 0 {
		// Test files increase complexity but also indicate quality intent.
		flagScore += 0.03 * math.Min(float64(s.TestFiles), 3)
	}

	// Weighted sum.
	raw := 0.30*fileDim +
		0.25*stepDim +
		0.10*descDim +
		0.10*langDim +
		0.25*math.Min(flagScore, 0.50) // cap flag contribution

	// Clamp to [0, 1].
	return math.Max(0, math.Min(1, raw))
}

// FormationComplexityLabel maps a score to a human-readable label string.
func FormationComplexityLabel(score float64) string {
	switch {
	case score < ThresholdTrivial:
		return FormationComplexityTrivial
	case score < ThresholdSmall:
		return formationLabelSmall
	case score < ThresholdMedium:
		return formationLabelMedium
	case score < ThresholdLarge:
		return formationLabelLarge
	default:
		return FormationComplexityCritical
	}
}

// ── Role Recommendation ─────────────────────────────────────────────────────

// RecommendRoles returns the recommended agent roles based on complexity and signals.
func RecommendRoles(label string, signals ComplexitySignals) []string {
	switch label {
	case FormationComplexityTrivial:
		return []string{"senior_dev"}

	case formationLabelSmall:
		roles := []string{"senior_dev"}
		if signals.TestFiles > 0 || signals.HasAPIChanges {
			roles = append(roles, "qa")
		}
		return roles

	case formationLabelMedium:
		roles := []string{"senior_dev", "qa"}
		if signals.HasSecurityImpact {
			roles = append(roles, "security")
		}
		if signals.HasUIChanges {
			roles = append(roles, "ui_ux")
		}
		return roles

	case formationLabelLarge:
		roles := []string{"architect", "senior_dev", "junior_dev", "qa", "security"}
		if signals.HasUIChanges {
			roles = append(roles, "ui_ux")
		}
		return roles

	case FormationComplexityCritical:
		roles := []string{"architect", "senior_dev", "senior_dev", "junior_dev", "qa", "security"}
		if signals.HasUIChanges {
			roles = append(roles, "ui_ux")
		}
		if signals.HasMigrations || signals.HasAPIChanges {
			roles = append(roles, "docs")
		}
		return roles

	default:
		return []string{"senior_dev", "qa"}
	}
}

// ── ELO-Aware Adjustments ───────────────────────────────────────────────────

// eloAdjustments modifies the role list based on ELO tiers and produces reasoning.
func (s *TeamFormationService) eloAdjustments(
	roles []string,
	roleELOs map[string]RoleELOInfo,
	label string,
	signals ComplexitySignals,
) ([]string, string) {
	var reasonParts []string
	reasonParts = append(reasonParts, fmt.Sprintf(
		"%s complexity: %d files, %d steps, %d languages",
		formationTitleCase(label), signals.FileCount, signals.StepCount, signals.LanguageCount,
	))

	// Count tiers.
	expertCount := 0
	restrictedCount := 0
	for _, info := range roleELOs {
		switch info.Tier {
		case TierExpert:
			expertCount++
		case TierRestricted:
			restrictedCount++
		}
	}

	adjustedRoles := make([]string, len(roles))
	copy(adjustedRoles, roles)

	// Strategy 1: If we have restricted-tier roles on a large+ task,
	// add a second senior_dev for redundancy.
	if restrictedCount > 0 && (label == formationLabelLarge || label == FormationComplexityCritical) {
		seniorCount := 0
		for _, r := range adjustedRoles {
			if r == "senior_dev" {
				seniorCount++
			}
		}
		if seniorCount < 2 {
			adjustedRoles = append(adjustedRoles, "senior_dev")
			reasonParts = append(reasonParts,
				fmt.Sprintf("Added extra senior_dev: %d restricted-tier roles need supervision", restrictedCount))
		}
	}

	// Strategy 2: If all roles are expert-tier on a medium task,
	// we can trim the team (remove junior_dev if present).
	if expertCount == len(roleELOs) && expertCount > 0 && label == formationLabelMedium {
		trimmed := make([]string, 0, len(adjustedRoles))
		removedJunior := false
		for _, r := range adjustedRoles {
			if r == "junior_dev" && !removedJunior {
				removedJunior = true
				continue
			}
			trimmed = append(trimmed, r)
		}
		if removedJunior {
			adjustedRoles = trimmed
			reasonParts = append(reasonParts,
				"Trimmed junior_dev: all roles are expert-tier")
		}
	}

	// Strategy 3: If security role is restricted but task has security impact,
	// add architect as backup security reviewer.
	secInfo, hasSec := roleELOs["security"]
	if hasSec && secInfo.Tier == TierRestricted && signals.HasSecurityImpact {
		hasArchitect := false
		for _, r := range adjustedRoles {
			if r == "architect" {
				hasArchitect = true
				break
			}
		}
		if !hasArchitect {
			adjustedRoles = append(adjustedRoles, "architect")
			reasonParts = append(reasonParts,
				"Added architect: security role is restricted-tier but task has security impact")
		}
	}

	// Build ELO summary in reasoning.
	if len(roleELOs) > 0 {
		eloParts := make([]string, 0, len(roleELOs))
		for role, info := range roleELOs {
			eloParts = append(eloParts, fmt.Sprintf("%s=%.0f(%s)", role, info.ELO, info.Tier))
		}
		sort.Strings(eloParts)
		reasonParts = append(reasonParts, "ELO: "+strings.Join(eloParts, ", "))
	}

	return adjustedRoles, strings.Join(reasonParts, ". ")
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// formationSigmoid returns a smooth 0→1 curve: value / (value + midpoint).
func formationSigmoid(value, midpoint float64) float64 {
	if value <= 0 {
		return 0
	}
	return value / (value + midpoint)
}

// formationTitleCase capitalises the first letter of a string.
func formationTitleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// formationFileExtension extracts the extension from a file path.
func formationFileExtension(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' {
			return ""
		}
	}
	return ""
}

// formationExtensionToLanguage maps file extensions to language names.
func formationExtensionToLanguage(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".sql":
		return "sql"
	case ".proto":
		return "protobuf"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".md", ".mdx":
		return "markdown"
	case ".css", ".scss":
		return "css"
	case ".html":
		return "html"
	case ".sh", ".bash":
		return "shell"
	default:
		return ""
	}
}

// formationIsTestFile returns true if the file path looks like a test file.
func formationIsTestFile(path string) bool {
	return strings.Contains(path, "_test.") ||
		strings.Contains(path, ".test.") ||
		strings.Contains(path, ".spec.") ||
		strings.Contains(path, "/test/") ||
		strings.Contains(path, "/tests/") ||
		strings.Contains(path, "__tests__/")
}
