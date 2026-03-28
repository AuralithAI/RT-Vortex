// ─── Zustand UI Store ────────────────────────────────────────────────────────
// Client-side ephemeral state: sidebar, modals, toast notifications.
// ─────────────────────────────────────────────────────────────────────────────

import { create } from "zustand";

export interface Toast {
  id: string;
  title: string;
  description?: string;
  variant?: "default" | "success" | "error" | "warning";
}

interface UIState {
  sidebarOpen: boolean;
  toggleSidebar: () => void;
  setSidebarOpen: (open: boolean) => void;

  toasts: Toast[];
  addToast: (toast: Omit<Toast, "id">) => void;
  removeToast: (id: string) => void;

  confirmDialog: {
    open: boolean;
    title: string;
    description: string;
    onConfirm: (() => void) | null;
  };
  showConfirm: (title: string, description: string, onConfirm: () => void) => void;
  hideConfirm: () => void;
}

let toastCounter = 0;

export const useUIStore = create<UIState>((set) => ({
  // ── Sidebar ─────────────────────────────────────────────────────────────
  sidebarOpen: true,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),

  // ── Toasts ──────────────────────────────────────────────────────────────
  toasts: [],
  addToast: (toast) => {
    const id = `toast-${++toastCounter}`;
    set((s) => ({ toasts: [...s.toasts, { ...toast, id }] }));
    // Auto-remove after 5 seconds
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
    }, 5_000);
  },
  removeToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),

  // ── Confirm Dialog ──────────────────────────────────────────────────────
  confirmDialog: {
    open: false,
    title: "",
    description: "",
    onConfirm: null,
  },
  showConfirm: (title, description, onConfirm) =>
    set({ confirmDialog: { open: true, title, description, onConfirm } }),
  hideConfirm: () =>
    set({
      confirmDialog: { open: false, title: "", description: "", onConfirm: null },
    }),
}));
