/// <reference types="vitest/globals" />
import { ApiError, AuthError } from "@/lib/api/client";

// ─────────────────────────────────────────────────────────────────────────────
// ApiError
// ─────────────────────────────────────────────────────────────────────────────
describe("ApiError", () => {
  it("creates an error with status, statusText, and body", () => {
    const err = new ApiError(404, "Not Found", { detail: "Resource missing" });
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(404);
    expect(err.statusText).toBe("Not Found");
    expect(err.body).toEqual({ detail: "Resource missing" });
    expect(err.name).toBe("ApiError");
    expect(err.message).toBe("404 Not Found");
  });

  it("handles empty body", () => {
    const err = new ApiError(500, "Internal Server Error", null);
    expect(err.status).toBe(500);
    expect(err.body).toBeNull();
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// AuthError
// ─────────────────────────────────────────────────────────────────────────────
describe("AuthError", () => {
  it("creates a 401 auth error", () => {
    const err = new AuthError({ message: "Session expired" });
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toBeInstanceOf(AuthError);
    expect(err.status).toBe(401);
    expect(err.name).toBe("AuthError");
  });
});
