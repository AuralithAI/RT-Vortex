// ─── Auth Context ────────────────────────────────────────────────────────────
// Wraps useMe() query into a context for easy consumption across the app.
// Provides user info, loading state, and isAuthenticated flag.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  createContext,
  useContext,
  type ReactNode,
} from "react";
import { useMe } from "@/lib/api/queries";
import { useLogout } from "@/lib/api/mutations";
import type { User } from "@/types/api";

interface AuthContextValue {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  error: Error | null;
  logout: () => void;
  isLoggingOut: boolean;
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  isLoading: true,
  isAuthenticated: false,
  error: null,
  logout: () => {},
  isLoggingOut: false,
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const { data: user, isLoading, error } = useMe();
  const logoutMutation = useLogout();

  const value: AuthContextValue = {
    user: user ?? null,
    isLoading,
    isAuthenticated: !!user && !error,
    error: error as Error | null,
    logout: () => logoutMutation.mutate(),
    isLoggingOut: logoutMutation.isPending,
  };

  return <AuthContext value={value}>{children}</AuthContext>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (ctx === undefined) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}
