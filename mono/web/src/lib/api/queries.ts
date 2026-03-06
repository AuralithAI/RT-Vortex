// ─── TanStack Query Hooks ────────────────────────────────────────────────────
// Read-only queries with caching, pagination, and auto-refresh.
// ─────────────────────────────────────────────────────────────────────────────

import { useQuery, useInfiniteQuery } from "@tanstack/react-query";
import api from "./client";
import type { PaginationParams } from "@/types/api";

// ── Keys ────────────────────────────────────────────────────────────────────

export const queryKeys = {
  me: ["user", "me"] as const,
  providers: ["auth", "providers"] as const,
  orgs: (p?: PaginationParams) => ["orgs", p] as const,
  org: (id: string) => ["orgs", id] as const,
  orgMembers: (id: string, p?: PaginationParams) => ["orgs", id, "members", p] as const,
  repos: (p?: PaginationParams) => ["repos", p] as const,
  repo: (id: string) => ["repos", id] as const,
  indexStatus: (id: string) => ["repos", id, "index-status"] as const,
  reviews: (p?: PaginationParams & { repo_id?: string }) => ["reviews", p] as const,
  review: (id: string) => ["reviews", id] as const,
  reviewComments: (id: string) => ["reviews", id, "comments"] as const,
  llmProviders: ["llm", "providers"] as const,
  adminStats: ["admin", "stats"] as const,
  adminHealth: ["admin", "health"] as const,
} as const;

// ── Auth ────────────────────────────────────────────────────────────────────

export function useAuthProviders() {
  return useQuery({
    queryKey: queryKeys.providers,
    queryFn: () => api.auth.providers(),
    staleTime: 5 * 60 * 1000, // providers rarely change
    retry: 3, // retry up to 3 times on transient failures (e.g. 429, network)
    retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 8000), // 1s, 2s, 4s
    refetchOnWindowFocus: true, // refetch when user returns to tab
  });
}

// ── User ────────────────────────────────────────────────────────────────────

export function useMe() {
  return useQuery({
    queryKey: queryKeys.me,
    queryFn: () => api.users.me(),
    retry: false, // don't retry on 401
  });
}

// ── Organizations ───────────────────────────────────────────────────────────

export function useOrgs(params?: PaginationParams) {
  return useQuery({
    queryKey: queryKeys.orgs(params),
    queryFn: () => api.orgs.list(params),
  });
}

export function useOrg(id: string) {
  return useQuery({
    queryKey: queryKeys.org(id),
    queryFn: () => api.orgs.get(id),
    enabled: !!id,
  });
}

export function useOrgMembers(orgId: string, params?: PaginationParams) {
  return useQuery({
    queryKey: queryKeys.orgMembers(orgId, params),
    queryFn: () => api.orgs.members(orgId, params),
    enabled: !!orgId,
  });
}

// ── Repositories ────────────────────────────────────────────────────────────

export function useRepos(params?: PaginationParams) {
  return useQuery({
    queryKey: queryKeys.repos(params),
    queryFn: () => api.repos.list(params),
  });
}

export function useRepo(id: string) {
  return useQuery({
    queryKey: queryKeys.repo(id),
    queryFn: () => api.repos.get(id),
    enabled: !!id,
  });
}

export function useIndexStatus(repoId: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.indexStatus(repoId),
    queryFn: () => api.repos.indexStatus(repoId),
    enabled: !!repoId && enabled,
    refetchInterval: (query) => {
      // Poll while indexing
      const status = query.state.data?.status;
      return status === "indexing" ? 2000 : false;
    },
  });
}

// ── Reviews ─────────────────────────────────────────────────────────────────

export function useReviews(params?: PaginationParams & { repo_id?: string }) {
  return useQuery({
    queryKey: queryKeys.reviews(params),
    queryFn: () => api.reviews.list(params),
  });
}

export function useReviewsInfinite(repoId?: string) {
  return useInfiniteQuery({
    queryKey: ["reviews", "infinite", repoId],
    queryFn: ({ pageParam = 0 }) =>
      api.reviews.list({ limit: 20, offset: pageParam as number, repo_id: repoId }),
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? lastPage.offset + lastPage.limit : undefined,
    initialPageParam: 0,
  });
}

export function useReview(id: string) {
  return useQuery({
    queryKey: queryKeys.review(id),
    queryFn: () => api.reviews.get(id),
    enabled: !!id,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "in_progress" || status === "pending" ? 3000 : false;
    },
  });
}

export function useReviewComments(reviewId: string) {
  return useQuery({
    queryKey: queryKeys.reviewComments(reviewId),
    queryFn: () => api.reviews.comments(reviewId),
    enabled: !!reviewId,
  });
}

// ── LLM ─────────────────────────────────────────────────────────────────────

export function useLLMProviders() {
  return useQuery({
    queryKey: queryKeys.llmProviders,
    queryFn: () => api.llm.providers(),
  });
}

// ── Admin ───────────────────────────────────────────────────────────────────

export function useAdminStats() {
  return useQuery({
    queryKey: queryKeys.adminStats,
    queryFn: () => api.admin.stats(),
    refetchInterval: 30_000, // refresh every 30s
  });
}

export function useDetailedHealth() {
  return useQuery({
    queryKey: queryKeys.adminHealth,
    queryFn: () => api.admin.health(),
    refetchInterval: 15_000,
  });
}
