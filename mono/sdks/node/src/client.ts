/**
 * HTTP client for AI-PR-Reviewer API
 */

import { fetch, type RequestInit } from "undici";
import {
  type ReviewRequest,
  type ReviewResponse,
  type IndexRequest,
  type IndexResponse,
  ReviewStatus,
} from "./types";
import {
  AIPRAPIError,
  AIPRConnectionError,
  AIPRTimeoutError,
} from "./errors";

/**
 * Client configuration options
 */
export interface AIPRClientOptions {
  /** Base URL of the AIPR API */
  baseUrl?: string;
  /** API key for authentication */
  apiKey?: string;
  /** Request timeout in milliseconds */
  timeout?: number;
}

/**
 * Wait options for polling operations
 */
export interface WaitOptions {
  /** Polling interval in milliseconds */
  pollInterval?: number;
  /** Maximum wait time in milliseconds */
  timeout?: number;
}

/**
 * Convert camelCase to snake_case for API requests
 */
function toSnakeCase(obj: Record<string, unknown>): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    const snakeKey = key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);
    if (value && typeof value === "object" && !Array.isArray(value)) {
      result[snakeKey] = toSnakeCase(value as Record<string, unknown>);
    } else if (Array.isArray(value)) {
      result[snakeKey] = value.map((item) =>
        typeof item === "object" && item !== null
          ? toSnakeCase(item as Record<string, unknown>)
          : item
      );
    } else {
      result[snakeKey] = value;
    }
  }
  return result;
}

/**
 * Convert snake_case to camelCase for API responses
 */
function toCamelCase(obj: Record<string, unknown>): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    const camelKey = key.replace(/_([a-z])/g, (_, letter) => letter.toUpperCase());
    if (value && typeof value === "object" && !Array.isArray(value)) {
      result[camelKey] = toCamelCase(value as Record<string, unknown>);
    } else if (Array.isArray(value)) {
      result[camelKey] = value.map((item) =>
        typeof item === "object" && item !== null
          ? toCamelCase(item as Record<string, unknown>)
          : item
      );
    } else {
      result[camelKey] = value;
    }
  }
  return result;
}

/**
 * Main client for interacting with the AI-PR-Reviewer API
 *
 * @example
 * ```typescript
 * const client = new AIPRClient({
 *   baseUrl: "https://api.aipr.example.com",
 *   apiKey: "your-api-key",
 * });
 *
 * const response = await client.review({
 *   repositoryUrl: "https://github.com/owner/repo",
 *   pullRequestId: 123,
 * });
 *
 * for (const comment of response.comments) {
 *   console.log(`[${comment.severity}] ${comment.file}:${comment.line} - ${comment.message}`);
 * }
 * ```
 */
export class AIPRClient {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly timeout: number;
  private readonly headers: Record<string, string>;

  constructor(options: AIPRClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "http://localhost:8080").replace(/\/$/, "");
    this.apiKey = options.apiKey;
    this.timeout = options.timeout ?? 300000; // 5 minutes default

    this.headers = {
      "Content-Type": "application/json",
      Accept: "application/json",
      "User-Agent": "aipr-node-sdk/0.1.0",
    };

    if (this.apiKey) {
      this.headers["Authorization"] = `Bearer ${this.apiKey}`;
    }
  }

  /**
   * Submit a pull request for review
   *
   * @param request - The review request
   * @returns The review response with comments and metadata
   */
  async review(request: ReviewRequest): Promise<ReviewResponse> {
    return this.post<ReviewResponse>("/api/v1/reviews", request);
  }

  /**
   * Get the status of a review by ID
   *
   * @param reviewId - The review ID
   * @returns The review response
   */
  async getReview(reviewId: string): Promise<ReviewResponse> {
    return this.get<ReviewResponse>(`/api/v1/reviews/${reviewId}`);
  }

  /**
   * Wait for a review to complete
   *
   * @param reviewId - The review ID
   * @param options - Wait options
   * @returns The completed review response
   */
  async waitForReview(
    reviewId: string,
    options: WaitOptions = {}
  ): Promise<ReviewResponse> {
    const pollInterval = options.pollInterval ?? 5000;
    const timeout = options.timeout;
    const startTime = Date.now();

    while (true) {
      const response = await this.getReview(reviewId);

      if (this.isComplete(response)) {
        return response;
      }

      if (timeout && Date.now() - startTime >= timeout) {
        throw new AIPRTimeoutError(
          `Review ${reviewId} did not complete within ${timeout}ms`
        );
      }

      await this.sleep(pollInterval);
    }
  }

  /**
   * Start indexing a repository
   *
   * @param request - The index request
   * @returns The index job response
   */
  async index(request: IndexRequest): Promise<IndexResponse> {
    return this.post<IndexResponse>("/api/v1/index", request);
  }

  /**
   * Get the status of an indexing job
   *
   * @param jobId - The job ID
   * @returns The index job response
   */
  async getIndexStatus(jobId: string): Promise<IndexResponse> {
    return this.get<IndexResponse>(`/api/v1/index/${jobId}`);
  }

  /**
   * Wait for an indexing job to complete
   *
   * @param jobId - The job ID
   * @param options - Wait options
   * @returns The completed index response
   */
  async waitForIndex(
    jobId: string,
    options: WaitOptions = {}
  ): Promise<IndexResponse> {
    const pollInterval = options.pollInterval ?? 5000;
    const timeout = options.timeout;
    const startTime = Date.now();

    while (true) {
      const response = await this.getIndexStatus(jobId);

      const status = response.status.toLowerCase();
      if (status === "completed" || status === "failed") {
        return response;
      }

      if (timeout && Date.now() - startTime >= timeout) {
        throw new AIPRTimeoutError(
          `Index job ${jobId} did not complete within ${timeout}ms`
        );
      }

      await this.sleep(pollInterval);
    }
  }

  /**
   * Check if the API server is healthy
   *
   * @returns True if the server is healthy
   */
  async isHealthy(): Promise<boolean> {
    try {
      const response = await fetch(`${this.baseUrl}/actuator/health`, {
        method: "GET",
        headers: this.headers,
      });
      return response.ok;
    } catch {
      return false;
    }
  }

  private async get<T>(path: string): Promise<T> {
    return this.request<T>("GET", path);
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>("POST", path, body);
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    const url = `${this.baseUrl}${path}`;

    const init: RequestInit = {
      method,
      headers: this.headers,
    };

    if (body) {
      init.body = JSON.stringify(toSnakeCase(body as Record<string, unknown>));
    }

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), this.timeout);
      init.signal = controller.signal;

      const response = await fetch(url, init);
      clearTimeout(timeoutId);

      const responseText = await response.text();

      if (!response.ok) {
        throw new AIPRAPIError(
          `API request failed: ${response.status} ${response.statusText}`,
          response.status,
          responseText
        );
      }

      const json = JSON.parse(responseText) as Record<string, unknown>;
      return toCamelCase(json) as T;
    } catch (error) {
      if (error instanceof AIPRAPIError) {
        throw error;
      }

      if (error instanceof Error) {
        if (error.name === "AbortError") {
          throw new AIPRTimeoutError(`Request to ${path} timed out`);
        }
        if (error.message.includes("ECONNREFUSED") || error.message.includes("ENOTFOUND")) {
          throw new AIPRConnectionError(`Failed to connect to ${url}`);
        }
      }

      throw new AIPRConnectionError(
        `Request failed: ${error instanceof Error ? error.message : String(error)}`
      );
    }
  }

  private isComplete(response: ReviewResponse): boolean {
    const status = response.status.toLowerCase();
    return status === ReviewStatus.Completed || status === ReviewStatus.Failed;
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}
