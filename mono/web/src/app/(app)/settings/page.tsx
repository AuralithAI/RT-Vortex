// ─── Settings Page ───────────────────────────────────────────────────────────
// Tabbed settings: Profile, LLM Configuration, Agent Orchestration, Embeddings,
// Version Control.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/layout/page-header";
import { ProfileSettings } from "@/components/settings/profile-settings";
import { LLMSettings } from "@/components/settings/llm-settings";
import { AgentOrchestration } from "@/components/settings/agent-orchestration";
import { EmbeddingsSettings } from "@/components/settings/embeddings-settings";
import { VCSSettings } from "@/components/settings/vcs-settings";

export default function SettingsPage() {
  return (
    <>
      <PageHeader
        title="Settings"
        description="Manage your profile and application settings"
      />

      <Tabs defaultValue="profile" className="space-y-4">
        <TabsList>
          <TabsTrigger value="profile">Profile</TabsTrigger>
          <TabsTrigger value="llm">LLM Configuration</TabsTrigger>
          <TabsTrigger value="orchestration">Agent Orchestration</TabsTrigger>
          <TabsTrigger value="embeddings">Embeddings</TabsTrigger>
          <TabsTrigger value="vcs">Version Control</TabsTrigger>
        </TabsList>

        <TabsContent value="profile">
          <ProfileSettings />
        </TabsContent>

        <TabsContent value="llm">
          <LLMSettings />
        </TabsContent>

        <TabsContent value="orchestration">
          <AgentOrchestration />
        </TabsContent>

        <TabsContent value="embeddings">
          <EmbeddingsSettings />
        </TabsContent>

        <TabsContent value="vcs">
          <VCSSettings />
        </TabsContent>
      </Tabs>
    </>
  );
}
