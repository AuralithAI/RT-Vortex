package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxTeams is the maximum number of concurrent teams.
const MaxTeams = 5

// Team represents a swarm team from the database.
type Team struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	LeadAgentID *uuid.UUID  `json:"lead_agent_id,omitempty"`
	Status      string      `json:"status"` // idle, busy, offline
	AgentIDs    []uuid.UUID `json:"agent_ids"`
}

// ── TeamManager ─────────────────────────────────────────────────────────────

// TeamManager handles on-demand team formation, scaling, and lifecycle.
type TeamManager struct {
	db *pgxpool.Pool
	mu sync.RWMutex
}

// NewTeamManager creates a new TeamManager.
func NewTeamManager(db *pgxpool.Pool) *TeamManager {
	return &TeamManager{db: db}
}

// CreateTeam inserts a new team and returns it.
func (m *TeamManager) CreateTeam(ctx context.Context, name string) (*Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Enforce max teams.
	count, err := m.activeTeamCount(ctx)
	if err != nil {
		return nil, err
	}
	if count >= MaxTeams {
		return nil, fmt.Errorf("max teams reached (%d)", MaxTeams)
	}

	team := &Team{
		ID:     uuid.New(),
		Name:   name,
		Status: "idle",
	}

	_, err = m.db.Exec(ctx, `
		INSERT INTO swarm_teams (id, name, status) VALUES ($1, $2, $3)`,
		team.ID, team.Name, team.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting team: %w", err)
	}

	slog.Info("swarm team created", "team_id", team.ID, "name", name)
	return team, nil
}

// GetIdleTeam returns the first idle team, or nil if none is idle.
func (m *TeamManager) GetIdleTeam(ctx context.Context) *Team {
	m.mu.RLock()
	defer m.mu.RUnlock()

	row := m.db.QueryRow(ctx, `
		SELECT id, name, lead_agent_id, status, agent_ids
		FROM swarm_teams WHERE status = 'idle' LIMIT 1`)

	var t Team
	err := row.Scan(&t.ID, &t.Name, &t.LeadAgentID, &t.Status, &t.AgentIDs)
	if err != nil {
		return nil
	}
	return &t
}

// MarkTeamBusy sets a team's status to busy.
func (m *TeamManager) MarkTeamBusy(ctx context.Context, teamID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.db.Exec(ctx, `UPDATE swarm_teams SET status = 'busy' WHERE id = $1`, teamID)
}

// ReleaseTeam sets a team's status back to idle.
func (m *TeamManager) ReleaseTeam(ctx context.Context, teamID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.db.Exec(ctx, `UPDATE swarm_teams SET status = 'idle' WHERE id = $1`, teamID)
	slog.Info("swarm team released to idle", "team_id", teamID)
}

// CanCreateTeam checks whether we're below the max team count.
func (m *TeamManager) CanCreateTeam() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count, err := m.activeTeamCount(context.Background())
	if err != nil {
		return false
	}
	return count < MaxTeams
}

// ListTeams returns all teams.
func (m *TeamManager) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, name, lead_agent_id, status, agent_ids
		FROM swarm_teams ORDER BY formed_at`)
	if err != nil {
		return nil, fmt.Errorf("listing teams: %w", err)
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.LeadAgentID, &t.Status, &t.AgentIDs); err != nil {
			return nil, fmt.Errorf("scanning team: %w", err)
		}
		teams = append(teams, t)
	}
	return teams, nil
}

// RegisterAgent inserts or updates an agent in the DB, linked to a team.
func (m *TeamManager) RegisterAgent(ctx context.Context, agentID uuid.UUID, role string, teamID uuid.UUID, hostname, version string) error {
	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_agents (id, role, team_id, status, hostname, version)
		VALUES ($1, $2, $3, 'idle', $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			role = EXCLUDED.role,
			team_id = EXCLUDED.team_id,
			status = 'idle',
			hostname = EXCLUDED.hostname,
			version = EXCLUDED.version,
			registered_at = NOW()`,
		agentID, role, teamID, hostname, version,
	)
	if err != nil {
		return fmt.Errorf("registering agent: %w", err)
	}

	// Add agent to team's agent_ids array.
	_, _ = m.db.Exec(ctx, `
		UPDATE swarm_teams
		SET agent_ids = array_append(
			COALESCE(agent_ids, ARRAY[]::UUID[]),
			$1
		)
		WHERE id = $2 AND NOT ($1 = ANY(COALESCE(agent_ids, ARRAY[]::UUID[])))`,
		agentID, teamID,
	)

	return nil
}

// ListAgents returns all agents with optional status filter.
func (m *TeamManager) ListAgents(ctx context.Context, statusFilter string) ([]AgentInfo, error) {
	query := `SELECT id, role, team_id, status, elo_score, tasks_done, tasks_rated, avg_rating, hostname, version
	          FROM swarm_agents`
	args := []interface{}{}
	if statusFilter != "" {
		query += ` WHERE status = $1`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY registered_at`

	rows, err := m.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	defer rows.Close()

	var agents []AgentInfo
	for rows.Next() {
		var a AgentInfo
		if err := rows.Scan(&a.ID, &a.Role, &a.TeamID, &a.Status, &a.EloScore,
			&a.TasksDone, &a.TasksRated, &a.AvgRating, &a.Hostname, &a.Version); err != nil {
			return nil, fmt.Errorf("scanning agent: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// AgentInfo holds agent details for the REST API.
type AgentInfo struct {
	ID         uuid.UUID  `json:"id"`
	Role       string     `json:"role"`
	TeamID     *uuid.UUID `json:"team_id,omitempty"`
	Status     string     `json:"status"`
	EloScore   float64    `json:"elo_score"`
	TasksDone  int        `json:"tasks_done"`
	TasksRated int        `json:"tasks_rated"`
	AvgRating  float64    `json:"avg_rating"`
	Hostname   string     `json:"hostname"`
	Version    string     `json:"version"`
}

// activeTeamCount returns the number of non-offline teams. Must be called under lock.
func (m *TeamManager) activeTeamCount(ctx context.Context) (int, error) {
	var count int
	err := m.db.QueryRow(ctx, `SELECT COUNT(*) FROM swarm_teams WHERE status != 'offline'`).Scan(&count)
	return count, err
}
