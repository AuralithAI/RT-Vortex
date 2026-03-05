/**
 * RTVortex API client for Node.js / TypeScript.
 */

import { throwForStatus } from "./errors.js";
import { iterSSEEvents } from "./streaming.js";
import type {
  AdminStats,
  HealthStatus,
  IndexStatus,
  MemberListResponse,
  Org,
  OrgListResponse,
  OrgMember,
  PaginationOptions,
  ProgressEvent,
  Repo,
  RepoListResponse,
  Review,
  ReviewComment,
  ReviewListResponse,
  User,
  UserUpdateRequest,
} from "./types.js";

const DEFAULT_BASE_URL = "https://api.rtvortex.dev";
const USER_AGENT = "@rtvortex/sdk/0.1.0";

export interface RTVortexClientOptions {
  /** API bearer token. */
  token: string;
  /** Base URL of the RTVortex API (defaults to https://api.rtvortex.dev). */
  baseUrl?: string;
  /** Request timeout in milliseconds (default 30 000). */
  timeout?: number;
  /** Custom fetch implementation (defaults to globalThis.fetch). */
  fetch?: typeof globalThis.fetch;
}

export class RTVortexClient {
  private readonly baseUrl: string;
  private readonly token: string;
  private readonly timeout: number;
  private readonly _fetch: typeof globalThis.fetch;

  constructor(options: RTVortexClientOptions) {
    this.baseUrl = (options.baseUrl ?? DEFAULT_BASE_URL).replace(/\/+$/, "");
    this.token = options.token;
    this.timeout = options.timeout ?? 30_000;
    this._fetch = options.fetch ?? globalThis.fetch;
  }

  // ── Internal helpers ──────────────────────────────────────────────────────

  private async request<T>(
    method: string,
    path: string,
    options?: {
      params?: Record<string, string | number>;
      body?: unknown;
    },
  ): Promise<T> {
    let url = `${this.baseUrl}${path}`;
    if (options?.params) {
      const sp = new URLSearchParams();
      for (const [k, v] of Object.entries(options.params)) {
        if (v !== undefined) sp.set(k, String(v));
      }
      const qs = sp.toString();
      if (qs) url += `?${qs}`;
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);

    try {
      const resp = await this._fetch(url, {
        method,
        headers: {
          Authorization: `Bearer ${this.token}`,
          "Content-Type": "application/json",
          Accept: "application/json",
          "User-Agent": USER_AGENT,
        },
        body: options?.body ? JSON.stringify(options.body) : undefined,
        signal: controller.signal,
      });

      await throwForStatus(resp);

      if (resp.status === 204) return undefined as T;
      return (await resp.json()) as T;
    } finally {
      clearTimeout(timer);
    }
  }

  private paginationParams(
    opts?: PaginationOptions,
  ): Record<string, number> | undefined {
    if (!opts) return undefined;
    const p: Record<string, number> = {};
    if (opts.limit !== undefined) p.limit = opts.limit;
    if (opts.offset !== undefined) p.offset = opts.offset;
    return p;
  }

  // ── User ──────────────────────────────────────────────────────────────────

  async me(): Promise<User> {
    return this.request("GET", "/user/me");
  }

  async updateMe(update: UserUpdateRequest): Promise<User> {
    return this.request("PUT", "/user/me", { body: update });
  }

  // ── Organizations ─────────────────────────────────────────────────────────

  async listOrgs(pagination?: PaginationOptions): Promise<OrgListResponse> {
    return this.request("GET", "/orgs", {
      params: this.paginationParams(pagination),
    });
  }

  async createOrg(data: {
    name: string;
    slug: string;
    plan?: string;
  }): Promise<Org> {
    return this.request("POST", "/orgs", { body: data });
  }

  async getOrg(orgId: string): Promise<Org> {
    return this.request("GET", `/orgs/${orgId}`);
  }

  async updateOrg(
    orgId: string,
    update: Partial<Pick<Org, "name" | "slug" | "plan">>,
  ): Promise<Org> {
    return this.request("PUT", `/orgs/${orgId}`, { body: update });
  }

  // ── Org Members ───────────────────────────────────────────────────────────

  async listMembers(
    orgId: string,
    pagination?: PaginationOptions,
  ): Promise<MemberListResponse> {
    return this.request("GET", `/orgs/${orgId}/members`, {
      params: this.paginationParams(pagination),
    });
  }

  async inviteMember(
    orgId: string,
    data: { email: string; role?: string },
  ): Promise<OrgMember> {
    return this.request("POST", `/orgs/${orgId}/members`, { body: data });
  }

  async removeMember(orgId: string, userId: string): Promise<void> {
    return this.request("DELETE", `/orgs/${orgId}/members/${userId}`);
  }

  // ── Repositories ──────────────────────────────────────────────────────────

  async listRepos(
    pagination?: PaginationOptions,
  ): Promise<RepoListResponse> {
    return this.request("GET", "/repos", {
      params: this.paginationParams(pagination),
    });
  }

  async registerRepo(data: {
    org_id: string;
    platform: string;
    owner: string;
    name: string;
    clone_url?: string;
  }): Promise<Repo> {
    return this.request("POST", "/repos", { body: data });
  }

  async getRepo(repoId: string): Promise<Repo> {
    return this.request("GET", `/repos/${repoId}`);
  }

  async updateRepo(
    repoId: string,
    fields: Record<string, unknown>,
  ): Promise<Repo> {
    return this.request("PUT", `/repos/${repoId}`, { body: fields });
  }

  async deleteRepo(repoId: string): Promise<void> {
    return this.request("DELETE", `/repos/${repoId}`);
  }

  // ── Reviews ───────────────────────────────────────────────────────────────

  async listReviews(
    pagination?: PaginationOptions,
  ): Promise<ReviewListResponse> {
    return this.request("GET", "/reviews", {
      params: this.paginationParams(pagination),
    });
  }

  async triggerReview(data: {
    repo_id: string;
    pr_number: number;
    [key: string]: unknown;
  }): Promise<Review> {
    return this.request("POST", "/reviews", { body: data });
  }

  async getReview(reviewId: string): Promise<Review> {
    return this.request("GET", `/reviews/${reviewId}`);
  }

  async getReviewComments(reviewId: string): Promise<ReviewComment[]> {
    const data = await this.request<unknown>(
      "GET",
      `/reviews/${reviewId}/comments`,
    );
    if (Array.isArray(data)) return data as ReviewComment[];
    if (
      typeof data === "object" &&
      data !== null &&
      "comments" in data
    ) {
      return (data as { comments: ReviewComment[] }).comments;
    }
    return [];
  }

  /**
   * Stream review progress events via SSE.
   */
  async *streamReview(
    reviewId: string,
  ): AsyncGenerator<ProgressEvent, void, undefined> {
    const url = `${this.baseUrl}/reviews/${reviewId}/ws`;
    const controller = new AbortController();

    const resp = await this._fetch(url, {
      method: "GET",
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: "text/event-stream",
        "User-Agent": USER_AGENT,
      },
      signal: controller.signal,
    });

    await throwForStatus(resp);

    if (!resp.body) {
      throw new Error("Response body is null — streaming not supported");
    }

    try {
      yield* iterSSEEvents(resp.body);
    } finally {
      controller.abort();
    }
  }

  // ── Indexing ──────────────────────────────────────────────────────────────

  async triggerIndex(repoId: string): Promise<IndexStatus> {
    return this.request("POST", `/repos/${repoId}/index`);
  }

  async getIndexStatus(repoId: string): Promise<IndexStatus> {
    return this.request("GET", `/repos/${repoId}/index/status`);
  }

  // ── Admin ─────────────────────────────────────────────────────────────────

  async getStats(): Promise<AdminStats> {
    return this.request("GET", "/admin/stats");
  }

  async health(): Promise<HealthStatus> {
    return this.request("GET", "/health");
  }

  async healthDetailed(): Promise<HealthStatus> {
    return this.request("GET", "/admin/health/detailed");
  }
}
