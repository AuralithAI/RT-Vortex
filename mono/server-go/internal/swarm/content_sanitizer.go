package swarm

import (
	"fmt"
	"regexp"
	"strings"
)

// SanitizeLLMContent strips tool-call XML, narration lines, and other
// artifacts that LLM providers leak into their text responses.
//
// This runs server-side so that:
//   - WebSocket broadcasts carry clean content (no XML debris in the UI)
//   - Python consensus engine compares clean prose (not tool invocations)
//   - The HTTP probe response to the agent contains usable text
//
// The function is provider-aware: each provider leaks different patterns.
func SanitizeLLMContent(content, provider string) string {
	if content == "" {
		return ""
	}

	cleaned := content

	switch strings.ToLower(provider) {
	case "anthropic":
		cleaned = sanitizeAnthropic(cleaned)
	case "grok":
		cleaned = sanitizeGrok(cleaned)
	case "gemini", "google":
		cleaned = sanitizeGemini(cleaned)
	case "openai":
		// OpenAI rarely leaks tool artifacts.
	}

	// Shared rules — safe for all providers.
	cleaned = sanitizeShared(cleaned)

	cleaned = strings.TrimSpace(cleaned)

	// If everything was stripped, check if we can extract a plan summary
	// from the original content (report_plan tool call).
	if len(cleaned) < 20 {
		if plan := extractPlanContent(content); plan != "" {
			cleaned = plan
		}
	}

	return cleaned
}

// ── Compiled regexes (package-level, compiled once) ─────────────────────────

var (
	// Namespaced XML tool blocks: <prefix:function_calls>…</prefix:function_calls>
	reNamespacedXMLPaired = regexp.MustCompile(
		`(?is)<[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\b[\s\S]*?</[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\s*>`)
	reNamespacedXMLOrphan = regexp.MustCompile(
		`(?i)</?[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)[^>]*>`)

	// Non-namespaced XML tool blocks.
	reFunctionCalls       = regexp.MustCompile(`(?is)<function_calls>[\s\S]*?</function_calls>`)
	reFunctionResult      = regexp.MustCompile(`(?is)<function_result>[\s\S]*?</function_result>`)
	reInvoke              = regexp.MustCompile(`(?is)<invoke[\s\S]*?</invoke>`)
	reResult              = regexp.MustCompile(`(?is)<result>[\s\S]*?</result>`)
	reUnclosedToolTagOpen = regexp.MustCompile(
		`(?i)<(function_calls|invoke|function_result|result)\b`)
	reOrphanToolTag = regexp.MustCompile(
		`(?i)</?(?:function_calls|invoke|function_result|parameter|result|antml:invoke|antml:parameter)[^>]*>`)

	// Snake-case XML tags (search_code, get_file_content, etc.).
	// Go RE2 does not support backreferences (\1), so we enumerate known tool names.
	reSnakeCasePaired = regexp.MustCompile(
		`(?is)<(search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index|step_by_step_plan|affected_files|agents_needed|target_files|expected_outcome|repo_id|file_path|start_line|end_line)\s*>[\s\S]*?</(?:search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index|step_by_step_plan|affected_files|agents_needed|target_files|expected_outcome|repo_id|file_path|start_line|end_line)\s*>`)
	reSnakeCaseSelf = regexp.MustCompile(
		`(?i)<([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*/?>`)
	reSnakeCaseClose = regexp.MustCompile(
		`(?i)</([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\s*>`)

	// Known single-word tool-parameter wrapper tags.
	reKnownParamTags = regexp.MustCompile(
		`(?i)</?(?:query|path|instruction|step_by_step_plan|affected_files|agents_needed|summary|complexity|steps|details|step_id|description|target_files|expected_outcome|repo_id|file_path|start_line|end_line)\s*>`)

	// JSON tool fragments.
	reJSONToolFrag = regexp.MustCompile(`\{"type"\s*:\s*"tool_(?:use|result)"[^}]*\}`)

	// Bare UUID lines (repo_id echo).
	reUUIDLine = regexp.MustCompile(
		`(?m)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?:\s+\S.*)?$`)

	// Orphaned tool-call ID lines.
	reToolCallID = regexp.MustCompile(`(?m)^(?:toolu_|call_|chatcmpl-)[A-Za-z0-9_-]+\s*$`)

	// Collapse 3+ newlines.
	reExcessiveNewlines = regexp.MustCompile(`\n{3,}`)

	// ── Anthropic-specific ──────────────────────────────────────────────

	reAnthropicNarration = regexp.MustCompile(
		`(?m)^(?:(?:Now )?[Ll]et me (?:search|look|examine|check|find|also|now|see|explore|investigate|analyze|dig|review|get|read|trace|view|open|inspect|scan|browse|pull|fetch|query|retrieve|try|understand|start|continue|proceed|narrow|focus|zoom)` +
			`|(?:I'll|I will|I need to|I want to|I should|I'm going to) (?:search|look|examine|check|find|now|also|start|try|analyze|explore|investigate|help|dig|review|get|read|trace|view|open|inspect|need|scan|browse|pull|fetch|query|retrieve|understand|continue|proceed)` +
			`|(?:Let me also|Now I'll|Now let me|And let me)` +
			`|(?:Based on (?:my|the|this)|After reviewing|After examining|Looking at|Searching for)).*$`)

	reAnthropicStatusLine = regexp.MustCompile(
		`(?im)^(?:The (?:search|repository|file|code|results?|index|tool)|(?:Here are|I found|Found \d+|Searching|Checking)\s)(?:(?:returned|is indexed|contains|shows|are the|the relevant|matches in)\b)[^\n]*$`)

	reAnthropicICanSee = regexp.MustCompile(
		`(?im)^I can see (?:the issue|that|from|here|it|this)[^\n]*$`)

	// ── Grok-specific ───────────────────────────────────────────────────

	reGrokAction = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Next\s+)?Action(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokToolUsage = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?Tool Usage(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokSearchTerms = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Search (?:Queries|Terms))(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokFileInspection = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?File Inspection(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokAssuming = regexp.MustCompile(
		`(?im)^(?:\*?\(Assuming\s[^\n]*\)\*?|Assuming (?:the |search |this )[^\n]*)$`)
	reGrokInlineUUID = regexp.MustCompile(
		`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reGrokBrokenFenceOpen  = regexp.MustCompile("(?m)^``(\\w+)\\s*$")
	reGrokBrokenFenceClose = regexp.MustCompile("(?m)^``\\s*$")
	reGrokToolIntent       = regexp.MustCompile(
		`(?im)^(?:I will (?:use the provided tools|start by|now (?:proceed|search|check|examine)|follow|also check)|Let me (?:begin|start|now) by)[^\n]*$`)
	reGrokStepByStep = regexp.MustCompile(
		`(?im)^(?:First|Next|Then|Finally|Now),?\s+I(?:\s+will|\s+need\s+to|\s+should|\s+am\s+going\s+to|'ll)\s[^\n]*$`)
	reGrokPlanBoilerplate = regexp.MustCompile(
		`(?im)^(?:This (?:plan )?will be submitted|Below is the structured plan|If there are any discrepancies|I will now submit this plan|Based on (?:the (?:task description|information gathered|analysis|error)|my (?:exploration|analysis|review)|standard workflow))[^\n]*$`)
	reGrokStepNoun = regexp.MustCompile(
		`(?im)^(?:### )?Step \d+:\s*(?:Understand(?:ing)?|Analy[sz]e|Submit|Gather|Search|Check|Verify|Review|Identify|Determine|Plan|Prepare|Locate|Modify|Update|Test|Implement|Fix|Explore)\s+(?:the\s+|and\s+)?(?:Codebase|Scope|Plan|Context|Information|Data|Code|Issue|Files|Repository|Error|Fix|Changes|Logic|Export|Runtime|Model)[^\n]*$`)
	reGrokOnce = regexp.MustCompile(
		`(?im)^Once (?:confirmed|I have|the|this|verified|indexed)[^\n]*$`)
	reGrokPlanDoc = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Preliminary )?(?:Plan )?(?:Document for Task|Summary)(?:\*\*)?\s*:?\s*[^\n]*$`)
	reGrokImplPlan = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Step-by-Step\s+)?Implementation Plan(?:\*\*)?\s*(?:\(JSON[^\)]*\))?\s*:?\s*$`)
	reGrokAffectedFiles = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Files to Modify|Affected Files)(?:\*\*)?\s*:?\s*[^\n]*$`)
	reGrokComplexity = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Estimated )?Complexity(?: Estimate)?(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokAgentsNeeded = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?Agents Needed(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokStepsNeeded = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?Steps Needed(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokJSONStepArray = regexp.MustCompile(
		"(?s)```json\\s*\\n\\s*\\[\\s*\\{[\\s\\S]*?\"(?:step|step_id)\"\\s*:[\\s\\S]*?\\}\\s*\\]\\s*\\n\\s*```")
	reGrokBareJSONStepArray = regexp.MustCompile(
		`(?ms)^\s*\[\s*\n?\s*\{[\s\S]*?"(?:step|step_id)"\s*:[\s\S]*?\}\s*\]\s*$`)
	reGrokRoleDesc = regexp.MustCompile(
		`(?im)^A (?:senior (?:developer|dev)|QA (?:agent|engineer)|(?:code|security) reviewer)\s+to\s[^\n]*$`)
	reGrokBareToolName = regexp.MustCompile(
		`(?im)^\s*(?:report_plan|search_code|get_file_content|get_index_status|submit_plan|create_plan|get_index)\s*$`)
	reGrokToolHeader = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?Tool(?:\*\*)?\s*:\s*(?:get_index_status|search_code|get_file_content|report_plan|submit_plan|create_plan|get_index)\s*$`)
	reGrokParamsHeader = regexp.MustCompile(
		`(?im)^[-*\s]*(?:\*\*)?(?:Parameters|Queries)(?:\*\*)?\s*:\s*[^\n]*$`)
	reGrokLikelyFile = regexp.MustCompile(
		`(?im)^[-*\s]*(?:Likely (?:the|a) (?:file|module|script|config)|Potentially (?:other|the)|Export script|The file defining)[^\n]*$`)

	// ── Gemini-specific ─────────────────────────────────────────────────

	reGeminiPythonToolCall = regexp.MustCompile(
		`(?m)^(?:print\s*\(\s*)?(?:search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index)\s*\([^\n]*\)\s*\)?$`)
	reGeminiEmptyFence = regexp.MustCompile(
		"(?s)```(?:python|json|yaml|xml|go|bash|sh)?\\s*\\n\\s*```")
	reGeminiFencedToolCall = regexp.MustCompile(
		"(?s)```(?:python)?\\s*\\n(?:print\\s*\\(\\s*)?(?:search_code|get_file_content|get_index_status|report_plan|submit_plan|create_plan|get_index)\\s*\\([^\\n]*\\)\\s*\\)?\\s*\\n```")

	reTrailingJSONPlan = regexp.MustCompile(
		`(?s)\n\s*\{\s*\n?\s*"summary"\s*:\s*"[\s\S]*?"steps"\s*:\s*\[[\s\S]*?\]\s*,[\s\S]*?\}\s*$`)

	// ── Preamble patterns ───────────────────────────────────────────────

	rePreambleOrchestrator = regexp.MustCompile(
		`(?i)^(?:ok[,.]?\s+)?(?:i am|i'm|as)\s+(?:the\s+)?(?:an?\s+)?orchestrator`)
	rePreambleLetMe = regexp.MustCompile(
		`(?i)^(?:ok[,.]?\s+)?(?:let me|i'll|i will)\s+(?:start|begin|create|produce|analy[sz]e)`)
	rePreambleSure = regexp.MustCompile(
		`(?i)^(?:sure|alright|certainly|understood)[,!.]`)
	rePreambleHello = regexp.MustCompile(
		`(?i)^(?:hello|hi|hey|greetings)[!,.\s]`)
	rePreambleAs = regexp.MustCompile(
		`(?i)^as\s+the\s+(?:orchestrator|team\s+lead)`)
	rePreambleIHave = regexp.MustCompile(
		`(?i)^i(?:'ve|'ve| have)\s+(?:analy[sz]ed|reviewed|examined|formulated|assessed)`)
	rePreambleTasked = regexp.MustCompile(
		`(?i)^i\s+am\s+(?:the\s+)?(?:orchestrator|agent)\s+(?:agent\s+)?tasked\s+with`)
	rePreambleFollowSteps = regexp.MustCompile(
		`(?i)^i\s+will\s+follow\s+the\s+outlined\s+steps`)
	rePreambleUseTools = regexp.MustCompile(
		`(?i)^i\s+will\s+use\s+the\s+provided\s+tools`)
	rePreambleMyFocus = regexp.MustCompile(
		`(?i)^my\s+focus\s+will\s+be\s+on`)
	rePreambleUnderstood = regexp.MustCompile(
		`(?i)^understood\.\s+i\s+am\s+the`)

	// ── Plan extraction ─────────────────────────────────────────────────

	reReportPlanBlock = regexp.MustCompile(
		`(?is)<invoke\s+name\s*=\s*"report_plan"[^>]*>([\s\S]*?)</invoke>`)
	rePlanSummaryParam = regexp.MustCompile(
		`(?is)<parameter\s+name\s*=\s*"summary"[^>]*>([\s\S]*?)</parameter>`)
	rePlanStepsParam = regexp.MustCompile(
		`(?is)<parameter\s+name\s*=\s*"steps"[^>]*>([\s\S]*?)</parameter>`)
	rePlanAffectedParam = regexp.MustCompile(
		`(?is)<parameter\s+name\s*=\s*"affected_files"[^>]*>([\s\S]*?)</parameter>`)
	rePlanComplexityParam = regexp.MustCompile(
		`(?is)<parameter\s+name\s*=\s*"complexity"[^>]*>([\s\S]*?)</parameter>`)
)

// ── Provider-specific sanitizers ────────────────────────────────────────────

func sanitizeAnthropic(text string) string {
	c := text

	// Strip namespaced XML first (can be tens of thousands of chars).
	c = reNamespacedXMLPaired.ReplaceAllString(c, "")
	c = stripUnclosedNamespacedXML(c)
	c = reNamespacedXMLOrphan.ReplaceAllString(c, "")

	// Strip tool blocks.
	c = reFunctionResult.ReplaceAllString(c, "")
	c = reFunctionCalls.ReplaceAllString(c, "")
	c = reInvoke.ReplaceAllString(c, "")
	c = reResult.ReplaceAllString(c, "")

	// Trailing unclosed blocks (loop until stable).
	c = stripUnclosedToolXML(c)

	c = reOrphanToolTag.ReplaceAllString(c, "")
	c = reJSONToolFrag.ReplaceAllString(c, "")

	// Tool-narration lines.
	c = reAnthropicNarration.ReplaceAllString(c, "")
	c = reAnthropicStatusLine.ReplaceAllString(c, "")
	c = reAnthropicICanSee.ReplaceAllString(c, "")

	return c
}

func sanitizeGrok(text string) string {
	c := text

	c = reJSONToolFrag.ReplaceAllString(c, "")
	c = reGrokAction.ReplaceAllString(c, "")
	c = reGrokToolUsage.ReplaceAllString(c, "")
	c = reGrokSearchTerms.ReplaceAllString(c, "")
	c = reGrokFileInspection.ReplaceAllString(c, "")
	c = reGrokAssuming.ReplaceAllString(c, "")
	c = reGrokInlineUUID.ReplaceAllString(c, "")
	c = reGrokBrokenFenceOpen.ReplaceAllString(c, "```$1")
	c = reGrokBrokenFenceClose.ReplaceAllString(c, "```")
	c = reGrokToolIntent.ReplaceAllString(c, "")
	c = reGrokStepByStep.ReplaceAllString(c, "")
	c = reGrokPlanBoilerplate.ReplaceAllString(c, "")
	c = reGrokStepNoun.ReplaceAllString(c, "")
	c = reGrokOnce.ReplaceAllString(c, "")
	c = reGrokPlanDoc.ReplaceAllString(c, "")
	c = reGrokImplPlan.ReplaceAllString(c, "")
	c = reGrokAffectedFiles.ReplaceAllString(c, "")
	c = reGrokComplexity.ReplaceAllString(c, "")
	c = reGrokAgentsNeeded.ReplaceAllString(c, "")
	c = reGrokStepsNeeded.ReplaceAllString(c, "")
	c = reGrokJSONStepArray.ReplaceAllString(c, "")
	c = reGrokBareJSONStepArray.ReplaceAllString(c, "")
	c = reGrokRoleDesc.ReplaceAllString(c, "")
	c = reGrokBareToolName.ReplaceAllString(c, "")
	c = reGrokToolHeader.ReplaceAllString(c, "")
	c = reGrokParamsHeader.ReplaceAllString(c, "")
	c = reGrokLikelyFile.ReplaceAllString(c, "")

	return c
}

func sanitizeGemini(text string) string {
	c := text
	c = reGeminiPythonToolCall.ReplaceAllString(c, "")
	c = reGeminiEmptyFence.ReplaceAllString(c, "")
	c = reGeminiFencedToolCall.ReplaceAllString(c, "")
	c = reTrailingJSONPlan.ReplaceAllString(c, "")
	return c
}

// sanitizeShared applies rules safe for all providers.
func sanitizeShared(text string) string {
	c := text

	// XML tool blocks (catch any that provider-specific cleaners missed).
	c = reNamespacedXMLPaired.ReplaceAllString(c, "")
	c = stripUnclosedNamespacedXML(c)
	c = reNamespacedXMLOrphan.ReplaceAllString(c, "")
	c = reFunctionCalls.ReplaceAllString(c, "")
	c = reFunctionResult.ReplaceAllString(c, "")
	c = reInvoke.ReplaceAllString(c, "")
	c = reResult.ReplaceAllString(c, "")

	c = stripUnclosedToolXML(c)

	c = reOrphanToolTag.ReplaceAllString(c, "")
	c = reSnakeCasePaired.ReplaceAllString(c, "")
	c = reSnakeCaseSelf.ReplaceAllString(c, "")
	c = reSnakeCaseClose.ReplaceAllString(c, "")
	c = reKnownParamTags.ReplaceAllString(c, "")
	c = reUUIDLine.ReplaceAllString(c, "")
	c = reToolCallID.ReplaceAllString(c, "")

	c = reTrailingJSONPlan.ReplaceAllString(c, "")

	// Strip preamble.
	c = stripPreamble(c)

	c = reExcessiveNewlines.ReplaceAllString(c, "\n\n")
	return c
}

// stripUnclosedNamespacedXML removes trailing unclosed namespaced XML blocks
// like `<function_calls>...` that have no matching close tag.
// This replaces the negative-lookahead regex that Go RE2 doesn't support.
func stripUnclosedNamespacedXML(text string) string {
	// Find the last occurrence of a namespaced opening tag.
	re := regexp.MustCompile(`(?i)<[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\b`)
	locs := re.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return text
	}
	// Check from the last match backwards.
	for i := len(locs) - 1; i >= 0; i-- {
		start := locs[i][0]
		tail := text[start:]
		// If there's no matching close tag in the tail, strip from start.
		closeRe := regexp.MustCompile(`(?i)</[a-z][a-z0-9]*:(?:function_calls|invoke|function_result|parameter)\s*>`)
		if !closeRe.MatchString(tail) {
			text = text[:start]
		}
	}
	return text
}

// stripUnclosedToolXML removes trailing unclosed non-namespaced tool XML
// blocks like `<function_calls>...` that have no matching close tag.
// This replaces the negative-lookahead regex that Go RE2 doesn't support.
func stripUnclosedToolXML(text string) string {
	locs := reUnclosedToolTagOpen.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return text
	}
	for i := len(locs) - 1; i >= 0; i-- {
		start := locs[i][0]
		tail := text[start:]
		// Extract the tag name from the match.
		m := reUnclosedToolTagOpen.FindStringSubmatch(tail)
		if len(m) < 2 {
			continue
		}
		tagName := m[1]
		closeTag := "</" + tagName
		if !strings.Contains(strings.ToLower(tail), strings.ToLower(closeTag)) {
			text = text[:start]
		}
	}
	return text
}

// stripPreamble removes conversational self-introduction lines from the
// beginning of a response.
func stripPreamble(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	pastPreamble := false
	scanned := 0

	for _, line := range lines {
		if !pastPreamble {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			scanned++
			if scanned > 5 {
				pastPreamble = true
				result = append(result, line)
				continue
			}
			lower := strings.ToLower(trimmed)
			if rePreambleOrchestrator.MatchString(lower) ||
				rePreambleLetMe.MatchString(lower) ||
				rePreambleSure.MatchString(lower) ||
				rePreambleHello.MatchString(lower) ||
				rePreambleAs.MatchString(lower) ||
				rePreambleIHave.MatchString(lower) ||
				rePreambleTasked.MatchString(lower) ||
				rePreambleFollowSteps.MatchString(lower) ||
				rePreambleUseTools.MatchString(lower) ||
				rePreambleMyFocus.MatchString(lower) ||
				rePreambleUnderstood.MatchString(lower) {
				continue
			}
			pastPreamble = true
		}
		result = append(result, line)
	}

	if len(result) > 0 {
		return strings.Join(result, "\n")
	}
	return text
}

// extractPlanContent extracts a readable plan from report_plan tool
// invocations embedded in XML tool-call blocks. This is the fallback when
// the entire LLM response is tool invocations (common with Claude).
func extractPlanContent(raw string) string {
	matches := reReportPlanBlock.FindStringSubmatch(raw)
	if len(matches) < 2 {
		return ""
	}
	body := matches[1]

	var parts []string

	// Extract summary.
	if sm := rePlanSummaryParam.FindStringSubmatch(body); len(sm) >= 2 {
		summary := strings.TrimSpace(sm[1])
		if summary != "" {
			parts = append(parts, "## Summary\n\n"+summary)
		}
	}

	// Extract steps — try to parse as JSON array for clean rendering.
	if sm := rePlanStepsParam.FindStringSubmatch(body); len(sm) >= 2 {
		stepsRaw := strings.TrimSpace(sm[1])
		if stepsRaw != "" {
			rendered := renderPlanSteps(stepsRaw)
			if rendered != "" {
				parts = append(parts, "## Implementation Steps\n\n"+rendered)
			}
		}
	}

	// Extract affected files.
	if sm := rePlanAffectedParam.FindStringSubmatch(body); len(sm) >= 2 {
		files := strings.TrimSpace(sm[1])
		if files != "" {
			parts = append(parts, "## Affected Files\n\n"+files)
		}
	}

	// Extract complexity.
	if sm := rePlanComplexityParam.FindStringSubmatch(body); len(sm) >= 2 {
		complexity := strings.TrimSpace(sm[1])
		if complexity != "" {
			parts = append(parts, "**Complexity:** "+complexity)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// renderPlanSteps converts a JSON step array or plain text steps into
// a clean numbered list. Handles both JSON and plain-text formats.
func renderPlanSteps(raw string) string {
	raw = strings.TrimSpace(raw)

	// Try JSON array: [{"step_id":"1","description":"...","target_files":["..."]}]
	// Simple field extraction without a full JSON parser.
	if strings.HasPrefix(raw, "[") {
		var lines []string
		stepNum := 0

		// Split on },{ boundaries.
		entries := splitJSONArray(raw)
		for _, entry := range entries {
			stepNum++
			desc := extractJSONField(entry, "description")
			if desc == "" {
				desc = extractJSONField(entry, "step")
			}
			if desc == "" {
				continue
			}
			target := extractJSONField(entry, "target_files")
			line := fmt.Sprintf("%d. %s", stepNum, desc)
			if target != "" && target != "[]" {
				line += "\n   Files: " + target
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}

	// Fallback: return as-is (might already be numbered text).
	return raw
}

// splitJSONArray splits a JSON array string into individual object strings.
func splitJSONArray(arr string) []string {
	arr = strings.TrimSpace(arr)
	if !strings.HasPrefix(arr, "[") || !strings.HasSuffix(arr, "]") {
		return nil
	}
	arr = arr[1 : len(arr)-1] // strip [ ]

	var results []string
	depth := 0
	start := 0

	for i, ch := range arr {
		switch ch {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 {
				results = append(results, arr[start:i+1])
			}
		}
	}
	return results
}

// extractJSONField extracts a string value from a JSON object string.
func extractJSONField(obj, field string) string {
	key := `"` + field + `"`
	idx := strings.Index(obj, key)
	if idx < 0 {
		return ""
	}
	rest := obj[idx+len(key):]
	// Skip : and whitespace.
	rest = strings.TrimLeft(rest, ": \t\n")
	if len(rest) == 0 {
		return ""
	}

	if rest[0] == '"' {
		// String value.
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			return rest[1:]
		}
		return rest[1 : end+1]
	}
	if rest[0] == '[' {
		// Array value — return as-is up to the closing bracket.
		depth := 0
		for i, ch := range rest {
			switch ch {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					return rest[:i+1]
				}
			}
		}
	}
	// Number or other value — grab until comma or closing brace.
	end := strings.IndexAny(rest, ",}")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}
