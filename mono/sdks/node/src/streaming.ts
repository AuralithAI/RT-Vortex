/**
 * SSE streaming utilities for the RTVortex SDK.
 */

import type { ProgressEvent } from "./types.js";

/**
 * Parse a single SSE text block into a ProgressEvent.
 */
export function parseSSEBlock(block: string): ProgressEvent | null {
  let eventType = "";
  const dataLines: string[] = [];

  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) {
      eventType = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trim());
    }
  }

  if (dataLines.length === 0) return null;

  const raw = dataLines.join("\n");
  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(raw);
  } catch {
    payload = { message: raw };
  }

  if (eventType) {
    payload.event ??= eventType;
  }

  return payload as unknown as ProgressEvent;
}

/**
 * Async generator that yields ProgressEvents from a ReadableStream (SSE).
 *
 * Works with the native `fetch()` body stream in Node 18+ and browsers.
 */
export async function* iterSSEEvents(
  stream: ReadableStream<Uint8Array>,
): AsyncGenerator<ProgressEvent, void, undefined> {
  const decoder = new TextDecoder();
  const reader = stream.getReader();
  let buf = "";

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });

      while (buf.includes("\n\n")) {
        const idx = buf.indexOf("\n\n");
        const block = buf.slice(0, idx).trim();
        buf = buf.slice(idx + 2);
        if (!block) continue;
        const evt = parseSSEBlock(block);
        if (evt) yield evt;
      }
    }

    // flush remainder
    const rest = buf.trim();
    if (rest) {
      const evt = parseSSEBlock(rest);
      if (evt) yield evt;
    }
  } finally {
    reader.releaseLock();
  }
}
