/**
 * Type definitions for AI-PR-Reviewer API
 */

/**
 * Status of a review
 */
export enum ReviewStatus {
  Pending = "pending",
  Processing = "processing",
  Completed = "completed",
  Failed = "failed",
}

/**
 * Severity levels for review comments
 */
export enum CommentSeverity {
  Info = "info",
  Warning = "warning",
  Error = "error",
  Critical = "critical",
}

/**
 * Categories for review comments
 */
export enum CommentCategory {
  Security = "security",
  Performance = "performance",
  Testing = "testing",
  Architecture = "architecture",
  CodeStyle = "code_style",
  Documentation = "documentation",
  General = "general",
}

/**
 * Represents a file change in a pull request
 */
export interface FileChange {
  /** File path */
  path: string;
  /** Change status: added, modified, deleted, renamed */
  status?: string;
  /** Number of lines added */
  additions?: number;
  /** Number of lines deleted */
  deletions?: number;
  /** Unified diff patch */
  patch?: string;
}

/**
 * Additional context for the review
 */
export interface ReviewContext {
  /** Pull request title */
  prTitle?: string;
  /** Pull request description */
  prDescription?: string;
  /** Author username */
  author?: string;
  /** PR labels */
  labels?: string[];
}

/**
 * Request to submit a pull request for review
 */
export interface ReviewRequest {
  /** Repository URL */
  repositoryUrl: string;
  /** Pull request number */
  pullRequestId?: number;
  /** Base commit SHA */
  baseSha?: string;
  /** Head commit SHA */
  headSha?: string;
  /** Raw diff content */
  diffContent?: string;
  /** Changed files */
  files?: FileChange[];
  /** PR context */
  context?: ReviewContext;
  /** Additional configuration */
  config?: Record<string, unknown>;
}

/**
 * A single review comment
 */
export interface ReviewComment {
  /** File path */
  file: string;
  /** Line number */
  line?: number;
  /** End line number for multi-line comments */
  endLine?: number;
  /** Severity level */
  severity: CommentSeverity | string;
  /** Comment category */
  category: CommentCategory | string;
  /** Comment message */
  message: string;
  /** Suggested fix */
  suggestion?: string;
  /** Confidence score 0-1 */
  confidence?: number;
}

/**
 * Metrics from the review
 */
export interface ReviewMetrics {
  /** Number of files reviewed */
  filesReviewed: number;
  /** Number of lines reviewed */
  linesReviewed: number;
  /** Total number of comments */
  totalComments: number;
  /** Number of critical issues */
  criticalIssues: number;
  /** Processing time in milliseconds */
  processingTimeMs?: number;
  /** LLM tokens used */
  llmTokensUsed?: number;
}

/**
 * Response from a review request
 */
export interface ReviewResponse {
  /** Unique review ID */
  reviewId: string;
  /** Review status */
  status: ReviewStatus | string;
  /** Overall assessment */
  overallAssessment?: string;
  /** Review summary */
  summary?: string;
  /** Review comments */
  comments: ReviewComment[];
  /** Review metrics */
  metrics?: ReviewMetrics;
  /** Creation timestamp */
  createdAt?: string;
  /** Completion timestamp */
  completedAt?: string;
  /** Error message if failed */
  error?: string;
}

/**
 * Request to index a repository
 */
export interface IndexRequest {
  /** Repository URL */
  repositoryUrl: string;
  /** Branch to index */
  branch?: string;
  /** Specific commit SHA */
  commitSha?: string;
  /** File patterns to include */
  includePatterns?: string[];
  /** File patterns to exclude */
  excludePatterns?: string[];
  /** Force re-indexing */
  forceReindex?: boolean;
}

/**
 * Response from an index request
 */
export interface IndexResponse {
  /** Unique job ID */
  jobId: string;
  /** Job status */
  status: string;
  /** Repository URL */
  repositoryUrl?: string;
  /** Branch being indexed */
  branch?: string;
  /** Commit SHA */
  commitSha?: string;
  /** Number of files indexed */
  filesIndexed?: number;
  /** Number of chunks created */
  chunksCreated?: number;
  /** Progress percentage 0-100 */
  progressPercent?: number;
  /** Creation timestamp */
  createdAt?: string;
  /** Completion timestamp */
  completedAt?: string;
  /** Error message if failed */
  error?: string;
}
