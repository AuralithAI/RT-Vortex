// ─── Typed API Client ────────────────────────────────────────────────────────
// All requests go via same-origin (/api/…) when behind nginx (Option A),
// or to NEXT_PUBLIC_API_URL when deployed to CDN (Option B).
// Auth token is read from the httpOnly cookie automatically (same-origin)
// or sent as Authorization header (cross-origin).
// ─────────────────────────────────────────────────────────────────────────────

import { getApiBaseUrl } from "@/lib/utils";
import type {
  AuthProvider,
  DetailedHealth,
  IndexStatus,
  LLMProvider,
  LLMTestResult,
  Org,
  OrgMember,
  PaginatedResponse,
  PaginationParams,
  Repo,
  Review,
  ReviewComment,
  SystemStats,
  User,
} from "@/types/api";

// ── Error classes ───────────────────────────────────────────────────────────

export class ApiError extends Error {
  constructor(
    public status: number,
    public statusText: string,
    public body: unknown,
  ) {
    super(`${status} ${statusText}`);
    this.name = "ApiError";
  }
}

export class AuthError extends ApiError {
  constructor(body: unknown) {
    super(401, "Unauthorized", body);
    this.name = "AuthError";
  }
}

// ── Core fetch wrapper ─────────────────────────────────────────────────────

const BASE = getApiBaseUrl();

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const url = `${BASE}${path}`;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string>),
  };

  const res = await fetch(url, {
    ...init,
    headers,
    credentials: "include", // send cookies (same-origin via nginx)
  });

  if (res.status === 401) {
    // Try refresh once
    const refreshed = await tryRefreshToken();
    if (refreshed) {
      const retry = await fetch(url, {
        ...init,
        headers,
        credentials: "include",
      });
      if (retry.ok) {
        if (retry.status === 204) return undefined as T;
        return retry.json() as Promise<T>;
      }
    }
    throw new AuthError(await res.json().catch(() => null));
  }

  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new ApiError(res.status, res.statusText, body);
  }

  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

async function tryRefreshToken(): Promise<boolean> {
  try {
    const res = await fetch(`${BASE}/api/v1/auth/refresh`, {
      method: "POST",
      credentials: "include",
    });
    return res.ok;
  } catch {
    return false;
  }
}

function qs(params?: PaginationParams): string {
  if (!params) return "";
  const parts: string[] = [];
  if (params.limit != null) parts.push(`limit=${params.limit}`);
  if (params.offset != null) parts.push(`offset=${params.offset}`);
  return parts.length ? `?${parts.join("&")}` : "";
}

// ── Auth ────────────────────────────────────────────────────────────────────

export const auth = {
  providers: () =>
    request<AuthProvider[]>("/api/v1/auth/providers"),

  loginUrl: (provider: string, redirectUrl: string) =>
    `${BASE}/api/v1/auth/login/${provider}?redirect_url=${encodeURIComponent(redirectUrl)}`,

  logout: () =>
    request<void>("/api/v1/auth/logout", { method: "POST" }),
};

// ── User ────────────────────────────────────────────────────────────────────

export const users = {
  me: () =>
    request<User>("/api/v1/user/me"),

  updateMe: (fields: Partial<Pick<User, "name" | "email">>) =>
    request<User>("/api/v1/user/me", {
      method: "PUT",
      body: JSON.stringify(fields),
    }),
};

// ── Organizations ───────────────────────────────────────────────────────────

export const orgs = {
  list: (p?: PaginationParams) =>
    request<PaginatedResponse<Org>>(`/api/v1/orgs${qs(p)}`),

  get: (id: string) =>
    request<Org>(`/api/v1/orgs/${id}`),

  create: (data: { name: string; slug: string; plan?: string }) =>
    request<Org>("/api/v1/orgs", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  update: (id: string, fields: Partial<Org>) =>
    request<Org>(`/api/v1/orgs/${id}`, {
      method: "PUT",
      body: JSON.stringify(fields),
    }),

  members: (id: string, p?: PaginationParams) =>
    request<PaginatedResponse<OrgMember>>(`/api/v1/orgs/${id}/members${qs(p)}`),

  inviteMember: (orgId: string, email: string, role: string) =>
    request<OrgMember>(`/api/v1/orgs/${orgId}/members`, {
      method: "POST",
      body: JSON.stringify({ email, role }),
    }),

  removeMember: (orgId: string, userId: string) =>
    request<void>(`/api/v1/orgs/${orgId}/members/${userId}`, {
      method: "DELETE",
    }),
};

// ── Repositories ────────────────────────────────────────────────────────────

export const repos = {
  list: (p?: PaginationParams) =>
    request<PaginatedResponse<Repo>>(`/api/v1/repos${qs(p)}`),

  get: (id: string) =>
    request<Repo>(`/api/v1/repos/${id}`),

  create: (data: Record<string, string>) =>
    request<Repo>("/api/v1/repos", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  update: (id: string, fields: Record<string, unknown>) =>
    request<Repo>(`/api/v1/repos/${id}`, {
      method: "PUT",
      body: JSON.stringify(fields),
    }),

  delete: (id: string) =>
    request<void>(`/api/v1/repos/${id}`, { method: "DELETE" }),

  triggerIndex: (id: string) =>
    request<void>(`/api/v1/repos/${id}/index`, { method: "POST" }),

  indexStatus: (id: string) =>
    request<IndexStatus>(`/api/v1/repos/${id}/index/status`),
};

// ── Reviews ─────────────────────────────────────────────────────────────────

export const reviews = {
  list: (p?: PaginationParams & { repo_id?: string }) => {
    const params = new URLSearchParams();
    if (p?.limit != null) params.set("limit", String(p.limit));
    if (p?.offset != null) params.set("offset", String(p.offset));
    if (p?.repo_id) params.set("repo_id", p.repo_id);
    const q = params.toString();
    return request<PaginatedResponse<Review>>(`/api/v1/reviews${q ? `?${q}` : ""}`);
  },

  get: (id: string) =>
    request<Review>(`/api/v1/reviews/${id}`),

  trigger: (data: { repo_id: string; pr_number: number; pr_url: string }) =>
    request<Review>("/api/v1/reviews", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  comments: (id: string) =>
    request<ReviewComment[]>(`/api/v1/reviews/${id}/comments`),
};

// ── LLM ─────────────────────────────────────────────────────────────────────

export const llm = {
  providers: () =>
    request<LLMProvider[]>("/api/v1/llm/providers"),

  test: (data: {
    provider: string;
    api_key?: string;
    model?: string;
    base_url?: string;
  }) =>
    request<LLMTestResult>("/api/v1/llm/providers/test", {
      method: "POST",
      body: JSON.stringify(data),
    }),
};

// ── Admin ───────────────────────────────────────────────────────────────────

export const admin = {
  stats: () =>
    request<SystemStats>("/api/v1/admin/stats"),

  health: () =>
    request<DetailedHealth>("/api/v1/admin/health/detailed"),
};

// ── Convenience export ──────────────────────────────────────────────────────

const api = { auth, users, orgs, repos, reviews, llm, admin };
export default api;
