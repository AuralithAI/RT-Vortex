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

  // ── Namespaced XML tool blocks (any provider) ─────────────────────────
  // LLMs sometimes produce <prefix:function_calls>, <prefix:invoke>, etc.
  // with arbitrary namespace prefixes (anythingllm:, anthropic:, etc.).
  // These are never valid user-facing content.
  cleaned = cleaned.replace(
    /<[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\b[\s\S]*?<\/[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\s*>/gi,
    "",
  );
  // Unclosed namespaced blocks (truncated at max_tokens).
  cleaned = cleaned.replace(
    /<[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\b(?:(?!<\/[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter))[\s\S])*$/gi,
    "",
  );
  // Orphaned namespaced tags.
  cleaned = cleaned.replace(
    /<\/?[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)[^>]*>/gi,
    "",
  );

  // Non-namespaced tool blocks.
  cleaned = cleaned.replace(/<function_calls>[\s\S]*?<\/function_calls>/gi, "");
  cleaned = cleaned.replace(/<function_result>[\s\S]*?<\/function_result>/gi, "");
  cleaned = cleaned.replace(/<invoke[\s\S]*?<\/invoke>/gi, "");
  // Unclosed non-namespaced blocks.
  cleaned = cleaned.replace(
    /<(?:function_calls|invoke|function_result)\b(?:(?!<\/(?:function_calls|invoke|function_result))[\s\S])*$/gi,
    "",
  );
  // Orphaned non-namespaced tags.
  cleaned = cleaned.replace(
    /<\/?(?:function_calls|invoke|function_result|parameter)\b[^>]*>/gi,
    "",
  );

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
    /<\/?(?:query|path|instruction|step_by_step_plan|affected_files|agents_needed|result|summary|complexity|steps|details|step_id|description|target_files|expected_outcome|repo_id|file_path|start_line|end_line)\s*>/gi,
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

  // Safety net: if after all sanitization the output is still excessively
  // large, it's almost certainly unsanitized tool debris.  Truncate to
  // prevent the markdown renderer from hanging the browser.
  // 30k chars ≈ 8-10k tokens — generous enough for long-form analysis
  // while still protecting against runaway tool debris.
  const MAX_SANITIZED_LENGTH = 30000;
  if (cleaned.length > MAX_SANITIZED_LENGTH) {
    // Check if the remaining content is mostly XML/tool debris by looking
    // for a high density of angle brackets — normal prose has very few.
    const angleBrackets = (cleaned.match(/[<>]/g) || []).length;
    const ratio = angleBrackets / cleaned.length;
    if (ratio > 0.02) {
      // High XML density — this is tool debris, not useful content.
      // Try to salvage the first meaningful paragraph.
      const firstParagraph = cleaned.split(/\n{2,}/)[0] || "";
      if (firstParagraph.length > 20 && firstParagraph.length < 2000) {
        cleaned = firstParagraph + "\n\n*(Response contained tool-call data that was filtered out.)*";
      } else {
        cleaned = "*(Response contained only tool-call data — no analysis produced. The model may have hit its output limit.)*";
      }
    } else {
      // Not XML-heavy — just long prose. Truncate cleanly.
      cleaned = cleaned.slice(0, MAX_SANITIZED_LENGTH) + "\n\n*(Response truncated for display.)*";
    }
  }

  return cleaned;
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper: extract code blocks from a Claude <function_result> body
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Given the raw inner text of a `<function_result>…</function_result>` block,
 * pull out fenced code blocks and their surrounding file-path headers.
 *
 * Claude's search_code and get_file_content tools echo source files inside
 * numbered results like:
 *
 *   1. **path/to/file.py** (lines 1-30)
 *   ```python
 *   import foo
 *   …
 *   ```
 *
 * We want to keep these code blocks (they show relevant source from the PR)
 * but strip the tool-result boilerplate ("Here are the search results:", etc.)
 * and the numbered-result wrappers.
 *
 * If there are no code blocks at all (e.g. "The repository is indexed and
 * ready for search."), return empty string — that's just status text.
 */
function extractCodeFromToolResult(inner: string): string {
  const trimmed = inner.trim();

  // Skip pure status / narration results (no code content).
  if (!trimmed || /^(the repository is|no results|error:|not found)/i.test(trimmed)) {
    return "";
  }

  // Skip search result summaries — "Found N matches in M files:" followed
  // by file listings. These are tool metadata, not analysis.
  if (/^Found \d+ match(?:es)? in \d+ file/i.test(trimmed)) {
    return "";
  }

  // Skip "Repository is indexed and ready" status lines.
  if (/^Repository is indexed/i.test(trimmed)) {
    return "";
  }

  // Skip search_code / tool result JSON arrays — these are metadata
  // (file_path, score, line_number lists), not actual source code.
  // Detect: starts with [ and looks like an array of objects with "file_path"/"score".
  if (/^\s*\[/.test(trimmed) && /"(?:file_path|score|line_number)"/.test(trimmed)) {
    return "";
  }

  // Skip "The file at X contains:" narration blocks — these just echo
  // file content from get_file_content calls. The code inside them is
  // from the repo under review, not the LLM's analysis.
  if (/^the file at\s/i.test(trimmed)) {
    return "";
  }

  // ── 1) Fenced code blocks with file-path headers ────────────────────
  // Pattern: optional "N. " prefix + **path** (optional line range) + code fence.
  const blocks: string[] = [];
  const blockPattern =
    /(?:^\d+\.\s*)?\*\*([^*]+)\*\*(?:\s*\(lines?\s*[\d–-]+(?:\s*to\s*\d+)?\))?[\s\S]*?```(\w*)\n([\s\S]*?)```/gm;

  let match: RegExpExecArray | null;
  while ((match = blockPattern.exec(inner)) !== null) {
    const filePath = match[1].trim();
    const lang = match[2] || "";
    const code = match[3];
    if (code && code.trim().length > 0) {
      blocks.push(`**\`${filePath}\`**\n\`\`\`${lang}\n${code.trimEnd()}\n\`\`\``);
    }
  }

  if (blocks.length > 0) {
    return "\n\n" + blocks.join("\n\n") + "\n\n";
  }

  // ── 2) Standalone code fences (no file-path header) ─────────────────
  const standaloneFences: string[] = [];
  const fencePattern = /```(\w*)\n([\s\S]*?)```/gm;
  while ((match = fencePattern.exec(inner)) !== null) {
    const lang = match[1] || "";
    const code = match[2];
    if (code && code.trim().length > 0) {
      standaloneFences.push(`\`\`\`${lang}\n${code.trimEnd()}\n\`\`\``);
    }
  }

  if (standaloneFences.length > 0) {
    return "\n\n" + standaloneFences.join("\n\n") + "\n\n";
  }

  // ── 3) Raw unfenced code (from get_file_content) ────────────────────
  // Detect if the text looks like source code: starts with imports,
  // comments, class/function definitions, or typical code patterns.
  // Only wrap if it has enough lines and code-like signals.
  const lines = trimmed.split("\n");
  if (lines.length >= 3) {
    const codeSignals = lines.filter((l) =>
      /^(?:import |from |#|\/\/|\/\*|\*|package |class |def |func |fn |pub |use |const |let |var |export |module |require|self\.|    )/.test(
        l,
      ),
    ).length;
    const codeRatio = codeSignals / lines.length;

    if (codeRatio >= 0.25) {
      // Guess the language from common patterns.
      const lang = /^(?:import |from |class .*:|def |self\.)/.test(lines[0] || lines[1] || "")
        ? "python"
        : /^(?:package |func |import \()/.test(lines[0] || "")
          ? "go"
          : /^(?:import |export |const |let |var |\/\/)/.test(lines[0] || "")
            ? "typescript"
            : "";
      return `\n\n\`\`\`${lang}\n${trimmed}\n\`\`\`\n\n`;
    }
  }

  // Not code — likely a narration line ("Let me look at…"). Drop it.
  return "";
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper: extract report_plan / submit_plan content from tool-call XML
// ─────────────────────────────────────────────────────────────────────────────
/**
 * Claude (and sometimes other LLMs) wraps its actual analysis inside a
 * report_plan or submit_plan tool invocation.  The useful data lives in
 * <parameter name="summary"> and <parameter name="steps">.  If we strip
 * the entire <function_calls> block we lose the plan entirely (user sees
 * "0 response").  This helper extracts plan data as readable Markdown
 * BEFORE the XML purge runs.
 */
function extractPlanFromToolCalls(text: string): { cleaned: string; plans: string[] } {
  const plans: string[] = [];
  // Match invoke blocks for report_plan / submit_plan / create_plan.
  const invokePattern =
    /<invoke\s+name\s*=\s*"(?:report_plan|submit_plan|create_plan)"[^>]*>([\s\S]*?)<\/invoke>/gi;
  let m: RegExpExecArray | null;
  while ((m = invokePattern.exec(text)) !== null) {
    const body = m[1];
    // Extract named parameters.
    const paramPattern = /<parameter\s+name\s*=\s*"(\w+)"[^>]*>([\s\S]*?)<\/parameter>/gi;
    const params: Record<string, string> = {};
    let pm: RegExpExecArray | null;
    while ((pm = paramPattern.exec(body)) !== null) {
      params[pm[1]] = pm[2].trim();
    }
    const parts: string[] = [];
    if (params.summary) {
      parts.push(params.summary);
    }
    // Steps can be a JSON array or plain text.
    if (params.steps) {
      try {
        const steps = JSON.parse(params.steps);
        if (Array.isArray(steps)) {
          const stepLines = steps.map((s: Record<string, string>, i: number) => {
            const title = s.title || s.step || `Step ${i + 1}`;
            const desc = s.description || s.detail || "";
            return desc ? `${i + 1}. **${title}** — ${desc}` : `${i + 1}. **${title}**`;
          });
          parts.push(stepLines.join("\n"));
        }
      } catch {
        // Not valid JSON — use as plain text.
        parts.push(params.steps);
      }
    }
    if (params.affected_files) {
      try {
        const files = JSON.parse(params.affected_files);
        if (Array.isArray(files) && files.length > 0) {
          parts.push("**Affected files:** " + files.map((f: string) => `\`${f}\``).join(", "));
        }
      } catch {
        parts.push("**Affected files:** " + params.affected_files);
      }
    }
    if (params.complexity) {
      parts.push("**Complexity:** " + params.complexity);
    }
    if (parts.length > 0) {
      plans.push(parts.join("\n\n"));
    }
  }
  return { cleaned: text, plans };
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Anthropic / Claude
//    Claude is the most aggressive leaker — full XML tool-call blocks,
//    <function_calls>, <invoke>, <parameter>, truncated partial tags, and
//    tool-narration lines between invocations.
// ─────────────────────────────────────────────────────────────────────────────
function sanitizeAnthropic(text: string): string {
  let cleaned = text;

  // ── Step 0: Extract plan data BEFORE we strip XML ───────────────────
  // Claude sometimes wraps its entire analysis inside a report_plan
  // tool invocation.  If we strip all <function_calls> first, the plan
  // content is lost and the user sees an empty response.
  const { plans } = extractPlanFromToolCalls(cleaned);

  cleaned = cleaned.replace(
    /<[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\b[\s\S]*?<\/[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\s*>/gi,
    "",
  );

  cleaned = cleaned.replace(
    /<[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\b(?:(?!<\/[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter))[\s\S])*$/gi,
    "",
  );

  cleaned = cleaned.replace(
    /<\/?[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)[^>]*>/gi,
    "",
  );


  cleaned = cleaned.replace(
    /<result>([\s\S]*?)<\/result>/gi,
    (_match, inner: string) => extractCodeFromToolResult(inner),
  );
  cleaned = cleaned.replace(
    /<result>(?:(?!<\/result>)[\s\S])*$/gi,
    "",
  );
  cleaned = cleaned.replace(/<\/?result\s*>/gi, "");


  cleaned = cleaned.replace(
    /<function_result>([\s\S]*?)<\/function_result>/gi,
    (_match, inner: string) => extractCodeFromToolResult(inner),
  );

  cleaned = cleaned.replace(/<function_calls>[\s\S]*?<\/function_calls>/gi, "");
  cleaned = cleaned.replace(/<invoke[\s\S]*?<\/invoke>/gi, "");

  cleaned = cleaned.replace(/<parameter[^>]*$/gm, "");
  cleaned = cleaned.replace(/<invoke[^>]*$/gm, "");
  cleaned = cleaned.replace(/<function_calls[^>]*$/gm, "");
  cleaned = cleaned.replace(/<function_result[^>]*$/gm, "");

  let prev = "";
  while (prev !== cleaned) {
    prev = cleaned;
    cleaned = cleaned.replace(
      /<(?:invoke|function_calls|function_result)\b(?:(?!<\/(?:invoke|function_calls|function_result))[\s\S])*$/i,
      "",
    );
  }

  cleaned = cleaned.replace(
    /<\/?(?:function_calls|invoke|function_result|parameter|antml:invoke|antml:parameter)[^>]*>/gi,
    "",
  );

  cleaned = cleaned.replace(/\{"type"\s*:\s*"tool_(?:use|result)"[^}]{0,3000}\}/g, "");

  cleaned = cleaned.replace(
    /^(?:(?:Now )?[Ll]et me (?:search|look|examine|check|find|also|now|see|explore|investigate|analyze|dig|review|get|read|trace|view|open|inspect|scan|browse|pull|fetch|query|retrieve|try|understand|start|continue|proceed|narrow|focus|zoom)|(?:I'll|I will|I need to|I want to|I should|I'm going to) (?:search|look|examine|check|find|now|also|start|try|analyze|explore|investigate|help|dig|review|get|read|trace|view|open|inspect|need|scan|browse|pull|fetch|query|retrieve|understand|continue|proceed)|(?:Let me also|Now I'll|Now let me|And let me)|(?:Based on (?:my|the|this)|After reviewing|After examining|Looking at|Searching for)).*$/gm,
    "",
  );

  cleaned = cleaned.replace(
    /^(?:The (?:search|repository|file|code|results?|index|tool)|(?:Here are|I found|Found \d+|Searching|Checking)\s)(?:(?:returned|is indexed|contains|shows|are the|the relevant|matches in)\b)[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^I can see (?:the issue|that|from|here|it|this)[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(/^\s*:\s*$/gm, "");

  // ── Inject extracted plan data if cleaning wiped everything ─────────
  // If the entire response was tool-call XML (common when Claude wraps
  // its analysis in report_plan), the cleaned text is empty or nearly
  // so.  Inject the extracted plan content to give the user something.
  const trimmedResult = cleaned.replace(/\n{2,}/g, "\n\n").trim();
  if (plans.length > 0 && trimmedResult.length < 50) {
    return plans.join("\n\n---\n\n");
  }
  // Even if there IS some residual text, append plans if they exist —
  // the plan is the LLM's actual analysis and should be shown.
  if (plans.length > 0) {
    return trimmedResult + "\n\n" + plans.join("\n\n---\n\n");
  }

  return cleaned;
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Grok
//    Grok leaks tool-call narration ("Action: Use search_code…"),
//    repo-id UUIDs embedded in sentences, broken code fences (`` vs ```),
//    and JSON tool fragments.
// ─────────────────────────────────────────────────────────────────────────────
function sanitizeGrok(text: string): string {
  let cleaned = text;

  // JSON tool fragments.
  cleaned = cleaned.replace(/\{"type"\s*:\s*"tool_(?:use|result)"[^}]{0,3000}\}/g, "");

  // ── Tool narration headers ──────────────────────────────────────────
  // Grok dumps "Action:", "Tool Usage:", etc. as plain text or bold-Markdown:
  //   "Action: use search_code…"
  //   "**Action**: `search_code` with query…"
  //   "- **Tool Usage**: `get_index_status` to verify…"
  // The optional prefix [-*\s]* handles bullet/bold/indent variants.

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?(?:Next\s+)?Action(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?Tool Usage(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?Search (?:Queries|Terms)(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?File Inspection(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^\*?\(Assuming\s[^\n]*\)\*?\s*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^Assuming (?:the |search |this )[^\n]*$/gim,
    "",
  );

  // Inline repo-id UUIDs inside sentences (shared rule only catches
  // lines that are JUST a UUID — Grok embeds them in prose).
  cleaned = cleaned.replace(
    /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/g,
    "",
  );

  // Broken double-backtick fences: Grok sometimes writes ``json instead
  // of ```json. Fix them so Markdown renders properly.
  // Opening: ``lang  →  ```lang  (only at start of line, 2 backticks not 3+)
  cleaned = cleaned.replace(
    /^``(?!`)(\w+)\s*$/gm,
    "```$1",
  );
  // Closing: ``  →  ```  (bare double-backtick line)
  cleaned = cleaned.replace(
    /^``\s*$/gm,
    "```",
  );

  // "Agents Needed:" section with a JSON array on the next line(s).
  // Grok echoes the agents_needed parameter from report_plan.
  cleaned = cleaned.replace(
    /^Agents Needed:\s*\n``(?:`)?json\s*\n\[.*?\]\s*\n``(?:`)?/gm,
    "",
  );

  // ── Broad tool-narration patterns ─────────────────────────────────────
  // Grok narrates its tool usage: "I will use the search_code tool…",
  // "I will use get_file_content to review…", "If necessary, I will use…",
  // "After gathering…, I will draft/analyze/submit…"
  cleaned = cleaned.replace(
    /^(?:I will (?:use the provided tools|use (?:the )?(?:search_code|get_file_content|get_index_status|report_plan|submit_plan)\s+tool|start by|now (?:proceed|search|check|examine)|follow|also (?:check|look|use))|Let me (?:begin|start|now) by|If necessary,?\s+I will)[^\n]*$/gim,
    "",
  );

  // "Tool:" / "Parameters:" / "Queries:" headers — raw tool invocation echoes.
  cleaned = cleaned.replace(
    /^(?:Tool|Parameters|Queries)\s*:\s*[^\n]*$/gim,
    "",
  );

  // "Repository ID: ``" or "Repository ID: <uuid>" lines.
  cleaned = cleaned.replace(
    /^Repository ID\s*:\s*[^\n]*$/gim,
    "",
  );

  // Lines that are just a tool name (bare or backtick-wrapped).
  cleaned = cleaned.replace(
    /^\s*`?(?:search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index)`?\s*$/gim,
    "",
  );

  // "After gathering the necessary information, I will draft/analyze…"
  cleaned = cleaned.replace(
    /^After gathering[^\n]*$/gim,
    "",
  );

  // "Based on the search results, I will analyze:"
  cleaned = cleaned.replace(
    /^Based on (?:the search results|the results|my search|the gathered)[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^(?:First|Next|Then|Finally|Now|Additionally|Furthermore),?\s+I (?:will|need to|should|am going to|'ll)\s[^\n]*$/gim,
    "",
  );

  // Generic "I will <verb>" lines that narrate tool planning.
  cleaned = cleaned.replace(
    /^I will (?:also |then |now |next )?(?:use|check|look|review|search|examine|inspect|verify|analyze|draft|submit|create|gather|ensure|focus|need)\s[^\n]*$/gim,
    "",
  );

  // "Ensure these parameters are either removed…" — imperative planning lines.
  cleaned = cleaned.replace(
    /^Ensure (?:these|the|that|this)[^\n]*$/gim,
    "",
  );

  // "Update the runtime argument parsing…" — imperative single-line instructions.
  cleaned = cleaned.replace(
    /^(?:Update|Modify|Remove|Filter|Add|Change|Fix|Adjust) the (?:runtime|model|export|argument|config|script|file|code|function|method|class|constructor|parameter)[^\n]*$/gim,
    "",
  );

  // "This plan will be submitted…" / "Below is the structured plan…"
  // "If there are any discrepancies…" / "Based on the task description…"
  // "Based on the information gathered:" / "I will now submit this plan…"
  cleaned = cleaned.replace(
    /^(?:This (?:plan )?will be submitted|Below is the structured plan|If there are any discrepancies|I will now submit this plan|Based on (?:the (?:task description|information gathered|analysis|error)|my (?:exploration|analysis|review)|standard workflow))[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^(?:### )?Step \d+:\s*(?:Understand(?:ing)?|Analy[sz]e|Submit|Gather|Search|Check|Verify|Review|Identify|Determine|Plan|Prepare|Locate|Modify|Update|Test|Implement|Fix|Explore)\s+(?:the\s+|and\s+)?(?:Codebase|Scope|Plan|Context|Information|Data|Code|Issue|Files|Repository|Error|Fix|Changes|Logic|Export|Runtime|Model)[^\n]*$/gim,
    "",
  );

  // "Once confirmed, I will…" / "Once I have gathered…" / "Once verified…"
  cleaned = cleaned.replace(
    /^Once (?:confirmed|I have|the|this|verified|indexed)[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?(?:Preliminary )?(?:Plan )?(?:Document for Task|Summary)(?:\*\*)?\s*:?\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?(?:Step-by-Step\s+)?Implementation Plan(?:\*\*)?\s*(?:\(JSON[^\)]*\))?\s*:?\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?(?:Files to Modify|Affected Files)(?:\*\*)?\s*:?\s*$/gim,
    "",
  );
  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?(?:Files to Modify|Affected Files)(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );
  cleaned = cleaned.replace(
    /^[-*\s]*(?:Likely (?:the|a) (?:file|module|script|config)|Potentially (?:other|the)|Export script|The file defining)[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?(?:Estimated )?Complexity(?: Estimate)?(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?Agents Needed(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^[-*\s]*(?:\*\*)?Steps Needed(?:\*\*)?\s*:\s*[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /```json\s*\n\s*\[\s*\{[\s\S]*?"step"\s*:[\s\S]*?\}\s*\]\s*\n\s*```/gm,
    "",
  );

  cleaned = cleaned.replace(
    /```json\s*\n\s*\[\s*\{[\s\S]*?"step_id"\s*:[\s\S]*?\}\s*\]\s*\n\s*```/gm,
    "",
  );

  cleaned = cleaned.replace(
    /^\s*\[\s*\n?\s*\{[\s\S]*?"(?:step|step_id)"\s*:[\s\S]*?\}\s*\]\s*$/gm,
    "",
  );

  cleaned = cleaned.replace(
    /^A (?:senior (?:developer|dev)|QA (?:agent|engineer)|(?:code|security) reviewer)\s+to\s[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^\d+\.\s*(?:Identify|Modify|Update|Test|Ensure|Verify|Review|Check|Fix|Implement|Locate|Search|Adjust|Remove|Filter|Examine|Validate)\s+(?:where|how|the|if|that|any|all|whether)\s[^\n]*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^\s*(?:report_plan|search_code|get_file_content|get_index_status|submit_plan|create_plan|get_index)\s*$/gim,
    "",
  );

  cleaned = cleaned.replace(
    /^``(?:`)?(?:\s*)$/gm,
    "",
  );

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

  // Empty code fences that Gemini echoes for its internal tool calls.
  // Only strip fences that have NO content between them (orphaned markers).
  // Legitimate code blocks (with content) are preserved.
  cleaned = cleaned.replace(/```(?:python|json|yaml|xml|go|bash|sh)?\s*\n\s*```/gm, "");

  // Python-style tool call lines that Gemini wraps inside code fences.
  // These look like: ```python\nprint(search_code(...))\n```
  // Strip the whole fenced tool call.
  cleaned = cleaned.replace(
    /```(?:python)?\s*\n(?:print\s*\(\s*)?(?:search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index)\s*\([^\n]*\)\s*\)?\s*\n```/gm,
    "",
  );

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
        /^i('ve|'ve| have)\s+(analy[sz]ed|reviewed|examined|formulated|assessed)/.test(lower) ||
        /^i\s+am\s+(?:the\s+)?(?:orchestrator|agent)\s+(?:agent\s+)?tasked\s+with/.test(lower) ||
        /^i\s+will\s+follow\s+the\s+outlined\s+steps/.test(lower) ||
        /^i\s+will\s+use\s+the\s+provided\s+tools/.test(lower) ||
        /^my\s+focus\s+will\s+be\s+on/.test(lower) ||
        /^understood\.\s+i\s+am\s+the/.test(lower)
      ) {
        continue;
      }
      pastPreamble = true;
    }
    result.push(line);
  }
  return result.length > 0 ? result.join("\n") : text;
}
