// ─── Authenticated Layout ────────────────────────────────────────────────────
// Wraps all authenticated pages with Sidebar + TopBar + main content area.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useAuth } from "@/lib/auth/context";
import { Sidebar } from "@/components/layout/sidebar";
import { TopBar } from "@/components/layout/top-bar";
import { useUIStore } from "@/lib/stores/ui";
import { cn } from "@/lib/utils";

export default function AuthenticatedLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { isLoading, isAuthenticated } = useAuth();
  const { sidebarOpen } = useUIStore();

  // Show loading state while checking auth
  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="space-y-4 text-center">
          <div className="mx-auto h-10 w-10 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
          <p className="text-sm text-muted-foreground">Loading…</p>
        </div>
      </div>
    );
  }

  // If not authenticated, middleware should have redirected, but just in case
  if (!isAuthenticated) {
    return null;
  }

  return (
    <div className="min-h-screen bg-background">
      <Sidebar />
      <TopBar />
      <main
        className={cn(
          "pt-14 transition-all duration-300",
          sidebarOpen ? "pl-60" : "pl-16",
        )}
      >
        <div className="container mx-auto space-y-6 p-6">{children}</div>
      </main>
    </div>
  );
}
