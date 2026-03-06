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

// ── Helpers ─────────────────────────────────────────────────────────────────

/** In-memory token store. Populated by the callback page after OAuth. */
let _accessToken: string | null = null;

/** Set the access token (called from callback page). */
export function setAccessToken(token: string): void {
  _accessToken = token;
  // Also persist to localStorage so it survives page reloads.
  if (typeof window !== "undefined") {
    try { localStorage.setItem("rtvortex_token", token); } catch { /* ignore */ }
  }
}

/** Clear the access token (called on logout). */
export function clearAccessToken(): void {
  _accessToken = null;
  if (typeof window !== "undefined") {
    try { localStorage.removeItem("rtvortex_token"); } catch { /* ignore */ }
  }
}

/** Get the current access token. */
function getAccessToken(): string | null {
  if (_accessToken) return _accessToken;
  // Try localStorage on first access.
  if (typeof window !== "undefined") {
    try {
      const stored = localStorage.getItem("rtvortex_token");
      if (stored) { _accessToken = stored; return stored; }
    } catch { /* ignore */ }
  }
  // Fall back to cookie.
  if (typeof document !== "undefined") {
    const match = document.cookie.match(/(?:^|;\s*)token=([^;]*)/);
    if (match?.[1]) return match[1];
  }
  return null;
}

// ── Core fetch wrapper ─────────────────────────────────────────────────────

const BASE = getApiBaseUrl();

/** Public endpoints that should never send an Authorization header. */
const PUBLIC_PATHS = new Set([
  "/api/v1/auth/providers",
  "/api/v1/auth/refresh",
]);

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const url = `${BASE}${path}`;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string>),
  };

  // Inject Authorization header from stored token — but NOT for public
  // auth endpoints, which must work regardless of token state.
  const isPublic = PUBLIC_PATHS.has(path) || path.startsWith("/api/v1/auth/login/");
  const token = getAccessToken();
  if (token && !isPublic && !headers["Authorization"]) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(url, {
    ...init,
    headers,
  });

  if (res.status === 401 && !isPublic) {
    // Try refresh once — the refresh handler returns a new token pair.
    const refreshed = await tryRefreshToken();
    if (refreshed) {
      // Re-read token after refresh (tryRefreshToken calls setAccessToken).
      const newToken = getAccessToken();
      const retryHeaders = { ...headers };
      if (newToken) {
        retryHeaders["Authorization"] = `Bearer ${newToken}`;
      }
      const retry = await fetch(url, {
        ...init,
        headers: retryHeaders,
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
    // Build the request body — include the refresh token from localStorage
    // so it works cross-origin (cookies are not sent cross-origin).
    const body: Record<string, string> = {};
    if (typeof window !== "undefined") {
      try {
        const rt = localStorage.getItem("rtvortex_refresh_token");
        if (rt) body.refresh_token = rt;
      } catch { /* ignore */ }
    }

    const res = await fetch(`${BASE}/api/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include", // also send cookie if same-origin
      body: JSON.stringify(body),
    });

    if (!res.ok) return false;

    // The server returns { access_token, refresh_token, expires_in }.
    // Store the new tokens so subsequent requests use them.
    const data = await res.json();
    if (data.access_token) {
      setAccessToken(data.access_token);
    }
    if (data.refresh_token && typeof window !== "undefined") {
      try { localStorage.setItem("rtvortex_refresh_token", data.refresh_token); } catch { /* ignore */ }
    }
    return true;
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

  logout: () => {
    clearAccessToken();
    // Also clear cookies and refresh token.
    if (typeof document !== "undefined") {
      document.cookie = "token=; path=/; max-age=0";
    }
    if (typeof window !== "undefined") {
      try { localStorage.removeItem("rtvortex_refresh_token"); } catch { /* ignore */ }
    }
    return request<void>("/api/v1/auth/logout", { method: "POST" }).catch(() => {});
  },
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
