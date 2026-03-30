// ─── Swarm Types ─────────────────────────────────────────────────────────────
// TypeScript types for the Agent Swarm system.
// ─────────────────────────────────────────────────────────────────────────────

export type TaskStatus =
  | "submitted"
  | "planning"
  | "plan_review"
  | "implementing"
  | "self_review"
  | "diff_review"
  | "pr_creating"
  | "completed"
  | "cancelled"
  | "failed"
  | "timed_out";

export type AgentRole =
  | "orchestrator"
  | "architect"
  | "senior_dev"
  | "junior_dev"
  | "qa"
  | "security"
  | "ops"
  | "docs"
  | "ui_ux";

export type AgentStatus = "offline" | "idle" | "busy" | "errored";
export type TeamStatus = "idle" | "busy" | "offline";
export type DiffStatus = "pending" | "approved" | "rejected";
export type ChangeType = "modified" | "added" | "deleted" | "renamed";

export interface SwarmTask {
  id: string;
  repo_id: string;
  title: string;
  description: string;
  status: TaskStatus;
  plan_document?: PlanDocument;
  assigned_team_id?: string;
  assigned_agents: string[];
  pr_url?: string;
  pr_number?: number;
  human_rating?: number;
  human_comment?: string;
  submitted_by?: string;
  retry_count: number;
  failure_reason?: string;
  created_at: string;
  completed_at?: string;
  timeout_at?: string;
}

export interface PlanDocument {
  summary: string;
  steps: PlanStep[];
  affected_files: string[];
  estimated_complexity: "small" | "medium" | "large";
  agents_needed: number;
}

export interface PlanStep {
  description: string;
  files?: string[];
}

export interface SwarmAgent {
  id: string;
  role: AgentRole;
  team_id?: string;
  status: AgentStatus;
  elo_score: number;
  tasks_done: number;
  tasks_rated: number;
  avg_rating: number;
  hostname: string;
  version: string;
  registered_at: string;
}

export interface SwarmTeam {
  id: string;
  name: string;
  lead_agent_id?: string;
  status: TeamStatus;
  agent_ids: string[];
  formed_at: string;
}

export interface SwarmDiff {
  id: string;
  task_id: string;
  file_path: string;
  change_type: ChangeType;
  original: string;
  proposed: string;
  unified_diff: string;
  agent_id?: string;
  status: DiffStatus;
  created_at: string;
}

export interface SwarmDiffMeta {
  id: string;
  task_id: string;
  file_path: string;
  change_type: ChangeType;
  agent_id?: string;
  status: DiffStatus;
  created_at: string;
}

export interface DiffComment {
  id: string;
  diff_id: string;
  author_type: "agent" | "user";
  author_id: string;
  line_number: number;
  content: string;
  created_at: string;
}

export interface AgentFeedback {
  task_id: string;
  agent_id: string;
  rating: number;
  comment?: string;
}

export interface SwarmOverview {
  active_tasks: number;
  pending_tasks: number;
  completed_all_time: number;
  failed_all_time: number;
  active_teams: number;
  busy_teams: number;
  online_agents: number;
  busy_agents: number;
  avg_duration_seconds: number;
  total_retries: number;
  llm_percentage: number;
  agents?: AgentSnapshot[];
}

export interface AgentSnapshot {
  id: string;
  role: AgentRole;
  status: AgentStatus;
  team_id?: string;
}

export interface TaskSummary {
  id: string;
  repo_id: string;
  title: string;
  description: string;
  status: TaskStatus;
  retry_count: number;
  pr_url?: string;
  pr_number?: number;
  human_rating?: number;
  created_at: string;
  completed_at?: string;
  diff_count: number;
  agent_count: number;
  duration_sec?: number;
}

export interface TaskHistoryResponse {
  tasks: TaskSummary[];
  total: number;
  limit: number;
  offset: number;
}

export interface TaskSubmission {
  repo_id: string;
  title: string;
  description: string;
}
