// ─── Confirm Dialog ──────────────────────────────────────────────────────────
// Global confirmation dialog driven by the Zustand UI store.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useUIStore } from "@/lib/stores/ui";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

export function ConfirmDialog() {
  const { confirmDialog, hideConfirm } = useUIStore();

  const handleConfirm = () => {
    confirmDialog.onConfirm?.();
    hideConfirm();
  };

  return (
    <Dialog open={confirmDialog.open} onOpenChange={(open) => !open && hideConfirm()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{confirmDialog.title}</DialogTitle>
          <DialogDescription>{confirmDialog.description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={hideConfirm}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleConfirm}>
            Confirm
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
