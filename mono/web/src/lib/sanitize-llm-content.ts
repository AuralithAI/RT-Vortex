// ─── Provider-Aware LLM Response Sanitizer ──────────────────────────────────
// Each LLM provider leaks different artifacts into its text stream.  Rather
// than a single gauntlet of regexes that risk false-positive matches across
// providers, we dispatch to a dedicated cleaner per provider.
//
// Provider string is whatever the backend sends — typically "anthropic",
// "openai", "grok", "gemini".  An unknown or missing provider gets the
// conservative shared cleaner only.
// ─────────────────────────────────────────────────────────────────────────────

/** Known LLM provider identifiers. */
export type LLMProvider = "anthropic" | "grok" | "openai" | "gemini";

/**
 * Production-grade LLM response sanitizer.
 *
 * @param raw      — The raw LLM response text.
 * @param provider — Optional provider key (e.g. "anthropic", "gemini").
 *                   When omitted, only the shared (safe) rules run.
 */
export function sanitizeLLMContent(raw: string, provider?: string): string {
  if (!raw || typeof raw !== "string") return "";

  let cleaned = raw;

  // Normalise provider key so "Anthropic", "GEMINI", etc. all match.
  const key = (provider ?? "").toLowerCase().replace(/[^a-z]/g, "");

  switch (key) {
    case "anthropic":
      cleaned = sanitizeAnthropic(cleaned);
      break;
    case "grok":
      cleaned = sanitizeGrok(cleaned);
      break;
    case "gemini":
    case "google":
      cleaned = sanitizeGemini(cleaned);
      break;
    case "openai":
      cleaned = sanitizeOpenAI(cleaned);
      break;
    default:
      // Unknown / missing provider — apply all provider cleaners so
      // nothing leaks.  This is the fallback for call sites that don't
      // have a provider (e.g. agent chat messages).
      cleaned = sanitizeAnthropic(cleaned);
      cleaned = sanitizeGrok(cleaned);
      cleaned = sanitizeGemini(cleaned);
      break;
  }

  // ── Shared finishing rules (safe for every provider) ──────────────────

  // Generic snake_case XML tags — tool invocation tags use snake_case names
  // (search_code, get_file_content, report_plan, etc.) which are never
  // valid HTML or Markdown.  Requires ≥1 underscore so single-word HTML
  // tags (p, div, span…) are never touched.
  // Paired: <search_code>...</search_code>
  cleaned = cleaned.replace(/<([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*>[\s\S]*?<\/\1\s*>/gi, "");
  // Self-closing: <get_index_status />
  cleaned = cleaned.replace(/<([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*\/?>/gi, "");
  // Orphaned closing: </search_code>
  cleaned = cleaned.replace(/<\/([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*>/gi, "");

  // Known single-word tool-parameter wrapper tags.
  cleaned = cleaned.replace(
    /<\/?(?:query|path|instruction|step_by_step_plan|affected_files|agents_needed)\s*>/gi,
    "",
  );

  // Remove VSCode.Cell tags.
  cleaned = cleaned.replace(/<\/?VSCode\.Cell[^>]*>/g, "");

  // Lines that are just a bare UUID-v4 (repo_id echo).
  cleaned = cleaned.replace(
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?:\s+\S.*)?$/gm,
    "",
  );

  // Orphaned tool-call ID lines (toolu_..., call_..., chatcmpl-...).
  cleaned = cleaned.replace(/^(?:toolu_|call_|chatcmpl-)[A-Za-z0-9_-]+\s*$/gm, "");

  // Strip conversational preamble ("Ok I am the orchestrator…", etc.).
  cleaned = stripLLMPreamble(cleaned);

  // Collapse excessive whitespace left by removals.
  cleaned = cleaned.replace(/\n{3,}/g, "\n\n");

  // Trim.
  cleaned = cleaned.trim();

  return cleaned;
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Anthropic / Claude
//    Claude is the most aggressive leaker — full XML tool-call blocks,
//    <function_calls>, <invoke>, <parameter>, truncated partial tags, and
//    tool-narration lines between invocations.
// ─────────────────────────────────────────────────────────────────────────────
function sanitizeAnthropic(text: string): string {
  let cleaned = text;

  // Paired XML tool blocks.
  cleaned = cleaned.replace(/<function_calls>[\s\S]*?<\/function_calls>/gi, "");
  cleaned = cleaned.replace(/<function_result>[\s\S]*?<\/function_result>/gi, "");
  cleaned = cleaned.replace(/<invoke[\s\S]*?<\/invoke>/gi, "");

  // Truncated / cut-off tags that never got a closing counterpart.
  // These appear at the very end of a response when the model stopped
  // mid-tag (e.g. `<parameter name="end_line`).
  cleaned = cleaned.replace(/<parameter[^>]*$/gm, "");
  cleaned = cleaned.replace(/<invoke[^>]*$/gm, "");
  cleaned = cleaned.replace(/<function_calls[^>]*$/gm, "");
  cleaned = cleaned.replace(/<function_result[^>]*$/gm, "");

  // Any remaining opening/closing tool tags that survived above.
  cleaned = cleaned.replace(
    /<\/?(?:function_calls|invoke|function_result|parameter|antml:invoke|antml:parameter)[^>]*>/gi,
    "",
  );

  // tool_use / tool_result JSON fragments Claude sometimes emits inline.
  // e.g. {"type":"tool_use","id":"toolu_...","name":"search_code",...}
  cleaned = cleaned.replace(/\{"type"\s*:\s*"tool_(?:use|result)"[^}]{0,3000}\}/g, "");

  // Orphaned narration lines between stripped invoke blocks.
  // e.g. "Let me search for…", "Now I'll examine…"
  cleaned = cleaned.replace(
    /^(?:(?:Now )?[Ll]et me (?:search|look|examine|check|find|also|now)|(?:I'll|I will) (?:search|look|examine|check|find|now)|(?:Let me also|Now I'll|Now let me)|(?:Based on (?:my|the|this)|After reviewing|After examining)).*$/gm,
    "",
  );

  return cleaned;
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Grok
//    Grok occasionally leaks JSON tool fragments.
// ─────────────────────────────────────────────────────────────────────────────
function sanitizeGrok(text: string): string {
  let cleaned = text;

  // JSON tool fragments.
  cleaned = cleaned.replace(/\{"type"\s*:\s*"tool_(?:use|result)"[^}]{0,3000}\}/g, "");

  return cleaned;
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Gemini / Google
//    Gemini echoes Python-style tool calls (`print(search_code(…))`) and
//    stray code-fence language markers.
// ─────────────────────────────────────────────────────────────────────────────
function sanitizeGemini(text: string): string {
  let cleaned = text;

  // Python-style tool invocations (with or without wrapping `print()`).
  // Match only within a single line — [^\n]* to avoid cross-line eating.
  cleaned = cleaned.replace(
    /^(?:print\s*\(\s*)?(?:search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index)\s*\([^\n]*\)\s*\)?$/gm,
    "",
  );

  // Code-fence language markers that Gemini echoes for its internal calls.
  cleaned = cleaned.replace(/^```(?:python|json|yaml|xml|go|bash|sh)?\s*$/gm, "");

  return cleaned;
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. OpenAI
//    OpenAI almost never leaks tool markup — cleanest of the bunch.
// ─────────────────────────────────────────────────────────────────────────────
function sanitizeOpenAI(text: string): string {
  // Nothing to strip — OpenAI responses are clean.
  return text;
}

// ─────────────────────────────────────────────────────────────────────────────
// Preamble stripper
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Remove conversational self-introduction lines that some models (Grok, Gemini)
 * prefix their responses with, e.g. "Ok, I am the orchestrator agent…"
 * or "Hello! As the Orchestrator agent, I've analyzed…"
 *
 * Only scans the first few lines — once real content is found, the rest
 * of the text passes through untouched for performance.
 */
function stripLLMPreamble(text: string): string {
  const lines = text.split("\n");
  const result: string[] = [];
  let pastPreamble = false;
  let scanned = 0;

  for (const line of lines) {
    if (!pastPreamble) {
      const lower = line.trim().toLowerCase();
      if (!lower) continue; // skip leading blanks
      // Give up after 5 leading content lines — anything beyond isn't preamble.
      if (++scanned > 5) {
        pastPreamble = true;
        result.push(line);
        continue;
      }
      if (
        /^(ok[,.]?\s+)?(i am|i'm|as)\s+(the\s+)?(an?\s+)?orchestrator/.test(lower) ||
        /^(ok[,.]?\s+)?(let me|i'll|i will)\s+(start|begin|create|produce|analy[sz]e)/.test(lower) ||
        /^(sure|alright|certainly|understood)[,!.]/.test(lower) ||
        /^(hello|hi|hey|greetings)[!,.\s]/.test(lower) ||
        /^as\s+the\s+(orchestrator|team\s+lead)/.test(lower) ||
        /^i('ve|'ve| have)\s+(analy[sz]ed|reviewed|examined|formulated|assessed)/.test(lower)
      ) {
        continue;
      }
      pastPreamble = true;
    }
    result.push(line);
  }
  return result.length > 0 ? result.join("\n") : text;
}
