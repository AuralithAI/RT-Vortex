import { describe, it, expect } from "vitest";
import {
  RTVortexError,
  AuthenticationError,
  NotFoundError,
  ValidationError,
  QuotaExceededError,
  ServerError,
  throwForStatus,
} from "../src/errors.js";

describe("Error classes", () => {
  it("RTVortexError stores status and body", () => {
    const err = new RTVortexError("fail", 418, { detail: "teapot" });
    expect(err.message).toBe("fail");
    expect(err.statusCode).toBe(418);
    expect(err.body).toEqual({ detail: "teapot" });
    expect(err.name).toBe("RTVortexError");
  });

  it("AuthenticationError is instance of base", () => {
    const err = new AuthenticationError("no auth");
    expect(err).toBeInstanceOf(RTVortexError);
    expect(err.statusCode).toBe(401);
    expect(err.name).toBe("AuthenticationError");
  });

  it("NotFoundError has correct code", () => {
    const err = new NotFoundError("missing");
    expect(err.statusCode).toBe(404);
  });

  it("ValidationError has correct code", () => {
    const err = new ValidationError("invalid");
    expect(err.statusCode).toBe(422);
  });

  it("QuotaExceededError accepts status", () => {
    const err = new QuotaExceededError("too many", 429);
    expect(err.statusCode).toBe(429);
  });

  it("ServerError accepts status", () => {
    const err = new ServerError("boom", 503);
    expect(err.statusCode).toBe(503);
  });
});

describe("throwForStatus", () => {
  function makeResponse(status: number, body: unknown): Response {
    return new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  }

  it("does not throw on 200", async () => {
    await expect(
      throwForStatus(makeResponse(200, { ok: true })),
    ).resolves.toBeUndefined();
  });

  it("throws AuthenticationError on 401", async () => {
    await expect(
      throwForStatus(makeResponse(401, { error: "unauthorized" })),
    ).rejects.toThrow(AuthenticationError);
  });

  it("throws NotFoundError on 404", async () => {
    await expect(
      throwForStatus(makeResponse(404, { error: "not found" })),
    ).rejects.toThrow(NotFoundError);
  });

  it("throws ValidationError on 422", async () => {
    await expect(
      throwForStatus(makeResponse(422, { error: "bad input" })),
    ).rejects.toThrow(ValidationError);
  });

  it("throws QuotaExceededError on 429", async () => {
    await expect(
      throwForStatus(makeResponse(429, { error: "rate limited" })),
    ).rejects.toThrow(QuotaExceededError);
  });

  it("throws ServerError on 500", async () => {
    await expect(
      throwForStatus(makeResponse(500, { error: "internal" })),
    ).rejects.toThrow(ServerError);
  });

  it("throws RTVortexError on unknown status", async () => {
    await expect(
      throwForStatus(makeResponse(418, { error: "teapot" })),
    ).rejects.toThrow(RTVortexError);
  });
});
