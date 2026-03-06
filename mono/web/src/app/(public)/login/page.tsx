// ─── Login Page ──────────────────────────────────────────────────────────────
// Shows available OAuth providers. Clicking one redirects to the Go server's
// /api/v1/auth/login/:provider endpoint which handles the OAuth flow.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { useAuthProviders } from "@/lib/api/queries";
import { getApiBaseUrl } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/lib/api/queries";

function LoginContent() {
  const searchParams = useSearchParams();
  const returnTo = searchParams.get("returnTo") ?? "/dashboard";
  const { data: providers, isLoading, error, isFetching } = useAuthProviders();
  const queryClient = useQueryClient();

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
              <p>Failed to load login providers.</p>
              <button
                onClick={() => queryClient.invalidateQueries({ queryKey: queryKeys.providers })}
                disabled={isFetching}
                className="mt-2 inline-flex items-center gap-1 rounded-md bg-red-100 px-3 py-1.5 text-xs font-medium text-red-700 transition-colors hover:bg-red-200 disabled:opacity-50 dark:bg-red-900 dark:text-red-300 dark:hover:bg-red-800"
              >
                {isFetching ? "Retrying…" : "Retry"}
              </button>
            </div>
          )}

          {!isLoading && !error && Array.isArray(providers) && providers.length === 0 && (
            <div className="rounded-lg border border-yellow-200 bg-yellow-50 p-4 text-center text-sm text-yellow-700 dark:border-yellow-900 dark:bg-yellow-950 dark:text-yellow-400">
              <p className="font-medium">No login providers available</p>
              <p className="mt-1 text-xs">
                This may be a temporary issue. Try refreshing, or contact your administrator
                if the problem persists.
              </p>
              <button
                onClick={() => queryClient.invalidateQueries({ queryKey: queryKeys.providers })}
                disabled={isFetching}
                className="mt-2 inline-flex items-center gap-1 rounded-md bg-yellow-100 px-3 py-1.5 text-xs font-medium text-yellow-800 transition-colors hover:bg-yellow-200 disabled:opacity-50 dark:bg-yellow-900 dark:text-yellow-300 dark:hover:bg-yellow-800"
              >
                {isFetching ? "Retrying…" : "Retry"}
              </button>
            </div>
          )}

          {Array.isArray(providers) && providers.map((provider) => (
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
    microsoft: "🪟",
    apple: "🍎",
    x: "𝕏",
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
