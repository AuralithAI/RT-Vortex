package swarm

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── ELO Auto-Promotion / Demotion ───────────────────────────────────────────
//
// Agents that consistently perform well are promoted; underperformers are demoted.
// Thresholds: ≥1400 → expert, ≤1000 → restricted, between → standard.

const (
	PromotionThreshold = 1400.0
	DemotionThreshold  = 1000.0
	MinRatedTasks      = 5

	TierStandard   = "standard"
	TierExpert     = "expert"
	TierRestricted = "restricted"
)

// ELOAutoTierService periodically reviews agent ELO scores and adjusts
// their tier. This runs as a goroutine alongside the existing task manager loops.
type ELOAutoTierService struct {
	db       *pgxpool.Pool
	interval time.Duration
}

// NewELOAutoTierService creates a new auto-tier service.
func NewELOAutoTierService(db *pgxpool.Pool) *ELOAutoTierService {
	return &ELOAutoTierService{
		db:       db,
		interval: 5 * time.Minute,
	}
}

// Start begins the periodic tier review loop.
func (s *ELOAutoTierService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run once immediately.
	s.reviewTiers(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reviewTiers(ctx)
		}
	}
}

// reviewTiers checks all agents with enough rated tasks and adjusts tiers.
func (s *ELOAutoTierService) reviewTiers(ctx context.Context) {
	rows, err := s.db.Query(ctx, `
		SELECT id, role, elo_score, tasks_rated, COALESCE(tier, 'standard')
		FROM swarm_agents
		WHERE tasks_rated >= $1 AND status != 'offline'`,
		MinRatedTasks,
	)
	if err != nil {
		slog.Error("elo auto-tier: query failed", "error", err)
		return
	}
	defer rows.Close()

	var promoted, demoted int
	for rows.Next() {
		var (
			agentID     uuid.UUID
			role        string
			eloScore    float64
			tasksRated  int
			currentTier string
		)
		if err := rows.Scan(&agentID, &role, &eloScore, &tasksRated, &currentTier); err != nil {
			continue
		}

		newTier := currentTier
		if eloScore >= PromotionThreshold && currentTier != TierExpert {
			newTier = TierExpert
			promoted++
		} else if eloScore <= DemotionThreshold && currentTier != TierRestricted {
			newTier = TierRestricted
			demoted++
		} else if eloScore > DemotionThreshold && eloScore < PromotionThreshold && currentTier != TierStandard {
			newTier = TierStandard
		}

		if newTier != currentTier {
			_, err := s.db.Exec(ctx,
				`UPDATE swarm_agents SET tier = $1 WHERE id = $2`,
				newTier, agentID)
			if err != nil {
				slog.Error("elo auto-tier: update failed",
					"agent_id", agentID, "error", err)
				continue
			}
			slog.Info("elo auto-tier: agent tier changed",
				"agent_id", agentID,
				"role", role,
				"elo", eloScore,
				"old_tier", currentTier,
				"new_tier", newTier,
			)
		}
	}

	if promoted > 0 || demoted > 0 {
		slog.Info("elo auto-tier: review complete",
			"promoted", promoted,
			"demoted", demoted,
		)
	}
}

// GetAgentTier returns the current tier for an agent.
func (s *ELOAutoTierService) GetAgentTier(ctx context.Context, agentID uuid.UUID) string {
	var tier string
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(tier, 'standard') FROM swarm_agents WHERE id = $1`,
		agentID).Scan(&tier)
	if err != nil {
		return TierStandard
	}
	return tier
}
