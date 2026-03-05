// ─── OAuth Callback Page ─────────────────────────────────────────────────────
// The Go server redirects here with ?token=<jwt> after OAuth. We store the
// token in an httpOnly cookie via the Next.js API route, then redirect to
// the dashboard (or the original returnTo path).
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useSearchParams, useRouter } from "next/navigation";
import { useEffect, useState, Suspense } from "react";

function CallbackContent() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const token = searchParams.get("token");
    const returnTo = searchParams.get("returnTo") ?? "/dashboard";
    const errParam = searchParams.get("error");

    if (errParam) {
      setError(errParam);
      return;
    }

    if (!token) {
      setError("No authentication token received");
      return;
    }

    // Store token in httpOnly cookie via API route
    fetch("/api/auth/set-token", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    })
      .then((res) => {
        if (!res.ok) throw new Error("Failed to set auth token");
        router.replace(returnTo);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Authentication failed");
      });
  }, [searchParams, router]);

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="max-w-md space-y-4 rounded-xl border border-red-200 bg-white p-8 text-center shadow-lg dark:border-red-900 dark:bg-gray-950">
          <div className="text-4xl">⚠️</div>
          <h1 className="text-xl font-bold text-red-600 dark:text-red-400">
            Authentication Failed
          </h1>
          <p className="text-sm text-gray-600 dark:text-gray-400">{error}</p>
          <button
            onClick={() => router.replace("/login")}
            className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
          >
            Back to Login
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="space-y-4 text-center">
        <div className="mx-auto h-10 w-10 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
        <p className="text-sm text-gray-500 dark:text-gray-400">
          Completing authentication…
        </p>
      </div>
    </div>
  );
}

export default function CallbackPage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-screen items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
        </div>
      }
    >
      <CallbackContent />
    </Suspense>
  );
}
