// useTypewriter — progressively reveals text to simulate LLM streaming.
// Since our backend delivers complete responses (not token-by-token), this
// hook creates a ChatGPT-like reveal effect on the frontend.

import { useState, useEffect, useRef } from "react";

interface UseTypewriterOptions {
  /** Characters to reveal per tick (default: 8). */
  charsPerTick?: number;
  /** Milliseconds between ticks (default: 12). */
  intervalMs?: number;
  /** Skip animation and show text instantly (default: false). */
  instant?: boolean;
}

/**
 * Progressively reveal `fullText` one chunk at a time.
 *
 * Returns `{ displayedText, isTyping }`.
 * - `displayedText` — the portion of text revealed so far.
 * - `isTyping` — true while the animation is in progress.
 *
 * If `instant` is true, the full text is returned immediately (useful for
 * responses that were already shown, e.g. replayed history).
 */
export function useTypewriter(
  fullText: string,
  options: UseTypewriterOptions = {},
) {
  const { charsPerTick = 8, intervalMs = 12, instant = false } = options;

  const [charIndex, setCharIndex] = useState(0);
  const prevTextRef = useRef(fullText);

  // When the full text changes (new response arrived), reset.
  useEffect(() => {
    if (fullText !== prevTextRef.current) {
      // If the new text starts with the old text (i.e. the response grew),
      // continue from where we were. Otherwise start from scratch.
      if (fullText.startsWith(prevTextRef.current)) {
        // keep current charIndex
      } else {
        setCharIndex(0);
      }
      prevTextRef.current = fullText;
    }
  }, [fullText]);

  // Animate the reveal.
  useEffect(() => {
    if (instant || charIndex >= fullText.length) return;

    const timer = setInterval(() => {
      setCharIndex((prev) => {
        const next = Math.min(prev + charsPerTick, fullText.length);
        if (next >= fullText.length) {
          clearInterval(timer);
        }
        return next;
      });
    }, intervalMs);

    return () => clearInterval(timer);
  }, [fullText, charIndex, charsPerTick, intervalMs, instant]);

  // If instant, always show everything.
  if (instant) {
    return { displayedText: fullText, isTyping: false };
  }

  const isTyping = charIndex < fullText.length;
  const displayedText = fullText.slice(0, charIndex);

  return { displayedText, isTyping };
}
