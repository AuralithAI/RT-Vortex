package swarm

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ELO constants.
const (
	DefaultELO = 1200.0
	KFactor    = 32.0
)

// ELOService manages agent performance scoring.
type ELOService struct {
	db *pgxpool.Pool
}

// NewELOService creates a new ELO scoring service.
func NewELOService(db *pgxpool.Pool) *ELOService {
	return &ELOService{db: db}
}

// RecordFeedback updates an agent's ELO score based on human rating.
// Rating 1-5 is mapped to a 0.0-1.0 score for ELO calculation.
func (s *ELOService) RecordFeedback(ctx context.Context, agentID uuid.UUID, rating int) error {
	// Fetch current agent stats.
	var currentELO float64
	var tasksDone, tasksRated int
	var avgRating float64
	err := s.db.QueryRow(ctx, `
		SELECT elo_score, tasks_done, tasks_rated, avg_rating
		FROM swarm_agents WHERE id = $1`, agentID).Scan(&currentELO, &tasksDone, &tasksRated, &avgRating)
	if err != nil {
		return fmt.Errorf("fetching agent %s for ELO: %w", agentID, err)
	}

	// Map rating 1-5 → actual score 0.0-1.0.
	actual := float64(rating-1) / 4.0

	// Expected score (against the baseline of 1200).
	expected := 1.0 / (1.0 + math.Pow(10, (DefaultELO-currentELO)/400.0))

	// New ELO.
	newELO := currentELO + KFactor*(actual-expected)

	// Update running average rating.
	newAvg := (avgRating*float64(tasksRated) + float64(rating)) / float64(tasksRated+1)

	_, err = s.db.Exec(ctx, `
		UPDATE swarm_agents
		SET elo_score = $1,
		    tasks_done = tasks_done + 1,
		    tasks_rated = tasks_rated + 1,
		    avg_rating = $2
		WHERE id = $3`,
		newELO, newAvg, agentID,
	)
	if err != nil {
		return fmt.Errorf("updating ELO for agent %s: %w", agentID, err)
	}

	return nil
}

// IncrementTasksDone bumps the tasks_done counter (without a rating).
func (s *ELOService) IncrementTasksDone(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE swarm_agents SET tasks_done = tasks_done + 1 WHERE id = $1`, agentID)
	return err
}
