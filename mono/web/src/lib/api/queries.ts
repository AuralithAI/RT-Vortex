// ─── TanStack Query Hooks ────────────────────────────────────────────────────
// Read-only queries with caching, pagination, and auto-refresh.
// ─────────────────────────────────────────────────────────────────────────────

import { useQuery, useInfiniteQuery } from "@tanstack/react-query";
import api from "./client";
import type { PaginationParams, PRListFilter } from "@/types/api";

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
  llmRoutes: ["llm", "routes"] as const,
  embeddingsConfig: ["embeddings", "config"] as const,
  multimodalConfig: ["embeddings", "multimodal"] as const,
  adminStats: ["admin", "stats"] as const,
  adminHealth: ["admin", "health"] as const,
  pullRequests: (repoId: string, p?: PaginationParams, f?: PRListFilter) =>
    ["repos", repoId, "pull-requests", p, f] as const,
  pullRequest: (repoId: string, prId: string) =>
    ["repos", repoId, "pull-requests", prId] as const,
  pullRequestStats: (repoId: string) =>
    ["repos", repoId, "pull-requests", "stats"] as const,
  chatSessions: (repoId: string) =>
    ["repos", repoId, "chat", "sessions"] as const,
  chatSession: (repoId: string, sessionId: string) =>
    ["repos", repoId, "chat", "sessions", sessionId] as const,
  chatMessages: (repoId: string, sessionId: string) =>
    ["repos", repoId, "chat", "sessions", sessionId, "messages"] as const,
  vcsPlatforms: ["vcs", "platforms"] as const,
  branches: (repoId: string) =>
    ["repos", repoId, "branches"] as const,
  assets: (repoId: string) =>
    ["repos", repoId, "assets"] as const,
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
      // Poll while indexing, queued, or pending
      const status = query.state.data?.status;
      if (status === "indexing") return 2000;
      if (status === "idle") return false;
      // For pending/other active states, poll less aggressively
      return status && status !== "completed" && status !== "failed" ? 5000 : false;
    },
  });
}

export function useBranches(repoId: string, enabled = false) {
  return useQuery({
    queryKey: queryKeys.branches(repoId),
    queryFn: () => api.repos.branches(repoId),
    enabled: !!repoId && enabled,
    staleTime: 60 * 1000, // branches don't change that often
  });
}

// ── Assets ──────────────────────────────────────────────────────────────────

export function useAssets(repoId: string) {
  return useQuery({
    queryKey: queryKeys.assets(repoId),
    queryFn: () => api.assets.list(repoId),
    enabled: !!repoId,
    // Auto-refresh while any asset is still processing
    refetchInterval: (query: any) => {
      const data = query.state.data as import("@/types/api").Asset[] | undefined;
      if (data?.some((a) => a.status === "processing")) return 3000;
      return false;
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

export function useLLMRoutes() {
  return useQuery({
    queryKey: queryKeys.llmRoutes,
    queryFn: () => api.llm.routes(),
  });
}

export function useEmbeddingsConfig() {
  return useQuery({
    queryKey: queryKeys.embeddingsConfig,
    queryFn: () => api.embeddings.config(),
  });
}

export function useMultimodalConfig() {
  return useQuery({
    queryKey: queryKeys.multimodalConfig,
    queryFn: () => api.embeddings.multimodal(),
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

// ── Pull Requests ───────────────────────────────────────────────────────────

export function usePullRequests(
  repoId: string,
  params?: PaginationParams,
  filter?: PRListFilter,
) {
  return useQuery({
    queryKey: queryKeys.pullRequests(repoId, params, filter),
    queryFn: () => api.pullRequests.list(repoId, params, filter),
    enabled: !!repoId,
  });
}

export function usePullRequest(repoId: string, prId: string) {
  return useQuery({
    queryKey: queryKeys.pullRequest(repoId, prId),
    queryFn: () => api.pullRequests.get(repoId, prId),
    enabled: !!repoId && !!prId,
    refetchInterval: (query) => {
      const status = query.state.data?.review_status;
      return status === "pending" ? 3000 : false;
    },
  });
}

export function usePullRequestStats(repoId: string) {
  return useQuery({
    queryKey: queryKeys.pullRequestStats(repoId),
    queryFn: () => api.pullRequests.stats(repoId),
    enabled: !!repoId,
    refetchInterval: 15_000, // Poll every 15s to keep embed queue / counts fresh
  });
}

// ── Chat ────────────────────────────────────────────────────────────────────

export function useChatSessions(repoId: string) {
  return useQuery({
    queryKey: queryKeys.chatSessions(repoId),
    queryFn: async () => {
      const res = await api.chat.sessions(repoId, { limit: 50 });
      return res.sessions ?? [];
    },
    enabled: !!repoId,
  });
}

export function useChatSession(repoId: string, sessionId: string) {
  return useQuery({
    queryKey: queryKeys.chatSession(repoId, sessionId),
    queryFn: () => api.chat.getSession(repoId, sessionId),
    enabled: !!repoId && !!sessionId,
  });
}

export function useChatMessages(repoId: string, sessionId: string) {
  return useQuery({
    queryKey: queryKeys.chatMessages(repoId, sessionId),
    queryFn: async () => {
      const res = await api.chat.messages(repoId, sessionId, { limit: 200 });
      return res.messages ?? [];
    },
    enabled: !!repoId && !!sessionId,
  });
}

// ── VCS ─────────────────────────────────────────────────────────────────────

export function useVCSPlatforms() {
  return useQuery({
    queryKey: queryKeys.vcsPlatforms,
    queryFn: () => api.vcsPlatforms.list(),
  });
}

export function useVCSTokenCapabilities(platform?: string) {
  return useQuery({
    queryKey: ["vcs", "token-capabilities", platform ?? "all"] as const,
    queryFn: () => api.vcsPlatforms.tokenCapabilities(platform),
  });
}

// ── Benchmark ───────────────────────────────────────────────────────────────

export const benchmarkKeys = {
  summary: ["benchmark", "summary"] as const,
  runs: ["benchmark", "runs"] as const,
  run: (id: string) => ["benchmark", "runs", id] as const,
  ratings: ["benchmark", "ratings"] as const,
} as const;
