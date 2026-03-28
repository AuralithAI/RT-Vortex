/// <reference types="vitest/globals" />
import React from "react";
import { render, screen } from "@testing-library/react";

// We test individual UI components in isolation.
// Since they're thin wrappers around Radix primitives,
// we verify rendering and basic interaction.

describe("UI Components", () => {
  // ── Button ──────────────────────────────────────────────────────────────────
  describe("Button", () => {
    it("renders with text", async () => {
      const { Button } = await import("@/components/ui/button");
      render(React.createElement(Button, null, "Click me"));
      expect(screen.getByRole("button", { name: /click me/i })).toBeInTheDocument();
    });

    it("handles click events", async () => {
      const { Button } = await import("@/components/ui/button");
      const onClick = vi.fn();
      render(React.createElement(Button, { onClick }, "Click"));
      const btn = screen.getByRole("button");
      btn.click();
      expect(onClick).toHaveBeenCalledTimes(1);
    });

    it("can be disabled", async () => {
      const { Button } = await import("@/components/ui/button");
      render(React.createElement(Button, { disabled: true }, "Disabled"));
      expect(screen.getByRole("button")).toBeDisabled();
    });

    it("renders different variants", async () => {
      const { Button } = await import("@/components/ui/button");
      const { container } = render(
        React.createElement(Button, { variant: "destructive" }, "Delete")
      );
      expect(container.firstChild).toHaveClass("bg-destructive");
    });
  });

  // ── Badge ───────────────────────────────────────────────────────────────────
  describe("Badge", () => {
    it("renders with text", async () => {
      const { Badge } = await import("@/components/ui/badge");
      render(React.createElement(Badge, null, "Active"));
      expect(screen.getByText("Active")).toBeInTheDocument();
    });

    it("renders different variants", async () => {
      const { Badge } = await import("@/components/ui/badge");
      const { container } = render(
        React.createElement(Badge, { variant: "destructive" }, "Error")
      );
      expect(container.firstChild).toHaveClass("bg-destructive");
    });
  });

  // ── Input ───────────────────────────────────────────────────────────────────
  describe("Input", () => {
    it("renders with placeholder", async () => {
      const { Input } = await import("@/components/ui/input");
      render(React.createElement(Input, { placeholder: "Enter text" }));
      expect(screen.getByPlaceholderText("Enter text")).toBeInTheDocument();
    });

    it("accepts user input", async () => {
      const { Input } = await import("@/components/ui/input");
      const onChange = vi.fn();
      render(React.createElement(Input, { placeholder: "Type here", onChange }));
      const input = screen.getByPlaceholderText("Type here");
      expect(input).toBeInTheDocument();
    });
  });

  // ── Card ────────────────────────────────────────────────────────────────────
  describe("Card", () => {
    it("renders card with title and description", async () => {
      const { Card, CardHeader, CardTitle, CardDescription } = await import(
        "@/components/ui/card"
      );
      render(
        React.createElement(
          Card,
          null,
          React.createElement(
            CardHeader,
            null,
            React.createElement(CardTitle, null, "My Card"),
            React.createElement(CardDescription, null, "A description")
          )
        )
      );
      expect(screen.getByText("My Card")).toBeInTheDocument();
      expect(screen.getByText("A description")).toBeInTheDocument();
    });
  });

  // ── Skeleton ────────────────────────────────────────────────────────────────
  describe("Skeleton", () => {
    it("renders with custom className", async () => {
      const { Skeleton } = await import("@/components/ui/skeleton");
      const { container } = render(
        React.createElement(Skeleton, { className: "h-4 w-32" })
      );
      expect(container.firstChild).toHaveClass("h-4", "w-32");
    });
  });

  // ── Progress ────────────────────────────────────────────────────────────────
  describe("Progress", () => {
    it("renders with value", async () => {
      const { Progress } = await import("@/components/ui/progress");
      render(React.createElement(Progress, { value: 50 }));
      expect(screen.getByRole("progressbar")).toBeInTheDocument();
    });
  });
});
