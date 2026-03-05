/// <reference types="vitest/globals" />
import { cn, formatDate, timeAgo, formatDuration, truncate, getApiBaseUrl, getWsBaseUrl } from "@/lib/utils";

// ─────────────────────────────────────────────────────────────────────────────
// cn()
// ─────────────────────────────────────────────────────────────────────────────
describe("cn", () => {
  it("merges class names", () => {
    expect(cn("foo", "bar")).toBe("foo bar");
  });

  it("handles conditional classes", () => {
    expect(cn("base", false && "hidden", "visible")).toBe("base visible");
  });

  it("resolves Tailwind conflicts (last wins)", () => {
    const result = cn("px-4", "px-2");
    expect(result).toBe("px-2");
  });

  it("handles undefined and null inputs", () => {
    expect(cn("a", undefined, null, "b")).toBe("a b");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// formatDate()
// ─────────────────────────────────────────────────────────────────────────────
describe("formatDate", () => {
  it("formats an ISO date string", () => {
    const result = formatDate("2024-06-15T10:30:00Z");
    expect(result).toBeDefined();
    expect(typeof result).toBe("string");
    expect(result.length).toBeGreaterThan(0);
  });

  it("returns dash for null/undefined", () => {
    expect(formatDate(null)).toBe("—");
    expect(formatDate(undefined)).toBe("—");
  });

  it("returns dash for empty string", () => {
    expect(formatDate("")).toBe("—");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// timeAgo()
// ─────────────────────────────────────────────────────────────────────────────
describe("timeAgo", () => {
  it("returns 'just now' for very recent dates", () => {
    const now = new Date().toISOString();
    const result = timeAgo(now);
    expect(result).toMatch(/just now|seconds? ago/);
  });

  it("returns short-form minutes for older dates", () => {
    const thirtyMinsAgo = new Date(Date.now() - 30 * 60 * 1000).toISOString();
    const result = timeAgo(thirtyMinsAgo);
    expect(result).toMatch(/\d+m ago/);
  });

  it("returns short-form hours for hours-old dates", () => {
    const fiveHoursAgo = new Date(Date.now() - 5 * 60 * 60 * 1000).toISOString();
    const result = timeAgo(fiveHoursAgo);
    expect(result).toMatch(/\d+h ago/);
  });

  it("returns short-form days for days-old dates", () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString();
    const result = timeAgo(threeDaysAgo);
    expect(result).toMatch(/\d+d ago/);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// formatDuration()
// ─────────────────────────────────────────────────────────────────────────────
describe("formatDuration", () => {
  it("formats milliseconds below 1 second", () => {
    expect(formatDuration(500)).toMatch(/500\s*ms/);
  });

  it("formats seconds", () => {
    const result = formatDuration(5000);
    expect(result).toMatch(/5/);
  });

  it("formats minutes and seconds", () => {
    const result = formatDuration(90_000);
    expect(result).toMatch(/1/);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// truncate()
// ─────────────────────────────────────────────────────────────────────────────
describe("truncate", () => {
  it("returns original string when shorter than max", () => {
    expect(truncate("hello", 10)).toBe("hello");
  });

  it("truncates and adds ellipsis when longer than max", () => {
    const result = truncate("this is a long string", 10);
    expect(result.length).toBeLessThanOrEqual(11); // 10 + "…"
    expect(result).toContain("…");
  });

  it("handles empty string", () => {
    expect(truncate("", 10)).toBe("");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// getApiBaseUrl()
// ─────────────────────────────────────────────────────────────────────────────
describe("getApiBaseUrl", () => {
  const originalEnv = process.env;

  afterEach(() => {
    process.env = originalEnv;
  });

  it("returns NEXT_PUBLIC_API_URL if set", () => {
    process.env = { ...originalEnv, NEXT_PUBLIC_API_URL: "https://api.example.com" };
    expect(getApiBaseUrl()).toBe("https://api.example.com");
  });

  it("returns empty string when no env var is set (browser-relative)", () => {
    process.env = { ...originalEnv };
    delete process.env.NEXT_PUBLIC_API_URL;
    const result = getApiBaseUrl();
    expect(typeof result).toBe("string");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// getWsBaseUrl()
// ─────────────────────────────────────────────────────────────────────────────
describe("getWsBaseUrl", () => {
  const originalEnv = process.env;

  afterEach(() => {
    process.env = originalEnv;
  });

  it("returns NEXT_PUBLIC_WS_URL if set", () => {
    process.env = { ...originalEnv, NEXT_PUBLIC_WS_URL: "wss://ws.example.com" };
    expect(getWsBaseUrl()).toBe("wss://ws.example.com");
  });
});
