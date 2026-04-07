// Strip internal LLM tool-use markup from provider responses before rendering.
// Models like Claude emit XML-based function_calls / invoke blocks that
// should never be shown to the end user.

/**
 * Remove XML-based tool-call blocks and other LLM-internal markup from a
 * response string so only the human-readable prose remains.
 */
export function sanitizeLLMContent(raw: string): string {
  if (!raw) return raw;

  let cleaned = raw;

  // 1. Remove paired function_calls blocks (Claude-style).
  cleaned = cleaned.replace(/<function_calls>[\s\S]*?<\/function_calls>/g, "");

  // 2. Remove unclosed / trailing function_calls (truncated responses).
  cleaned = cleaned.replace(/<function_calls>[\s\S]*$/g, "");

  // 3. Remove individual invoke / antml:invoke tags that might be outside blocks.
  cleaned = cleaned.replace(/<\/?invoke[^>]*>/g, "");
  cleaned = cleaned.replace(/<\/?antml:invoke[^>]*>/g, "");
  cleaned = cleaned.replace(/<\/?antml:parameter[^>]*>[\s\S]*?(?:<\/antml:parameter>|$)/g, "");

  // 4. Remove workspace_ tool calls that appear as plain text.
  //    e.g. "<invoke name="workspace_search">" or "<invoke name="workspace_list_dir">"
  cleaned = cleaned.replace(/<invoke\s+name="workspace_[^"]*"[^>]*>[\s\S]*?<\/invoke>/g, "");

  // 5. Remove parameter tags.
  cleaned = cleaned.replace(/<\/?parameter[^>]*>/g, "");

  // 6. Remove VSCode.Cell tags.
  cleaned = cleaned.replace(/<\/?VSCode\.Cell[^>]*>/g, "");

  // 7. Collapse excessive whitespace left by removals.
  cleaned = cleaned.replace(/\n{3,}/g, "\n\n");

  // 8. Trim leading/trailing whitespace.
  cleaned = cleaned.trim();

  return cleaned;
}
