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

  // 5b. Remove ANY XML tag that looks like a tool/function invocation.
  //     LLMs (Claude, Gemini, Grok) echo tool calls as XML in their text
  //     stream.  Tool tags always use snake_case names (search_code,
  //     get_file_content, report_plan, etc.) which are never valid HTML or
  //     Markdown.  This generic approach catches all current and future tool
  //     names without maintaining an explicit list.
  //
  //     Pattern: <word_word> ... </word_word>  (snake_case, paired)
  //              <word_word> ... $              (unclosed / trailing)
  //              <word_word />                  (self-closing)
  //              </word_word>                   (orphaned closing tag)
  //
  //     Excludes single-word tags that could be HTML (p, div, span, etc.)
  //     by requiring at least one underscore in the tag name.
  // Paired: <search_code>...</search_code>
  cleaned = cleaned.replace(/<([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*>[\s\S]*?<\/\1\s*>/gi, "");
  // Unclosed / trailing
  cleaned = cleaned.replace(/<([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*>[\s\S]*$/gi, "");
  // Self-closing: <get_index_status />
  cleaned = cleaned.replace(/<([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*\/?>/gi, "");
  // Orphaned closing: </search_code>
  cleaned = cleaned.replace(/<\/([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*>/gi, "");

  // Also strip single-word XML tags that are known tool parameter wrappers
  // (query, path, instruction, etc.) — these are NOT valid HTML either.
  // Use a small explicit set here since single-word tags overlap with HTML.
  cleaned = cleaned.replace(
    /<\/?(?:query|path|instruction|step_by_step_plan|affected_files|agents_needed)\s*>/gi,
    "",
  );

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

  // 10b. Remove Gemini-style Python tool invocations that appear as text.
  //      e.g. `print(search_code("some query"))`, `print(get_file_content("path"))`,
  //      `print(get_index_status())`, `print(report_plan({...}))`.
  cleaned = cleaned.replace(
    /^print\(\s*(?:search_code|get_file_content|get_index_status|report_plan|get_index|submit_plan|create_plan)\s*\([\s\S]*?\)\s*\)\s*$/gm,
    "",
  );
  // Also bare calls without print()
  cleaned = cleaned.replace(
    /^(?:search_code|get_file_content|get_index_status|report_plan|get_index|submit_plan|create_plan)\s*\([\s\S]*?\)\s*$/gm,
    "",
  );

  // 11. Remove lines that are only tool invocation narration from Claude
  //     e.g. "Now let me search for..." or "Let me look for..." followed by
  //     nothing useful (often the only text between stripped invoke blocks).
  //     Now that tool-call XML is stripped, these orphaned narration lines
  //     are just noise.  Remove lines that are *only* tool-narration.
  cleaned = cleaned.replace(
    /^(?:(?:Now )?[Ll]et me (?:search|look|examine|check|find|also|now)|(?:I'll|I will) (?:search|look|examine|check|find|now)|(?:Let me also|Now I'll|Now let me)|(?:Based on (?:my|the|this)|After reviewing|After examining)).*$/gm,
    "",
  );
  // Remove standalone lines that are just "python" or "```python" (Gemini
  // echoes code-fence language markers for its internal tool calls).
  cleaned = cleaned.replace(/^```(?:python|json|go|bash|sh)?\s*$/gm, "");

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
