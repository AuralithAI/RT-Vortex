package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Role-Based ELO ───────────────────────────────────────────────
//
// ELO tracks (role, repo_id) pairs — NOT ephemeral agent UUIDs.  Every time
// a new agent registers for a given role + repo it inherits the role's
// accumulated score and tier.  Performance data survives agent lifecycles.
//
// The composite reward formula combines:
//   - Human rating (1-5 mapped to 0.0-1.0)
//   - Consensus quality (confidence score from consensus engine)
//   - Automatic signals (PR accepted, tests pass, build success)
//
// Reinforcement for poor performers:
//   - "restricted" tier agents get extra training probes (5 models instead of 3)
//   - "expert" agents get leaner probes (2 models — trusted to be fast)
//
// Decay: -2 ELO / day inactive, floored at DefaultELO - 100.

// ── Constants ───────────────────────────────────────────────────────────────

const (
	RoleELODefault     = 1200.0
	RoleELOKFactor     = 32.0
	RoleELODecayPerDay = 2.0
	RoleELOFloor       = RoleELODefault - 100.0 // 1100
	RoleELOExpertMin   = 1400.0
	RoleELORestrictMin = 1000.0

	// Composite reward weights (must sum to 1.0).
	WeightHumanRating      = 0.40
	WeightConsensusQuality = 0.35
	WeightAutoMetrics      = 0.25
)

// TaskOutcome captures all signals for a single task completion.
type TaskOutcome struct {
	TaskID            string  `json:"task_id"`
	HumanRating       int     `json:"human_rating,omitempty"`         // 1-5, 0 = not rated
	ConsensusConf     float64 `json:"consensus_confidence,omitempty"` // 0.0-1.0
	ConsensusStrategy string  `json:"consensus_strategy,omitempty"`
	TestsPassed       bool    `json:"tests_passed,omitempty"`
	PRAccepted        bool    `json:"pr_accepted,omitempty"`
	BuildSuccess      bool    `json:"build_success,omitempty"`
	ConsensusWin      bool    `json:"consensus_win,omitempty"` // this role's response won consensus
}

// RoleELO represents a persistent (role, repo_id) ELO record.
type RoleELO struct {
	ID             uuid.UUID `json:"id"`
	Role           string    `json:"role"`
	RepoID         string    `json:"repo_id"`
	ELOScore       float64   `json:"elo_score"`
	Tier           string    `json:"tier"`
	TasksDone      int       `json:"tasks_done"`
	TasksRated     int       `json:"tasks_rated"`
	AvgRating      float64   `json:"avg_rating"`
	Wins           int       `json:"wins"`
	Losses         int       `json:"losses"`
	ConsensusAvg   float64   `json:"consensus_avg"`
	BestStrategy   string    `json:"best_strategy"`
	TrainingProbes int       `json:"training_probes"`
	LastActive     time.Time `json:"last_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// RoleELOHistory is a single audit log entry for an ELO change.
type RoleELOHistory struct {
	ID        uuid.UUID       `json:"id"`
	Role      string          `json:"role"`
	RepoID    string          `json:"repo_id"`
	TaskID    string          `json:"task_id"`
	EventType string          `json:"event_type"`
	OldELO    float64         `json:"old_elo"`
	NewELO    float64         `json:"new_elo"`
	Delta     float64         `json:"delta"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ── Service ─────────────────────────────────────────────────────────────────

// RoleELOService manages persistent role-based ELO scoring.
type RoleELOService struct {
	db *pgxpool.Pool
}

// NewRoleELOService creates a new role-based ELO service.
func NewRoleELOService(db *pgxpool.Pool) *RoleELOService {
	return &RoleELOService{db: db}
}

// ── Get / Ensure ────────────────────────────────────────────────────────────

// GetRoleELO returns the ELO record for a (role, repoID) pair.
// If no record exists, a default one is created and returned.
func (s *RoleELOService) GetRoleELO(ctx context.Context, role, repoID string) (*RoleELO, error) {
	r, err := s.fetchRoleELO(ctx, role, repoID)
	if err != nil {
		return nil, err
	}
	if r != nil {
		return r, nil
	}

	// First time — create default record.
	id := uuid.New()
	now := time.Now().UTC()
	_, err = s.db.Exec(ctx, `
		INSERT INTO swarm_role_elo (id, role, repo_id, elo_score, tier, last_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $6)
		ON CONFLICT (role, repo_id) DO NOTHING`,
		id, role, repoID, RoleELODefault, TierStandard, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating role ELO for %s/%s: %w", role, repoID, err)
	}

	return s.fetchRoleELO(ctx, role, repoID)
}

func (s *RoleELOService) fetchRoleELO(ctx context.Context, role, repoID string) (*RoleELO, error) {
	var r RoleELO
	err := s.db.QueryRow(ctx, `
		SELECT id, role, repo_id, elo_score, tier, tasks_done, tasks_rated,
		       avg_rating, wins, losses, consensus_avg, best_strategy,
		       training_probes, last_active, created_at, updated_at
		FROM swarm_role_elo
		WHERE role = $1 AND repo_id = $2`,
		role, repoID,
	).Scan(
		&r.ID, &r.Role, &r.RepoID, &r.ELOScore, &r.Tier,
		&r.TasksDone, &r.TasksRated, &r.AvgRating,
		&r.Wins, &r.Losses, &r.ConsensusAvg, &r.BestStrategy,
		&r.TrainingProbes, &r.LastActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("fetching role ELO for %s/%s: %w", role, repoID, err)
	}
	return &r, nil
}

// ── Record Outcome ──────────────────────────────────────────────────────────

// RecordRoleOutcome applies a composite reward to the role ELO.
// It combines human rating, consensus quality, and automatic metrics.
func (s *RoleELOService) RecordRoleOutcome(ctx context.Context, role, repoID string, outcome TaskOutcome) error {
	re, err := s.GetRoleELO(ctx, role, repoID)
	if err != nil {
		return err
	}

	oldELO := re.ELOScore

	// Compute composite actual score (0.0 - 1.0).
	actual := s.computeCompositeScore(outcome)

	// Expected score (standard ELO formula against baseline).
	expected := 1.0 / (1.0 + math.Pow(10, (RoleELODefault-re.ELOScore)/400.0))

	// New ELO.
	newELO := re.ELOScore + RoleELOKFactor*(actual-expected)

	// Floor.
	if newELO < RoleELOFloor {
		newELO = RoleELOFloor
	}

	// Determine new tier.
	newTier := TierStandard
	if newELO >= RoleELOExpertMin {
		newTier = TierExpert
	} else if newELO <= RoleELORestrictMin {
		newTier = TierRestricted
	}

	// Update running averages.
	newTasksDone := re.TasksDone + 1
	newTasksRated := re.TasksRated
	newAvgRating := re.AvgRating
	if outcome.HumanRating > 0 {
		newTasksRated++
		newAvgRating = (re.AvgRating*float64(re.TasksRated) + float64(outcome.HumanRating)) / float64(newTasksRated)
	}

	// Update win/loss counts.
	newWins := re.Wins
	newLosses := re.Losses
	if outcome.ConsensusWin {
		newWins++
	} else if outcome.ConsensusConf > 0 {
		newLosses++
	}

	// Update consensus average.
	newConsensusAvg := re.ConsensusAvg
	if outcome.ConsensusConf > 0 {
		total := re.Wins + re.Losses
		if total > 0 {
			newConsensusAvg = (re.ConsensusAvg*float64(total) + outcome.ConsensusConf) / float64(total+1)
		} else {
			newConsensusAvg = outcome.ConsensusConf
		}
	}

	// Update best strategy.
	bestStrategy := re.BestStrategy
	if outcome.ConsensusStrategy != "" && outcome.ConsensusConf > 0.7 {
		bestStrategy = outcome.ConsensusStrategy
	}

	// Training probes: increment for restricted tier (reinforcement learning).
	trainingProbes := re.TrainingProbes
	if newTier == TierRestricted {
		trainingProbes++
	}

	now := time.Now().UTC()

	_, err = s.db.Exec(ctx, `
		UPDATE swarm_role_elo SET
			elo_score       = $1,
			tier            = $2,
			tasks_done      = $3,
			tasks_rated     = $4,
			avg_rating      = $5,
			wins            = $6,
			losses          = $7,
			consensus_avg   = $8,
			best_strategy   = $9,
			training_probes = $10,
			last_active     = $11,
			updated_at      = $11
		WHERE role = $12 AND repo_id = $13`,
		newELO, newTier, newTasksDone, newTasksRated, newAvgRating,
		newWins, newLosses, newConsensusAvg, bestStrategy, trainingProbes,
		now, role, repoID,
	)
	if err != nil {
		return fmt.Errorf("updating role ELO for %s/%s: %w", role, repoID, err)
	}

	// Record history.
	eventType := "rating"
	if outcome.HumanRating == 0 {
		eventType = "auto_metric"
	}
	if outcome.ConsensusConf > 0 {
		eventType = "consensus"
	}

	delta := newELO - oldELO
	detail, _ := json.Marshal(map[string]interface{}{
		"outcome":  outcome,
		"actual":   actual,
		"expected": expected,
		"old_tier": re.Tier,
		"new_tier": newTier,
	})

	_ = s.appendHistory(ctx, role, repoID, outcome.TaskID, eventType, oldELO, newELO, delta, detail)

	// Update Prometheus metrics.
	SwarmRoleELOGauge.WithLabelValues(role, repoID).Set(newELO)
	if re.Tier != newTier {
		SwarmRoleTierChanges.WithLabelValues(role, re.Tier, newTier).Inc()
		slog.Info("role ELO tier changed",
			"role", role, "repo_id", repoID,
			"old_tier", re.Tier, "new_tier", newTier,
			"elo", newELO,
		)
	}

	slog.Debug("role ELO updated",
		"role", role, "repo_id", repoID,
		"old_elo", oldELO, "new_elo", newELO,
		"delta", delta, "tier", newTier,
	)

	return nil
}

// computeCompositeScore produces a 0.0-1.0 score from multiple signals.
func (s *RoleELOService) computeCompositeScore(o TaskOutcome) float64 {
	var score float64
	var totalWeight float64

	// Human rating (1-5 → 0.0-1.0).
	if o.HumanRating > 0 {
		humanScore := float64(o.HumanRating-1) / 4.0
		score += WeightHumanRating * humanScore
		totalWeight += WeightHumanRating
	}

	// Consensus quality (direct 0.0-1.0).
	if o.ConsensusConf > 0 {
		consScore := o.ConsensusConf
		if o.ConsensusWin {
			consScore = math.Min(1.0, consScore*1.2) // 20% bonus for winning
		}
		score += WeightConsensusQuality * consScore
		totalWeight += WeightConsensusQuality
	}

	// Automatic metrics (binary signals → 0.0-1.0).
	autoCount := 0
	autoScore := 0.0
	if o.TestsPassed {
		autoScore += 1.0
		autoCount++
	}
	if o.PRAccepted {
		autoScore += 1.0
		autoCount++
	}
	if o.BuildSuccess {
		autoScore += 1.0
		autoCount++
	}
	if autoCount > 0 {
		autoNorm := autoScore / float64(autoCount)
		score += WeightAutoMetrics * autoNorm
		totalWeight += WeightAutoMetrics
	}

	// If we have no signals at all, give a neutral 0.5.
	if totalWeight == 0 {
		return 0.5
	}

	// Normalise by actual weight used (some signals may be missing).
	return score / totalWeight
}

// ── Decay ───────────────────────────────────────────────────────────────────

// ApplyDecay reduces ELO for roles that haven't been active recently.
// Called periodically by the background decay loop.
func (s *RoleELOService) ApplyDecay(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour)

	rows, err := s.db.Query(ctx, `
		SELECT id, role, repo_id, elo_score, last_active
		FROM swarm_role_elo
		WHERE last_active < $1 AND elo_score > $2`,
		cutoff, RoleELOFloor,
	)
	if err != nil {
		return 0, fmt.Errorf("querying decay candidates: %w", err)
	}
	defer rows.Close()

	var decayed int
	for rows.Next() {
		var (
			id         uuid.UUID
			role       string
			repoID     string
			eloScore   float64
			lastActive time.Time
		)
		if err := rows.Scan(&id, &role, &repoID, &eloScore, &lastActive); err != nil {
			continue
		}

		// Days since last active (minimum 1).
		daysSince := time.Since(lastActive).Hours() / 24.0
		if daysSince < 1 {
			continue
		}

		decayAmount := RoleELODecayPerDay * daysSince
		newELO := eloScore - decayAmount
		if newELO < RoleELOFloor {
			newELO = RoleELOFloor
		}

		// Determine new tier.
		newTier := TierStandard
		if newELO >= RoleELOExpertMin {
			newTier = TierExpert
		} else if newELO <= RoleELORestrictMin {
			newTier = TierRestricted
		}

		_, err := s.db.Exec(ctx, `
			UPDATE swarm_role_elo SET elo_score = $1, tier = $2, updated_at = NOW()
			WHERE id = $3`,
			newELO, newTier, id,
		)
		if err != nil {
			slog.Error("role ELO decay update failed", "role", role, "repo_id", repoID, "error", err)
			continue
		}

		delta := newELO - eloScore
		detail, _ := json.Marshal(map[string]interface{}{
			"days_inactive": daysSince,
			"decay_amount":  decayAmount,
		})
		_ = s.appendHistory(ctx, role, repoID, "", "decay", eloScore, newELO, delta, detail)

		SwarmRoleELODecayTotal.WithLabelValues(role).Inc()
		decayed++
	}

	return decayed, nil
}

// ── Leaderboard / Query ─────────────────────────────────────────────────────

// ListRoleELOs returns all role ELO records, optionally filtered by repo.
func (s *RoleELOService) ListRoleELOs(ctx context.Context, repoID string, limit int) ([]RoleELO, error) {
	if limit <= 0 {
		limit = 50
	}

	var query string
	var args []interface{}
	if repoID != "" {
		query = `
			SELECT id, role, repo_id, elo_score, tier, tasks_done, tasks_rated,
			       avg_rating, wins, losses, consensus_avg, best_strategy,
			       training_probes, last_active, created_at, updated_at
			FROM swarm_role_elo
			WHERE repo_id = $1
			ORDER BY elo_score DESC
			LIMIT $2`
		args = []interface{}{repoID, limit}
	} else {
		query = `
			SELECT id, role, repo_id, elo_score, tier, tasks_done, tasks_rated,
			       avg_rating, wins, losses, consensus_avg, best_strategy,
			       training_probes, last_active, created_at, updated_at
			FROM swarm_role_elo
			ORDER BY elo_score DESC
			LIMIT $1`
		args = []interface{}{limit}
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing role ELOs: %w", err)
	}
	defer rows.Close()

	var results []RoleELO
	for rows.Next() {
		var r RoleELO
		if err := rows.Scan(
			&r.ID, &r.Role, &r.RepoID, &r.ELOScore, &r.Tier,
			&r.TasksDone, &r.TasksRated, &r.AvgRating,
			&r.Wins, &r.Losses, &r.ConsensusAvg, &r.BestStrategy,
			&r.TrainingProbes, &r.LastActive, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning role ELO: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

// GetRoleELOHistory returns the ELO change history for a (role, repoID).
func (s *RoleELOService) GetRoleELOHistory(ctx context.Context, role, repoID string, limit int) ([]RoleELOHistory, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, role, repo_id, task_id, event_type,
		       old_elo, new_elo, delta, detail, created_at
		FROM swarm_role_elo_history
		WHERE role = $1 AND repo_id = $2
		ORDER BY created_at DESC
		LIMIT $3`,
		role, repoID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing role ELO history: %w", err)
	}
	defer rows.Close()

	var results []RoleELOHistory
	for rows.Next() {
		var h RoleELOHistory
		if err := rows.Scan(
			&h.ID, &h.Role, &h.RepoID, &h.TaskID, &h.EventType,
			&h.OldELO, &h.NewELO, &h.Delta, &h.Detail, &h.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning role ELO history: %w", err)
		}
		results = append(results, h)
	}
	return results, nil
}

// ── Tier Lookup (for registration injection) ────────────────────────────────

// TierForRole returns the current tier and ELO for a role in a repo.
// If no record exists, returns ("standard", DefaultELO).
func (s *RoleELOService) TierForRole(ctx context.Context, role, repoID string) (tier string, elo float64) {
	err := s.db.QueryRow(ctx, `
		SELECT tier, elo_score FROM swarm_role_elo
		WHERE role = $1 AND repo_id = $2`,
		role, repoID,
	).Scan(&tier, &elo)
	if err != nil {
		return TierStandard, RoleELODefault
	}
	return tier, elo
}

// ProbeCountForTier returns the recommended number of LLM probes based on tier.
//
//	restricted → 5 (more perspectives to compensate for lower quality)
//	standard   → 3 (balanced)
//	expert     → 2 (trusted, lean probing)
func ProbeCountForTier(tier string) int {
	switch tier {
	case TierRestricted:
		return 5
	case TierExpert:
		return 2
	default:
		return 3
	}
}

// ── History Append ──────────────────────────────────────────────────────────

func (s *RoleELOService) appendHistory(
	ctx context.Context,
	role, repoID, taskID, eventType string,
	oldELO, newELO, delta float64,
	detail json.RawMessage,
) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO swarm_role_elo_history
			(id, role, repo_id, task_id, event_type, old_elo, new_elo, delta, detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.New(), role, repoID, taskID, eventType, oldELO, newELO, delta, detail,
	)
	return err
}

// ── Background Decay Loop ───────────────────────────────────────────────────

// RoleELODecayService runs periodic ELO decay for inactive roles.
type RoleELODecayService struct {
	svc      *RoleELOService
	interval time.Duration
}

// NewRoleELODecayService creates a new decay service.
func NewRoleELODecayService(svc *RoleELOService) *RoleELODecayService {
	return &RoleELODecayService{
		svc:      svc,
		interval: 6 * time.Hour, // decay check every 6 hours
	}
}

// Start begins the periodic decay loop.
func (d *RoleELODecayService) Start(ctx context.Context) {
	slog.Info("role ELO decay service started", "interval", d.interval)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("role ELO decay service stopped")
			return
		case <-ticker.C:
			decayed, err := d.svc.ApplyDecay(ctx)
			if err != nil {
				slog.Error("role ELO decay failed", "error", err)
			} else if decayed > 0 {
				slog.Info("role ELO decay applied", "decayed", decayed)
			}
		}
	}
}
