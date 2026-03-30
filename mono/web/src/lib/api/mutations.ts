// ─── TanStack Query Mutations ────────────────────────────────────────────────
// Write operations with optimistic updates and cache invalidation.
// ─────────────────────────────────────────────────────────────────────────────

import { useMutation, useQueryClient } from "@tanstack/react-query";
import api from "./client";
import { queryKeys } from "./queries";
import type { User, Org, EmbeddingsUpdateRequest, EmbeddingTestRequest, AgentRoute, MultimodalUpdateRequest } from "@/types/api";

// ── User ────────────────────────────────────────────────────────────────────

export function useUpdateMe() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: Record<string, string>) => api.users.updateMe(data),
    onSuccess: (user) => {
      qc.setQueryData(queryKeys.me, user);
    },
  });
}

// ── Organizations ───────────────────────────────────────────────────────────

export function useCreateOrg() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { name: string; slug: string; plan?: string }) =>
      api.orgs.create(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orgs"] });
    },
  });
}

export function useUpdateOrg() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<Org> }) =>
      api.orgs.update(id, data),
    onSuccess: (_updated, { id }) => {
      qc.invalidateQueries({ queryKey: queryKeys.org(id) });
      qc.invalidateQueries({ queryKey: ["orgs"] });
    },
  });
}

export function useInviteMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ orgId, email, role }: { orgId: string; email: string; role: string }) =>
      api.orgs.inviteMember(orgId, email, role),
    onSuccess: (_data, { orgId }) => {
      qc.invalidateQueries({ queryKey: ["orgs", orgId, "members"] });
    },
  });
}

export function useRemoveMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ orgId, userId }: { orgId: string; userId: string }) =>
      api.orgs.removeMember(orgId, userId),
    onSuccess: (_data, { orgId }) => {
      qc.invalidateQueries({ queryKey: ["orgs", orgId, "members"] });
    },
  });
}

// ── Repositories ────────────────────────────────────────────────────────────

export function useCreateRepo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: Record<string, string>) => api.repos.create(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["repos"] });
    },
  });
}

export function useUpdateRepo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) =>
      api.repos.update(id, data),
    onSuccess: (_updated, { id }) => {
      qc.invalidateQueries({ queryKey: queryKeys.repo(id) });
      qc.invalidateQueries({ queryKey: ["repos"] });
    },
  });
}

export function useDeleteRepo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.repos.delete(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["repos"] });
    },
  });
}

export function useTriggerIndex() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoId, action, targetBranch }: {
      repoId: string;
      action?: "index" | "reindex" | "reclone";
      targetBranch?: string;
    }) => api.repos.triggerIndex(repoId, {
      action: action ?? "index",
      target_branch: targetBranch,
    }),
    onSuccess: (_data, { repoId }) => {
      // Invalidate both the index status and repo queries to trigger re-fetch
      qc.invalidateQueries({ queryKey: queryKeys.indexStatus(repoId) });
      qc.invalidateQueries({ queryKey: queryKeys.repo(repoId) });
    },
  });
}

// ── Reviews ─────────────────────────────────────────────────────────────────

export function useTriggerReview() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { repo_id: string; pr_number: number; pr_url: string }) =>
      api.reviews.trigger(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["reviews"] });
    },
  });
}

// ── LLM ─────────────────────────────────────────────────────────────────────

export function useTestLLM() {
  return useMutation({
    mutationFn: (data: {
      provider: string;
      api_key?: string;
      model?: string;
      base_url?: string;
    }) => api.llm.test(data),
  });
}

export function useConfigureLLM() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { provider: string; api_key?: string; model?: string; base_url?: string }) =>
      api.llm.configure(data.provider, {
        api_key: data.api_key,
        model: data.model,
        base_url: data.base_url,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["llm", "providers"] });
    },
  });
}

export function useSetPrimaryLLM() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (provider: string) => api.llm.setPrimary(provider),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["llm", "providers"] });
    },
  });
}

export function useCheckLLMBalance() {
  return useMutation({
    mutationFn: (provider: string) => api.llm.balance(provider),
  });
}

export function useSetLLMRoutes() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (routes: AgentRoute[]) => api.llm.setRoutes(routes),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.llmRoutes });
    },
  });
}

// ── Embeddings ──────────────────────────────────────────────────────────────

export function useUpdateEmbeddings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: EmbeddingsUpdateRequest) => api.embeddings.update(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.embeddingsConfig });
    },
  });
}

export function useTestEmbedding() {
  return useMutation({
    mutationFn: (data: EmbeddingTestRequest) => api.embeddings.test(data),
  });
}

export function useCheckEmbeddingCredits() {
  return useMutation({
    mutationFn: (data: { provider: string; endpoint?: string; api_key?: string }) =>
      api.embeddings.credits(data),
  });
}

export function useUpdateMultimodal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: MultimodalUpdateRequest) => api.embeddings.updateMultimodal(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.multimodalConfig });
    },
  });
}

// ── Auth ────────────────────────────────────────────────────────────────────

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.auth.logout(),
    onSuccess: () => {
      qc.clear();
      if (typeof window !== "undefined") {
        window.location.href = "/login";
      }
    },
  });
}

// ── Pull Requests ───────────────────────────────────────────────────────────

export function useSyncPullRequests() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (repoId: string) => api.pullRequests.sync(repoId),
    onSuccess: (_data, repoId) => {
      // Invalidate all PR queries for this repo
      qc.invalidateQueries({ queryKey: ["repos", repoId, "pull-requests"] });
    },
  });
}

export function useReviewPullRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoId, prId }: { repoId: string; prId: string }) =>
      api.pullRequests.review(repoId, prId),
    onSuccess: (_data, { repoId }) => {
      qc.invalidateQueries({ queryKey: ["repos", repoId, "pull-requests"] });
      qc.invalidateQueries({ queryKey: ["reviews"] });
    },
  });
}

// ── Chat ────────────────────────────────────────────────────────────────────

export function useCreateChatSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoId, title }: { repoId: string; title?: string }) =>
      api.chat.createSession(repoId, { title }),
    onSuccess: (_data, { repoId }) => {
      qc.invalidateQueries({ queryKey: queryKeys.chatSessions(repoId) });
    },
  });
}

export function useUpdateChatSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoId, sessionId, title }: { repoId: string; sessionId: string; title: string }) =>
      api.chat.updateSession(repoId, sessionId, title),
    onSuccess: (_data, { repoId }) => {
      qc.invalidateQueries({ queryKey: queryKeys.chatSessions(repoId) });
    },
  });
}

export function useDeleteChatSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoId, sessionId }: { repoId: string; sessionId: string }) =>
      api.chat.deleteSession(repoId, sessionId),
    onSuccess: (_data, { repoId }) => {
      qc.invalidateQueries({ queryKey: queryKeys.chatSessions(repoId) });
    },
  });
}

// ── VCS Platform Settings ───────────────────────────────────────────────────

export function useConfigureVCS() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ platform, fields }: { platform: string; fields: Record<string, string> }) =>
      api.vcsPlatforms.configure(platform, fields),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.vcsPlatforms });
    },
  });
}

export function useDeleteVCS() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (platform: string) => api.vcsPlatforms.remove(platform),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.vcsPlatforms });
    },
  });
}

export function useTestVCS() {
  return useMutation({
    mutationFn: (platform: string) => api.vcsPlatforms.test(platform),
  });
}

export function useCheckClonePermission() {
  return useMutation({
    mutationFn: ({ platform, cloneUrl }: { platform: string; cloneUrl: string }) =>
      api.vcsPlatforms.checkClone(platform, cloneUrl),
  });
}
