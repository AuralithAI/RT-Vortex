// ─── Settings Page ───────────────────────────────────────────────────────────
// Tabbed settings: Profile, LLM Configuration, Embeddings.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/layout/page-header";
import { ProfileSettings } from "@/components/settings/profile-settings";
import { LLMSettings } from "@/components/settings/llm-settings";
import { EmbeddingsSettings } from "@/components/settings/embeddings-settings";

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
          <TabsTrigger value="embeddings">Embeddings</TabsTrigger>
        </TabsList>

        <TabsContent value="profile">
          <ProfileSettings />
        </TabsContent>

        <TabsContent value="llm">
          <LLMSettings />
        </TabsContent>

        <TabsContent value="embeddings">
          <EmbeddingsSettings />
        </TabsContent>
      </Tabs>
    </>
  );
}
