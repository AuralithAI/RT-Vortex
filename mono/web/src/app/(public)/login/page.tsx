// ─── Login Page ──────────────────────────────────────────────────────────────
// Shows available OAuth providers. Clicking one redirects to the Go server's
// /api/v1/auth/login/:provider endpoint which handles the OAuth flow.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { useAuthProviders } from "@/lib/api/queries";
import { getApiBaseUrl } from "@/lib/utils";

function LoginContent() {
  const searchParams = useSearchParams();
  const returnTo = searchParams.get("returnTo") ?? "/dashboard";
  const { data: providers, isLoading, error } = useAuthProviders();

  const getCallbackUrl = () => {
    if (typeof window === "undefined") return "";
    return `${window.location.origin}/callback?returnTo=${encodeURIComponent(returnTo)}`;
  };

  const handleLogin = (providerId: string) => {
    const callbackUrl = getCallbackUrl();
    const base = getApiBaseUrl();
    window.location.href = `${base}/api/v1/auth/login/${providerId}?redirect_url=${encodeURIComponent(callbackUrl)}`;
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-blue-50 to-indigo-100 dark:from-gray-950 dark:to-gray-900">
      <div className="w-full max-w-md space-y-8 rounded-xl border bg-white p-8 shadow-lg dark:border-gray-800 dark:bg-gray-950">
        {/* Header */}
        <div className="text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-xl bg-blue-600 text-2xl font-bold text-white">
            V
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-gray-900 dark:text-gray-100">
            Welcome to RTVortex
          </h1>
          <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
            AI-powered code review for your pull requests
          </p>
        </div>

        {/* Provider Grid */}
        <div className="space-y-3">
          {isLoading && (
            <div className="flex justify-center py-8">
              <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
            </div>
          )}

          {error && (
            <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-center text-sm text-red-600 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
              Failed to load login providers. Please try again later.
            </div>
          )}

          {providers?.map((provider) => (
            <button
              key={provider.name}
              onClick={() => handleLogin(provider.name)}
              className="flex w-full items-center gap-3 rounded-lg border border-gray-200 px-4 py-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:border-gray-800 dark:text-gray-300 dark:hover:bg-gray-900"
            >
              <ProviderIcon provider={provider.name} />
              <span>Continue with {provider.display_name || provider.name}</span>
            </button>
          ))}
        </div>

        {/* Footer */}
        <p className="text-center text-xs text-gray-400 dark:text-gray-500">
          By continuing, you agree to our terms of service and privacy policy.
        </p>
      </div>
    </div>
  );
}

function ProviderIcon({ provider }: { provider: string }) {
  const iconMap: Record<string, string> = {
    github: "🐙",
    gitlab: "🦊",
    bitbucket: "🪣",
    google: "🔍",
  };
  return (
    <span className="flex h-6 w-6 items-center justify-center text-lg">
      {iconMap[provider.toLowerCase()] ?? "🔑"}
    </span>
  );
}

export default function LoginPage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-screen items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
        </div>
      }
    >
      <LoginContent />
    </Suspense>
  );
}
