/// <reference types="vitest/globals" />
import { useUIStore } from "@/lib/stores/ui";

describe("useUIStore", () => {
  beforeEach(() => {
    // Reset store between tests
    useUIStore.setState({
      sidebarOpen: true,
      toasts: [],
      confirmDialog: {
        open: false,
        title: "",
        description: "",
        onConfirm: null,
      },
    });
  });

  // ── Sidebar ────────────────────────────────────────────────────────────────
  describe("sidebar", () => {
    it("starts with sidebar open", () => {
      expect(useUIStore.getState().sidebarOpen).toBe(true);
    });

    it("toggles sidebar", () => {
      useUIStore.getState().toggleSidebar();
      expect(useUIStore.getState().sidebarOpen).toBe(false);

      useUIStore.getState().toggleSidebar();
      expect(useUIStore.getState().sidebarOpen).toBe(true);
    });

    it("sets sidebar explicitly", () => {
      useUIStore.getState().setSidebarOpen(false);
      expect(useUIStore.getState().sidebarOpen).toBe(false);

      useUIStore.getState().setSidebarOpen(true);
      expect(useUIStore.getState().sidebarOpen).toBe(true);
    });
  });

  // ── Toasts ─────────────────────────────────────────────────────────────────
  describe("toasts", () => {
    it("starts with no toasts", () => {
      expect(useUIStore.getState().toasts).toHaveLength(0);
    });

    it("adds a toast", () => {
      useUIStore.getState().addToast({
        variant: "success",
        title: "Test",
        description: "Toast message",
      });

      const toasts = useUIStore.getState().toasts;
      expect(toasts).toHaveLength(1);
      expect(toasts[0].title).toBe("Test");
      expect(toasts[0].variant).toBe("success");
    });

    it("removes a toast by id", () => {
      useUIStore.getState().addToast({
        title: "Removable",
      });

      const toasts = useUIStore.getState().toasts;
      expect(toasts).toHaveLength(1);
      const id = toasts[0].id;

      useUIStore.getState().removeToast(id);
      expect(useUIStore.getState().toasts).toHaveLength(0);
    });

    it("can add multiple toasts", () => {
      useUIStore.getState().addToast({ variant: "success", title: "First" });
      useUIStore.getState().addToast({ variant: "error", title: "Second" });
      useUIStore.getState().addToast({ title: "Third" });

      expect(useUIStore.getState().toasts).toHaveLength(3);
    });
  });

  // ── Confirm Dialog ────────────────────────────────────────────────────────
  describe("confirm dialog", () => {
    it("starts closed", () => {
      expect(useUIStore.getState().confirmDialog.open).toBe(false);
    });

    it("opens confirm dialog", () => {
      const onConfirm = vi.fn();
      useUIStore.getState().showConfirm(
        "Delete item?",
        "This cannot be undone",
        onConfirm,
      );

      const dialog = useUIStore.getState().confirmDialog;
      expect(dialog.open).toBe(true);
      expect(dialog.title).toBe("Delete item?");
      expect(dialog.description).toBe("This cannot be undone");
    });

    it("closes confirm dialog", () => {
      useUIStore.getState().showConfirm(
        "Test",
        "Test",
        vi.fn(),
      );

      expect(useUIStore.getState().confirmDialog.open).toBe(true);

      useUIStore.getState().hideConfirm();
      expect(useUIStore.getState().confirmDialog.open).toBe(false);
    });
  });
});
