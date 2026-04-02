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

// Index action — controls clone/git behaviour.
export type IndexAction = "index" | "reindex" | "reclone";

// Request body for POST /repos/{id}/index
export interface TriggerIndexRequest {
  action?: IndexAction;
  target_branch?: string;
}

// Response from GET /repos/{id}/branches
export interface BranchListResponse {
  branches: string[];
  default_branch: string;
  count: number;
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

// ── Agent Orchestration ─────────────────────────────────────────────────────

/** A single role → provider/model mapping. */
export interface AgentRoute {
  role: string;
  provider: string;
  model?: string;
}

/** Response from GET /api/v1/llm/routes */
export interface AgentRoutesResponse {
  routes: AgentRoute[];
  primary: string;
}

/** All agent roles in the review swarm. */
export const AGENT_ROLES = [
  "orchestrator",
  "architect",
  "senior_dev",
  "junior_dev",
  "qa",
  "security",
  "docs",
  "ops",
  "ui_ux",
] as const;

export type AgentRoleId = (typeof AGENT_ROLES)[number];

/** Human-readable labels and descriptions for each agent role. */
export const AGENT_ROLE_META: Record<AgentRoleId, { label: string; description: string; icon: string }> = {
  orchestrator: { label: "Orchestrator", description: "Plans and coordinates the overall review", icon: "🎯" },
  architect:    { label: "Architect",    description: "Architecture, design patterns, trade-offs", icon: "🏗️" },
  senior_dev:   { label: "Senior Dev",   description: "Deep code review, correctness, edge cases", icon: "👨‍💻" },
  junior_dev:   { label: "Junior Dev",   description: "Style, formatting, simple improvements", icon: "🧑‍💻" },
  qa:           { label: "QA",           description: "Test coverage and test quality", icon: "🧪" },
  security:     { label: "Security",     description: "Vulnerabilities, CVEs, OWASP checks", icon: "🔒" },
  docs:         { label: "Docs",         description: "Documentation quality, comments, READMEs", icon: "📝" },
  ops:          { label: "Ops",          description: "CI/CD, deployment, infrastructure", icon: "⚙️" },
  ui_ux:        { label: "UI/UX",       description: "UI components, styling, accessibility, UX patterns", icon: "🎨" },
};

// ── Embeddings ──────────────────────────────────────────────────────────────

export interface BuiltinEmbeddingModel {
  name: string;
  display_name: string;
  provider: string;
  dimensions: number;
  size_mb: number;
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
  active_builtin_model: string;
  builtin_models: BuiltinEmbeddingModel[];
  external_providers: ExternalEmbeddingProvider[];
}

export interface EmbeddingsUpdateRequest {
  use_builtin: boolean;
  builtin_model?: string;
  provider?: string;
  endpoint?: string;
  model?: string;
  dimensions?: number;
  api_key?: string;
}

export interface EmbeddingsUpdateResult {
  use_builtin: boolean;
  builtin_model: string;
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

// ── Multimodal Embeddings ───────────────────────────────────────────────────

export interface ModalityInfo {
  modality: "text" | "image" | "audio";
  model_name: string;
  enabled: boolean;
  status: "ready" | "downloading" | "pending" | "error";
  native_dimension: number;
  projected_dimension: number;
  description: string;
  size_mb: number;
  download_progress: number;
}

export interface MultimodalConfig {
  modalities: ModalityInfo[];
  unified_dimension: number;
  image_enabled: boolean;
  audio_enabled: boolean;
}

export interface MultimodalUpdateRequest {
  image_enabled?: boolean;
  audio_enabled?: boolean;
  image_model?: string;
  audio_model?: string;
}

export interface MultimodalUpdateResult {
  image_enabled: boolean;
  audio_enabled: boolean;
  image_model: string;
  audio_model: string;
  status: string;
}

// ── Assets ──────────────────────────────────────────────────────────────────

export type AssetType = "pdf" | "image" | "audio" | "video" | "webpage" | "document";
export type AssetStatus = "processing" | "ready" | "error";

export interface Asset {
  id: string;
  repo_id: string;
  asset_type: AssetType;
  source_url?: string;
  file_name?: string;
  mime_type?: string;
  size_bytes: number;
  chunks_count: number;
  status: AssetStatus;
  error_message?: string;
  metadata?: string;
  created_at: string;
  updated_at: string;
}

export interface AssetUploadResult {
  id: string;
  repo_id: string;
  asset_type: AssetType;
  file_name: string;
  mime_type: string;
  size_bytes: number;
  status: string;
}

export interface AssetIngestURLResult {
  id: string;
  repo_id: string;
  asset_type: string;
  source_url: string;
  status: string;
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
  type: "file" | "code_snippet" | "image" | "pdf" | "audio" | "url";
  filename: string;
  content: string;
  language?: string;
  mime_type?: string;
  size?: number;
  data_uri?: string;
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

// ── MCP Integrations ────────────────────────────────────────────────────────

export interface MCPActionDef {
  name: string;
  description: string;
  required_params?: string[];
  optional_params?: string[];
  consent_required: boolean;
}

export interface MCPProviderInfo {
  name: string;
  category: string;
  description: string;
  actions: MCPActionDef[];
}

export interface MCPConnection {
  id: string;
  user_id: string;
  org_id?: string;
  is_org_level: boolean;
  provider: string;
  status: "pending" | "active" | "expired" | "revoked" | "error";
  scopes: string[];
  metadata?: string;
  last_used_at?: string;
  connected_at: string;
  expires_at?: string;
  created_at: string;
}

export interface MCPCallLogEntry {
  id: string;
  connection_id: string;
  agent_id: string;
  task_id: string;
  action: string;
  input_hash: string;
  output_hash: string;
  latency_ms: number;
  status: "ok" | "error" | "rate_limited" | "consent_denied";
  error_message?: string;
  created_at: string;
}

export interface MCPTestResult {
  success: boolean;
  data?: Record<string, unknown>;
  error?: string;
}

// ── Custom MCP Templates ────────────────────────────────────────────────────

export interface CustomMCPActionDef {
  name: string;
  description: string;
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  required_params?: string[];
  optional_params?: string[];
  body_template?: string;
  consent_required: boolean;
}

export interface CustomMCPTemplate {
  id: string;
  name: string;
  label: string;
  category: string;
  description: string;
  base_url: string;
  auth_type: "bearer" | "basic" | "header" | "query";
  auth_header?: string;
  actions: CustomMCPActionDef[];
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface MCPValidationError {
  field: string;
  message: string;
}

export interface MCPValidationResult {
  valid: boolean;
  validation_errors?: MCPValidationError[];
}

export interface MCPSimulateResult {
  success: boolean;
  data?: Record<string, unknown>;
  error?: string;
}

// ── Keychain Vault ──────────────────────────────────────────────────────────

export interface KeychainStatus {
  initialized: boolean;
  key_version: number;
  secret_count: number;
}

export interface KeychainInitResponse {
  recovery_phrase: string;
}

export interface KeychainSecret {
  name: string;
  value: string;
}

export interface KeychainSecretListEntry {
  name: string;
  version: number;
  category?: string;
  updated_at: string;
}

export interface KeychainPutSecretRequest {
  name: string;
  value: string;
  category?: string;
  metadata?: string;
}

export interface KeychainRecoverRequest {
  recovery_phrase: string;
}

export interface KeychainAuditLogEntry {
  id: string;
  action: string;
  secret_name?: string;
  ip_addr?: string;
  user_agent?: string;
  created_at: string;
}

export interface KeychainSyncRequest {
  client_versions: Record<string, number>;
}

export interface KeychainSyncVersionEntry {
  name: string;
  version: number;
  category?: string;
  updated_at: string;
}

export interface KeychainSyncResponse {
  updated: KeychainSyncVersionEntry[];
  deleted: string[];
  server_versions: Record<string, number>;
}
