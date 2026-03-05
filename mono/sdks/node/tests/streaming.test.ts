import { describe, it, expect } from "vitest";
import { parseSSEBlock } from "../src/streaming.js";

describe("parseSSEBlock", () => {
  it("parses a basic event", () => {
    const block = 'event: progress\ndata: {"step":"parsing","status":"running"}';
    const evt = parseSSEBlock(block);
    expect(evt).not.toBeNull();
    expect(evt!.event).toBe("progress");
    expect(evt!.step).toBe("parsing");
    expect(evt!.status).toBe("running");
  });

  it("parses data-only block with default event", () => {
    const block = 'data: {"message":"hello"}';
    const evt = parseSSEBlock(block);
    expect(evt).not.toBeNull();
    expect(evt!.message).toBe("hello");
  });

  it("returns null for event-only block", () => {
    expect(parseSSEBlock("event: heartbeat")).toBeNull();
  });

  it("handles non-JSON data", () => {
    const block = "data: plain text";
    const evt = parseSSEBlock(block);
    expect(evt).not.toBeNull();
    expect(evt!.message).toBe("plain text");
  });

  it("returns null for empty block", () => {
    expect(parseSSEBlock("")).toBeNull();
  });

  it("handles complete event", () => {
    const block = 'event: complete\ndata: {"step":"done","status":"completed"}';
    const evt = parseSSEBlock(block);
    expect(evt!.event).toBe("complete");
  });
});
