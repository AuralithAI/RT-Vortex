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

// WebSocket PR embed progress event
export interface PREmbedProgressEvent {
  type: "pr_embed_progress";
  repo_id: string;
  pr_number: number;
  pr_id: string;
  state: "embedding" | "completed" | "failed";
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

export interface EmbeddingModelOption {
  name: string;
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
  available_models?: EmbeddingModelOption[];
}

export interface EmbeddingsConfig {
  use_builtin: boolean;
  active_provider: string;
  active_model: string;
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
  configured: boolean;
}

export interface EmbeddingTestRequest {
  provider: string;
  endpoint: string;
  model: string;
  api_key?: string;
}

export interface EmbeddingTestResult {
  provider: string;
  healthy: boolean;
  error?: string;
  model?: string;
  dimensions?: number;
  status_code?: number;
}

export interface EmbeddingCreditsResult {
  provider: string;
  status: "ok" | "low_balance" | "rate_limited" | "not_configured" | "error" | "unknown";
  message?: string;
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

// ── Tracked Pull Requests ───────────────────────────────────────────────────

export type PRSyncStatus =
  | "open"
  | "closed"
  | "merged"
  | "draft"
  | "stale"
  | "embedded"
  | "embedding"
  | "embed_error";

export type PRReviewStatus =
  | "none"
  | "pending"
  | "completed"
  | "skipped";

export interface TrackedPullRequest {
  id: string;
  repo_id: string;
  platform: string;
  pr_number: number;
  external_id: string;
  title: string;
  description: string;
  author: string;
  source_branch: string;
  target_branch: string;
  head_sha: string;
  base_sha: string;
  pr_url: string;
  sync_status: PRSyncStatus;
  review_status: PRReviewStatus;
  last_review_id?: string | null;
  files_changed: number;
  additions: number;
  deletions: number;
  embedded_at?: string | null;
  embed_error?: string;
  synced_at: string;
  created_at: string;
  updated_at: string;
}

export interface PRListFilter {
  sync_status?: PRSyncStatus;
  review_status?: PRReviewStatus;
  author?: string;
  target_branch?: string;
}

export interface PRStats {
  counts: Record<string, number>;
  embed_queue: number;
}

// ── Chat ────────────────────────────────────────────────────────────────────

export interface ChatSession {
  id: string;
  repo_id: string;
  user_id: string;
  title: string;
  message_count: number;
  last_message_at: string | null;
  model: string;
  provider: string;
  created_at: string;
  updated_at: string;
}

export type ChatMessageRole = "user" | "assistant" | "system";

export interface ChatCitation {
  file_path: string;
  start_line: number;
  end_line: number;
  content: string;
  language: string;
  relevance_score: number;
  symbols?: string[];
}

export interface ChatAttachment {
  type: "file" | "code_snippet" | "image";
  filename: string;
  content: string;
  language?: string;
  mime_type?: string;
  size?: number;
}

export interface ChatMessage {
  id: string;
  session_id: string;
  role: ChatMessageRole;
  content: string;
  encrypted: boolean;
  citations?: ChatCitation[];
  attachments?: ChatAttachment[];
  prompt_tokens: number;
  completion_tokens: number;
  search_time_ms: number;
  chunks_retrieved: number;
  created_at: string;
}

export interface ChatStreamEvent {
  type: "delta" | "citation" | "thinking" | "done" | "error";
  content?: string;
  citation?: ChatCitation;
  phase?: string;
  message?: string;
  message_id?: string;
  prompt_tokens?: number;
  completion_tokens?: number;
  search_time_ms?: number;
  chunks_retrieved?: number;
  error?: string;
}

// ── VCS Platform Settings ───────────────────────────────────────────────────

export interface VCSFieldInfo {
  key: string;
  label: string;
  secret: boolean;
  has_value: boolean;
  value: string;
  default_value?: string;
  hint?: string;
}

export interface VCSPlatformInfo {
  name: string;
  display_name: string;
  configured: boolean;
  fields: VCSFieldInfo[];
}

export interface VCSConfigureResult {
  platform: string;
  saved_secrets: number;
  saved_config: number;
}

export interface VCSTestResult {
  platform: string;
  success: boolean;
  message?: string;
  error?: string;
}

// ── VCS Token Capabilities ──────────────────────────────────────────────────

export interface VCSTokenCapability {
  token_type: string;
  label: string;
  can_clone: boolean;
  can_review: boolean;
  can_webhook: boolean;
  can_read_pr: boolean;
  scopes: string[];
  setup_guide: string;
}

export interface VCSCloneCheckResult {
  platform: string;
  can_clone: boolean;
  reason: string;
  has_token: boolean;
  needs_different: boolean;
}

// ── Engine Metrics ──────────────────────────────────

export interface HistogramSnapshot {
  count: number;
  sum: number;
  min_val: number;
  max_val: number;
  avg: number;
  p50: number;
  p90: number;
  p95: number;
  p99: number;
}

export interface MetricValue {
  type: "counter" | "gauge" | "histogram";
  scalar?: number;
  histogram?: HistogramSnapshot;
}

export interface EngineMetricsSnapshot {
  timestamp_ms: number;
  metrics: Record<string, MetricValue>;
  uptime_s: number;
  index_sizes_bytes?: Record<string, number>;
  knowledge_graph_nodes?: number;
  knowledge_graph_edges?: number;
}

export interface EngineMetricsWSEvent {
  type: "engine_metrics";
  data: EngineMetricsSnapshot;
}

export interface EngineHealthResponse {
  healthy: boolean;
  version: string;
  uptime_seconds: number;
  components: Record<string, string>;
  metrics_enabled: boolean;
  active_metric_streams: number;
  has_latest_snapshot: boolean;
}
