// useTypewriter — progressively reveals text word-by-word to simulate
// LLM token streaming.  Since our backend delivers complete responses
// (not token-by-token), this hook creates a ChatGPT / Claude-style
// reveal effect purely on the frontend.

import { useState, useEffect, useRef } from "react";

interface UseTypewriterOptions {
  /** Milliseconds between each word reveal (default: 22). Lower = faster. */
  intervalMs?: number;
  /** Skip animation and show text instantly (default: false). */
  instant?: boolean;
}

/**
 * Build the word-boundary index table for `text`.
 *
 * Returns an array of char-offsets where each "word" ends.
 * A "word" is any run of non-whitespace characters **plus** the
 * trailing whitespace that follows it.  This keeps markdown/code intact
 * and avoids trimming spaces between words.
 *
 * Example: `"Hello world\n"` → boundaries at [6, 12]
 */
function buildWordBoundaries(text: string): number[] {
  const boundaries: number[] = [];
  // Match: (non-ws run)(optional ws run)
  const re = /\S+\s*/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    boundaries.push(m.index + m[0].length);
  }
  // If the text starts with whitespace before the first word, fold it
  // into the first boundary so leading newlines aren't swallowed.
  if (boundaries.length > 0 && boundaries[0] > text.search(/\S/)) {
    // already handled — /\S+\s*/ naturally starts at first non-ws
  }
  return boundaries;
}

/**
 * Progressively reveal `fullText` word-by-word to simulate LLM streaming.
 *
 * Returns `{ displayedText, isTyping }`.
 * - `displayedText` — the portion of text revealed so far.
 * - `isTyping` — true while the animation is in progress.
 *
 * Each tick reveals one word (a run of non-whitespace + trailing whitespace).
 * `intervalMs` controls speed — lower = faster.  22ms ≈ 45 words/sec which
 * feels like real token streaming (ChatGPT / Claude style).
 *
 * If `instant` is true, the full text is returned immediately (useful for
 * responses that were already shown, e.g. replayed history).
 */
export function useTypewriter(
  fullText: string,
  options: UseTypewriterOptions = {},
) {
  const {
    intervalMs = 22,
    instant = false,
  } = options;

  const [charIndex, setCharIndex] = useState(0);
  const prevTextRef = useRef(fullText);
  const wordBoundariesRef = useRef<number[]>([]);
  const wordIdxRef = useRef(0);

  // When the full text changes (new response arrived), rebuild boundaries.
  useEffect(() => {
    if (fullText !== prevTextRef.current) {
      // If the new text starts with the old text (response grew),
      // keep current position; otherwise start from scratch.
      if (fullText.startsWith(prevTextRef.current)) {
        // keep current charIndex / wordIdx
      } else {
        setCharIndex(0);
        wordIdxRef.current = 0;
      }
      prevTextRef.current = fullText;
    }
    // Always rebuild boundaries when text changes so new words are tracked.
    wordBoundariesRef.current = buildWordBoundaries(fullText);
  }, [fullText]);

  // ── Animate — advance one word boundary per tick ─────────────────────
  useEffect(() => {
    if (instant || charIndex >= fullText.length) return;

    const boundaries = wordBoundariesRef.current;
    const timer = setInterval(() => {
      // Find the current word-boundary index that matches our charIndex,
      // then advance to the next one.  This avoids the off-by-one that
      // previously skipped the first word after a reset.
      let nextWordIdx = wordIdxRef.current;

      // If we haven't revealed anything yet (charIndex === 0), start at
      // the first boundary (index 0).  Otherwise advance past current.
      if (charIndex === 0 && nextWordIdx === 0) {
        // first tick — reveal the first word
      } else {
        nextWordIdx += 1;
      }

      wordIdxRef.current = nextWordIdx;

      const nextChar =
        nextWordIdx < boundaries.length
          ? boundaries[nextWordIdx]
          : fullText.length;

      setCharIndex(nextChar);
      if (nextChar >= fullText.length) {
        clearInterval(timer);
      }
    }, intervalMs);

    return () => clearInterval(timer);
  }, [fullText, charIndex, intervalMs, instant]);

  // If instant, always show everything.
  if (instant) {
    return { displayedText: fullText, isTyping: false };
  }

  const isTyping = charIndex < fullText.length;
  const displayedText = fullText.slice(0, charIndex);

  return { displayedText, isTyping };
}
