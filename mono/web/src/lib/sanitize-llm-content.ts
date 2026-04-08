// Strip internal LLM tool-use markup from provider responses before rendering.
// Models like Claude emit XML-based function_calls / invoke blocks that
// should never be shown to the end user.  Claude also tends to echo tool-call
// arguments as plain text (e.g. repo UUIDs followed by search queries).

/**
 * Remove XML-based tool-call blocks and other LLM-internal markup from a
 * response string so only the human-readable prose remains.
 */
export function sanitizeLLMContent(raw: string): string {
  if (!raw || typeof raw !== "string") return "";

  let cleaned = raw;

  // 1. Remove paired function_calls blocks (Claude-style), including nested
  //    function_result sub-blocks that Claude echoes back.
  cleaned = cleaned.replace(/<function_calls>[\s\S]*?<\/function_calls>/g, "");

  // 2. Remove unclosed / trailing function_calls (truncated responses).
  cleaned = cleaned.replace(/<function_calls>[\s\S]*$/g, "");

  // 3. Remove <function_result>...</function_result> blocks (Claude echoes
  //    search results, file contents, etc. back into its text stream).
  cleaned = cleaned.replace(/<function_result>[\s\S]*?<\/function_result>/g, "");
  // Unclosed / trailing function_result.
  cleaned = cleaned.replace(/<function_result>[\s\S]*$/g, "");

  // 4. Remove individual invoke / antml:invoke tags that might be outside blocks.
  cleaned = cleaned.replace(/<\/?invoke[^>]*>/g, "");
  cleaned = cleaned.replace(/<\/?antml:invoke[^>]*>/g, "");
  cleaned = cleaned.replace(/<\/?antml:parameter[^>]*>[\s\S]*?(?:<\/antml:parameter>|$)/g, "");

  // 5. Remove workspace_ tool calls that appear as plain text.
  //    e.g. "<invoke name="workspace_search">" or "<invoke name="workspace_list_dir">"
  cleaned = cleaned.replace(/<invoke\s+name="workspace_[^"]*"[^>]*>[\s\S]*?<\/invoke>/g, "");

  // 6. Remove parameter tags (including those with content between them).
  cleaned = cleaned.replace(/<parameter[^>]*>[\s\S]*?<\/parameter>/g, "");
  cleaned = cleaned.replace(/<\/?parameter[^>]*>/g, "");

  // 7. Remove VSCode.Cell tags.
  cleaned = cleaned.replace(/<\/?VSCode\.Cell[^>]*>/g, "");

  // 8. Remove lines that are just a UUID (repo_id) optionally followed by a
  //    search query — Claude echoes tool-call arguments as plain text.
  //    Pattern: lines containing a bare UUID-v4 possibly followed by words.
  cleaned = cleaned.replace(
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?:\s+\S.*)?$/gm,
    "",
  );

  // 9. Remove orphaned tool-call ID lines (toolu_..., call_..., chatcmpl-...).
  cleaned = cleaned.replace(/^(?:toolu_|call_|chatcmpl-)[A-Za-z0-9_-]+\s*$/gm, "");

  // 10. Remove tool_use / tool_result JSON-like fragments Claude sometimes
  //     emits as text (e.g. {"type":"tool_use","id":"toolu_...",...}).
  cleaned = cleaned.replace(/\{"type"\s*:\s*"tool_(?:use|result)"[\s\S]*?\}\s*/g, "");

  // 11. Remove lines that are only tool invocation narration from Claude
  //     e.g. "Now let me search for..." or "Let me look for..." followed by
  //     nothing useful (often the only text between stripped invoke blocks).
  //     Only remove when the cleaned result would otherwise be mostly this.
  //     — skip this aggressive rule; the narration may be useful to users.

  // 12. Strip conversational preamble (Grok "Ok I am the orchestrator…",
  //     Gemini "Hello! As the Orchestrator agent…", etc.).
  cleaned = stripLLMPreamble(cleaned);

  // 13. Collapse excessive whitespace left by removals.
  cleaned = cleaned.replace(/\n{3,}/g, "\n\n");

  // 14. Trim leading/trailing whitespace.
  cleaned = cleaned.trim();

  return cleaned;
}

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
