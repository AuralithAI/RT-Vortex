"use client";

import { useParams } from "next/navigation";
import { RepoChat } from "@/components/chat/repo-chat";
import { useRepo } from "@/lib/api/queries";
import { Skeleton } from "@/components/ui/skeleton";

export default function RepoChatPage() {
  const params = useParams<{ id: string }>();
  const repoId = params.id;
  const { data: repo, isLoading } = useRepo(repoId);

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-[calc(100vh-12rem)] w-full rounded-xl" />
      </div>
    );
  }

  return (
    <div className="p-6 space-y-4">
      <div>
        <h1 className="text-xl font-semibold text-zinc-100">Code Chat</h1>
        <p className="text-sm text-zinc-500">
          Ask questions about {repo?.full_name ?? repo?.name ?? "this repository"}
        </p>
      </div>
      <RepoChat repoId={repoId} repoName={repo?.full_name ?? repo?.name ?? "Repository"} />
    </div>
  );
}
