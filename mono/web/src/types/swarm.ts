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
  team_formation?: TeamFormationData;
  created_at: string;
  completed_at?: string;
  timeout_at?: string;
}

export interface PlanDocument {
  summary: string;
  steps: PlanStep[];
  affected_files: string[];
  estimated_complexity: "small" | "medium" | "large";
  agents_needed: string[] | number;
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

// ─── Multi-LLM Discussion Types ─────────────────────────────────────────────

export type ConsensusStrategy =
  | "pick_best"
  | "majority_vote"
  | "gpt_as_judge"
  | "multi_judge_panel"
  | "auto";

export type DiscussionStatus = "open" | "complete" | "synthesised";

/** A single LLM provider's response within a discussion thread. */
export interface ProviderResponseData {
  provider: string;
  model: string;
  content: string;
  latency_ms: number;
  finish_reason?: string;
  token_usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
  error?: string;
  timestamp?: number;
  /** Internal flag: true while content is being built from streaming chunks. */
  _streaming?: boolean;
}

/** A multi-LLM discussion thread — multiple providers answered the same question. */
export interface DiscussionThreadData {
  thread_id: string;
  agent_id: string;
  agent_role: string;
  topic: string;
  action_type?: string;
  responses: ProviderResponseData[];
  status: DiscussionStatus;
  synthesis?: string;
  synthesis_provider?: string;
  provider_count: number;
  success_count: number;
  created_at: number;
  completed_at?: number;
}

/** A single judge's verdict from the multi-judge panel. */
export interface JudgeVerdictData {
  judge_provider: string;
  judge_model: string;
  winner: string;
  confidence: number;
  scores: Record<string, number>;
  reasoning: string;
  error?: string;
}

/** The consensus engine's final result. */
export interface ConsensusResultData {
  thread_id?: string;
  strategy: ConsensusStrategy;
  provider: string;
  model?: string;
  confidence: number;
  reasoning: string;
  scores?: Record<string, number>;
  judge_count?: number;
  judge_agreement?: number;
  judge_verdicts?: JudgeVerdictData[];
}

/** LLM provider metadata for display (name, color, icon key). */
export interface LLMProviderMeta {
  name: string;
  displayName: string;
  color: string;
  bgColor: string;
  borderColor: string;
  /** Hex colour for SVG / canvas use. */
  accentHex?: string;
  /** Tailwind dot-colour class (e.g. "bg-orange-500"). */
  dotColor?: string;
}

/** Insight category for cross-task learning. */
export type InsightCategory =
  | "provider_reliability"
  | "strategy_effectiveness"
  | "code_pattern"
  | "provider_agreement"
  | "quality_signal";

/** A durable cross-task consensus insight. */
export interface ConsensusInsightData {
  id: string;
  repo_id: string;
  task_id: string;
  thread_id: string;
  category: InsightCategory;
  key: string;
  insight: string;
  confidence: number;
  strategy: string;
  provider: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

/** Aggregated provider reliability stats from consensus decisions. */
export interface ProviderReliabilityStatsData {
  provider: string;
  win_count: number;
  total_decisions: number;
  avg_confidence: number;
}

// ─── Role-Based ELO Types ─────────────────────────────────────────

/** Performance tier assigned to a (role, repo) pair. */
export type RoleELOTier = "restricted" | "standard" | "expert";

/** Persistent role-level ELO record — one per (role, repo_id) pair. */
export interface RoleELOData {
  id: string;
  role: string;
  repo_id: string;
  elo_score: number;
  tier: RoleELOTier;
  tasks_done: number;
  tasks_rated: number;
  avg_rating: number;
  wins: number;
  losses: number;
  consensus_avg: number;
  best_strategy: string;
  training_probes: number;
  last_active: string;
  created_at: string;
  updated_at: string;
}

/** A single role ELO history event (append-only audit log). */
export interface RoleELOHistoryData {
  id: string;
  role: string;
  repo_id: string;
  task_id: string;
  event_type: string;
  old_elo: number;
  new_elo: number;
  delta: number;
  detail: Record<string, unknown>;
  created_at: string;
}

/** Leaderboard entry — RoleELOData with a computed rank. */
export interface RoleELOLeaderboardEntry extends RoleELOData {
  rank: number;
}

// ─── CI Signal Types ──────────────────────────────────────────────

/** PR merge state from the VCS platform. */
export type PRState = "open" | "merged" | "closed" | "unknown";

/** Aggregated CI check state. */
export type CIState = "pending" | "success" | "failure" | "error" | "unknown";

/** A single CI check / status. */
export interface CICheckStatus {
  context: string;
  state: CIState;
  description: string;
  target_url: string;
  created_at: string;
}

/** CI signal record for a task — auto-ingested from VCS platform. */
export interface CISignalData {
  id: string;
  task_id: string;
  repo_id: string;
  pr_number: number;
  pr_state: PRState;
  pr_merged: boolean;
  ci_state: CIState;
  ci_total: number;
  ci_passed: number;
  ci_failed: number;
  ci_pending: number;
  ci_details: CICheckStatus[];
  elo_ingested: boolean;
  elo_ingested_at?: string;
  poll_count: number;
  last_polled_at?: string;
  finalized: boolean;
  finalized_at?: string;
  created_at: string;
  updated_at: string;
}

/** Lightweight CI signal summary for lists. */
export interface CISignalSummary {
  task_id: string;
  pr_state: PRState;
  pr_merged: boolean;
  ci_state: CIState;
  ci_total: number;
  ci_passed: number;
  ci_failed: number;
  ci_pending: number;
  finalized: boolean;
  updated_at: string;
}

// ─── Team Formation Types ─────────────────────────────────────────

/** Complexity label assigned by the team formation engine. */
export type ComplexityLabel = "trivial" | "small" | "medium" | "large" | "critical";

/** Team formation strategy used to compute the recommendation. */
export type FormationStrategy = "elo_weighted" | "static";

/** Raw complexity signals extracted from the plan document. */
export interface ComplexitySignalsData {
  file_count: number;
  step_count: number;
  description_length: number;
  language_count: number;
  test_files: number;
  has_migrations: boolean;
  cross_package: boolean;
  has_api_changes: boolean;
  has_security_impact: boolean;
  has_ui_changes: boolean;
  languages: string[];
}

/** ELO summary for a role in the context of a repo. */
export interface RoleELOInfoData {
  elo: number;
  tier: RoleELOTier;
}

/** Full team formation recommendation stored on the task. */
export interface TeamFormationData {
  complexity_score: number;
  complexity_label: ComplexityLabel;
  input_signals: ComplexitySignalsData;
  recommended_roles: string[];
  role_elos: Record<string, RoleELOInfoData>;
  team_size: number;
  reasoning: string;
  strategy: FormationStrategy;
  created_at: string;
}

// ─── Adaptive Probe Tuning Types ──────────────────────────────────

/** Strategy for how probe parameters are determined. */
export type ProbeStrategy = "adaptive" | "static" | "aggressive";

/** Per-role adaptive probe configuration. */
export interface ProbeConfigData {
  id: string;
  role: string;
  repo_id: string;
  action_type: string;
  num_models: number;
  preferred_providers: string[];
  excluded_providers: string[];
  temperature: number;
  max_tokens: number;
  timeout_ms: number;
  budget_cap_usd: number;
  tokens_spent: number;
  strategy: ProbeStrategy;
  confidence_threshold: number;
  max_retries: number;
  reasoning: string;
  last_tuned_at: string;
  created_at: string;
  updated_at: string;
}

/** A single probe outcome recorded by the tuning history. */
export interface ProbeHistoryData {
  id: string;
  role: string;
  repo_id: string;
  action_type: string;
  task_id: string;
  providers_queried: string[];
  providers_succeeded: string[];
  provider_winner: string;
  strategy_used: string;
  consensus_confidence: number;
  provider_latencies: Record<string, number>;
  provider_tokens: Record<string, { prompt: number; completion: number }>;
  total_ms: number;
  total_tokens: number;
  estimated_cost_usd: number;
  success: boolean;
  complexity_label: ComplexityLabel | "";
  num_models_used: number;
  temperature_used: number;
  created_at: string;
}

/** Aggregated provider statistics for a role. */
export interface ProviderStatsData {
  provider: string;
  total_probes: number;
  successes: number;
  success_rate: number;
  wins: number;
  win_rate: number;
  avg_latency_ms: number;
  reliability_score: number;
}

// ─── Self-Healing Pipeline Types ──────────────────────────────────

/** Provider circuit-breaker state. */
export type CircuitState = "closed" | "half_open" | "open";

/** Self-heal event severity. */
export type SelfHealSeverity = "info" | "warn" | "critical";

/** Self-heal event type. */
export type SelfHealEventType =
  | "circuit_opened"
  | "circuit_closed"
  | "circuit_half_open"
  | "task_retry"
  | "task_timeout_recovery"
  | "provider_failover"
  | "agent_restarted"
  | "stuck_task_detected";

/** Per-provider circuit breaker state. */
export interface ProviderCircuitData {
  id: string;
  provider: string;
  state: CircuitState;
  consecutive_failures: number;
  total_failures: number;
  total_successes: number;
  last_failure_at: string | null;
  last_success_at: string | null;
  open_until: string | null;
  half_open_probes: number;
  created_at: string;
  updated_at: string;
}

/** A single self-heal audit event. */
export interface SelfHealEventData {
  id: string;
  event_type: SelfHealEventType;
  provider: string;
  task_id: string | null;
  agent_id: string | null;
  details: Record<string, unknown>;
  severity: SelfHealSeverity;
  resolved: boolean;
  resolved_at: string | null;
  created_at: string;
}

/** Summary returned by GET /api/v1/swarm/self-heal/summary. */
export interface SelfHealSummaryData {
  total_events: number;
  unresolved_events: number;
  open_circuits: number;
  half_open_circuits: number;
  closed_circuits: number;
  providers: ProviderCircuitData[];
  recent_events: SelfHealEventData[];
}

// ─── Observability Dashboard Types ────────────────────────────────

/** A single metric snapshot data point. */
export interface MetricsSnapshotData {
  id: string;
  active_tasks: number;
  pending_tasks: number;
  completed_tasks: number;
  failed_tasks: number;
  online_agents: number;
  busy_agents: number;
  active_teams: number;
  busy_teams: number;
  llm_calls: number;
  llm_tokens: number;
  llm_avg_latency_ms: number;
  llm_error_rate: number;
  probe_calls: number;
  consensus_runs: number;
  consensus_avg_confidence: number;
  open_circuits: number;
  heal_events: number;
  estimated_cost_usd: number;
  queue_depth: number;
  agent_utilisation: number;
  health_score: number;
  created_at: string;
}

/** Per-provider performance data point. */
export interface ProviderPerfData {
  id: string;
  provider: string;
  calls: number;
  successes: number;
  failures: number;
  tokens_used: number;
  avg_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
  error_rate: number;
  estimated_cost_usd: number;
  consensus_wins: number;
  consensus_total: number;
  created_at: string;
}

/** Composite health score breakdown. */
export interface HealthBreakdownData {
  score: number;
  task_health_pct: number;
  agent_health_pct: number;
  provider_health_pct: number;
  queue_health_pct: number;
  error_rate_pct: number;
  details: string;
}

/** Aggregated cost summary. */
export interface CostSummaryData {
  today_usd: number;
  this_week_usd: number;
  this_month_usd: number;
  by_provider: Record<string, number>;
  budget?: CostBudgetData;
}

/** Monthly cost budget. */
export interface CostBudgetData {
  id: string;
  scope: string;
  month: string;
  budget_usd: number;
  spent_usd: number;
  alert_threshold: number;
}

/** Full observability dashboard response. */
export interface ObservabilityDashboardData {
  current: MetricsSnapshotData | null;
  time_series: MetricsSnapshotData[];
  provider_perf: ProviderPerfData[];
  provider_time_series: Record<string, ProviderPerfData[]>;
  health_breakdown: HealthBreakdownData | null;
  cost_summary: CostSummaryData | null;
  uptime_seconds: number;
}
