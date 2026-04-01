// ── User ──

export interface User {
  id: string;
  email: string;
  display_name: string;
  avatar_url: string;
  provider: string;
  created_at?: string;
}

export interface UserUpdateRequest {
  display_name?: string;
  avatar_url?: string;
}

// ── Organization ──

export interface Org {
  id: string;
  name: string;
  slug: string;
  plan: string;
  settings?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

export interface OrgMember {
  user_id: string;
  email: string;
  display_name: string;
  avatar_url: string;
  role: string;
  joined_at?: string;
}

// ── Repository ──

export interface Repo {
  id: string;
  org_id: string;
  platform: string;
  owner: string;
  name: string;
  default_branch: string;
  clone_url: string;
  external_id: string;
  webhook_secret: string;
  config?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

// ── Review ──

export interface Review {
  id: string;
  repo_id: string;
  pr_number: number;
  status: string;
  comments_count: number;
  current_step: string;
  total_steps?: number;
  steps_completed: number;
  created_at?: string;
  completed_at?: string;
  metadata?: Record<string, unknown>;
}

export interface ReviewComment {
  id: string;
  review_id: string;
  file_path: string;
  line_number: number;
  severity: string;
  category: string;
  message: string;
  suggestion: string;
  created_at?: string;
}

// ── Streaming ──

export interface ProgressEvent {
  event: string;
  step: string;
  step_index: number;
  total_steps: number;
  status: string;
  message: string;
  metadata?: Record<string, unknown>;
}

// ── Index ──

export interface IndexStatus {
  repo_id: string;
  status: string;
  progress: number;
  job_id: string;
  started_at?: string;
  completed_at?: string;
}

// ── Admin ──

export interface AdminStats {
  total_users: number;
  total_orgs: number;
  total_repos: number;
  total_reviews: number;
  reviews_today: number;
  active_jobs: number;
}

export interface HealthStatus {
  status: string;
  checks?: Record<string, string>;
  time: string;
}

// ── Pagination ──

export interface PaginationOptions {
  limit?: number;
  offset?: number;
}

export interface PaginatedResponse<T> {
  total: number;
  limit: number;
  offset: number;
  items: T[];
}

export interface OrgListResponse {
  total: number;
  limit: number;
  offset: number;
  organizations: Org[];
}

export interface MemberListResponse {
  total: number;
  limit: number;
  offset: number;
  members: OrgMember[];
}

export interface RepoListResponse {
  total: number;
  limit: number;
  offset: number;
  repositories: Repo[];
}

export interface ReviewListResponse {
  total: number;
  limit: number;
  offset: number;
  reviews: Review[];
}

// ── Keychain Vault ──

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
