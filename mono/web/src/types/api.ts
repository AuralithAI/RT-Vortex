// ─── RTVortex API Types ──────────────────────────────────────────────────────
// Mirrors the Go server's JSON responses.
// ─────────────────────────────────────────────────────────────────────────────

// ── Auth ────────────────────────────────────────────────────────────────────

export interface AuthProvider {
  name: string;
  display_name: string;
  enabled: boolean;
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_at: string;
}

// ── User ────────────────────────────────────────────────────────────────────

export interface User {
  id: string;
  email: string;
  name: string;
  avatar_url: string;
  role: "user" | "admin";
  provider: string;
  created_at: string;
  updated_at: string;
}

// ── Organization ────────────────────────────────────────────────────────────

export interface Org {
  id: string;
  name: string;
  slug: string;
  plan: "free" | "pro" | "enterprise";
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface OrgMember {
  user_id: string;
  org_id: string;
  role: "owner" | "admin" | "member";
  user: User;
  joined_at: string;
}

// ── Repository ──────────────────────────────────────────────────────────────

export interface Repo {
  id: string;
  org_id: string;
  name: string;
  full_name: string;
  clone_url: string;
  platform: "github" | "gitlab" | "bitbucket" | "azure-devops";
  default_branch: string;
  is_indexed: boolean;
  last_indexed_at: string | null;
  webhook_secret: string;
  created_at: string;
  updated_at: string;
}

export interface IndexStatus {
  repo_id: string;
  status: "idle" | "indexing" | "completed" | "failed";
  progress: number;
  phase?: string;
  message?: string;
  files_total: number;
  files_indexed: number;
  files_processed?: number;
  current_file?: string;
  eta_seconds?: number;
  job_id?: string;
  started_at: string | null;
  completed_at: string | null;
  error: string | null;
}

// WebSocket indexing progress event
export interface IndexProgressEvent {
  type: "index_progress";
  repo_id: string;
  job_id: string;
  state: string;
  progress: number;
  phase: string;
  message?: string;
  files_processed: number;
  files_total: number;
  current_file?: string;
  eta_seconds: number;
  error?: string;
  timestamp: string;
}

// ── Review ──────────────────────────────────────────────────────────────────

export type ReviewStatus =
  | "pending"
  | "in_progress"
  | "completed"
  | "failed"
  | "cancelled";

export type Severity = "critical" | "warning" | "suggestion" | "info" | "praise";

export interface Review {
  id: string;
  repo_id: string;
  repo_name: string;
  pr_number: number;
  pr_title: string;
  pr_url: string;
  author: string;
  status: ReviewStatus;
  summary: string;
  stats: ReviewStats;
  created_at: string;
  completed_at: string | null;
  duration_ms: number | null;
}

export interface ReviewStats {
  total_comments: number;
  critical: number;
  warnings: number;
  suggestions: number;
  info: number;
  praise: number;
  files_reviewed: number;
}

export interface ReviewComment {
  id: string;
  review_id: string;
  file_path: string;
  line_start: number;
  line_end: number;
  severity: Severity;
  category: string;
  title: string;
  body: string;
  suggestion: string | null;
  created_at: string;
}

// ── Review Progress (WebSocket) ─────────────────────────────────────────────

export interface ReviewProgressEvent {
  step: number;
  total_steps: number;
  label: string;
  status: "pending" | "running" | "completed" | "failed";
  detail: string | null;
  timestamp: string;
}

// ── LLM ─────────────────────────────────────────────────────────────────────

export interface LLMProvider {
  name: string;
  display_name: string;
  base_url: string;
  default_model: string;
  configured: boolean;
  requires_key: boolean;
  healthy: boolean;
  models: string[];
}

export interface LLMTestResult {
  provider: string;
  healthy: boolean;
  model?: string;
  response?: string;
  error?: string;
  usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
}

export interface LLMConfigureRequest {
  api_key?: string;
  model?: string;
  base_url?: string;
}

export interface LLMConfigureResult {
  provider: string;
  configured: boolean;
  healthy: boolean;
}

// Balance check result from POST /api/v1/llm/providers/{provider}/balance
export interface LLMBalanceResult {
  provider: string;
  status: "ok" | "low_balance" | "rate_limited" | "not_configured" | "error" | "unknown";
  message?: string;
  warning?: string;
}

// ── Embeddings ──────────────────────────────────────────────────────────────

export interface BuiltinEmbeddingModel {
  name: string;
  provider: string;
  dimensions: number;
  description: string;
}

export interface ExternalEmbeddingProvider {
  name: string;
  display_name: string;
  model: string;
  dimensions: number;
  endpoint: string;
  configured: boolean;
  requires_key: boolean;
}

export interface EmbeddingsConfig {
  use_builtin: boolean;
  active_provider: string;
  builtin_model: BuiltinEmbeddingModel;
  external_providers: ExternalEmbeddingProvider[];
}

export interface EmbeddingsUpdateRequest {
  use_builtin: boolean;
  provider?: string;
  endpoint?: string;
  model?: string;
  dimensions?: number;
  api_key?: string;
}

export interface EmbeddingsUpdateResult {
  use_builtin: boolean;
  provider: string;
  model: string;
  dimensions: number;
}

// ── Admin ───────────────────────────────────────────────────────────────────

export interface SystemStats {
  total_users: number;
  total_repos: number;
  total_reviews: number;
  total_orgs: number;
  reviews_today: number;
  reviews_this_week: number;
  avg_review_time_ms: number;
  active_indexing_jobs: number;
}

export interface DetailedHealth {
  status: "healthy" | "degraded" | "unhealthy";
  uptime_seconds: number;
  version: string;
  components: HealthComponent[];
}

export interface HealthComponent {
  name: string;
  status: "up" | "down" | "degraded";
  latency_ms: number;
  message: string;
}

// ── Pagination ──────────────────────────────────────────────────────────────

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  limit: number;
  offset: number;
  has_more: boolean;
}

export interface PaginationParams {
  limit?: number;
  offset?: number;
}
