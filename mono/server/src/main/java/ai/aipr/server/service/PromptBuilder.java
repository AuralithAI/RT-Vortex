package ai.aipr.server.service;

import ai.aipr.server.dto.ContextPack;
import ai.aipr.server.dto.FileChange;
import ai.aipr.server.dto.HeuristicFinding;
import ai.aipr.server.dto.ReviewConfig;
import ai.aipr.server.dto.TouchedSymbol;
import org.jetbrains.annotations.NotNull;
import org.springframework.stereotype.Service;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

/**
 * Service for building prompts for LLM reviews.
 */
@Service
public class PromptBuilder {

    private static final String SYSTEM_PROMPT_TEMPLATE = """
        You are an expert code reviewer. Your task is to review the code changes (diff) in a pull request.

        Guidelines:
        - Focus on code quality, potential bugs, security issues, and maintainability
        - Be constructive and provide actionable feedback
        - Prioritize critical issues over style suggestions
        - Consider the context provided from the codebase
        - Respond in %s

        Review Depth: %s
        Focus Areas: %s

        Output Format:
        Return a JSON object with the following structure:
        {
            "summary": "Brief overview of the changes",
            "overallAssessment": "APPROVE | REQUEST_CHANGES | COMMENT",
            "comments": [
                {
                    "filePath": "path/to/file.ext",
                    "lineStart": 10,
                    "lineEnd": 15,
                    "body": "Comment text",
                    "severity": "error | warning | info | suggestion",
                    "category": "security | performance | bug | style | documentation | maintainability",
                    "suggestion": "Optional code suggestion",
                    "blocking": true | false
                }
            ],
            "suggestions": ["List of general suggestions"]
        }
        """;

    private static final String USER_PROMPT_TEMPLATE = """
        ## Pull Request Context
        %s

        ## Code Changes (Diff)
        %s

        ## Relevant Context from Codebase
        %s

        ## Heuristic Findings (Pre-detected Issues)
        %s

        Please review the changes and provide feedback.
        """;

    /**
     * Build a complete review prompt.
     */
    public LLMPrompt buildReviewPrompt(
            ContextPack contextPack,
            List<HeuristicFinding> heuristicFindings,
            ReviewConfig config
    ) {
        String systemPrompt = buildSystemPrompt(config);
        String userPrompt = buildUserPrompt(contextPack, heuristicFindings, config);

        return new LLMPrompt(systemPrompt, userPrompt);
    }

    /**
     * Build the system prompt with configuration.
     * Uses {@link ReviewConfig} fields: responseLanguage, reviewDepth, focusAreas,
     * includeSecurityChecks, includePerformanceChecks, includeStyleChecks.
     */
    private String buildSystemPrompt(ReviewConfig config) {
        String language = config != null ? config.responseLanguage() : "English";
        String depth = config != null ? config.reviewDepth() : "detailed";
        String focusAreas = config != null && config.focusAreas() != null && !config.focusAreas().isEmpty()
                ? String.join(", ", config.focusAreas())
                : "all";

        String prompt = String.format(SYSTEM_PROMPT_TEMPLATE, language, depth, focusAreas);

        // Add disabled category instructions based on config flags
        if (config != null) {
            List<String> disabledCategories = new ArrayList<>();
            if (!config.includeSecurityChecks()) disabledCategories.add("security");
            if (!config.includePerformanceChecks()) disabledCategories.add("performance");
            if (!config.includeStyleChecks()) disabledCategories.add("style");

            if (!disabledCategories.isEmpty()) {
                prompt += "\nDisabled categories (do NOT review these): "
                        + String.join(", ", disabledCategories) + "\n";
            }
        }

        return prompt;
    }

    /**
     * Build the user prompt with context and diff.
     */
    @NotNull
    private String buildUserPrompt(ContextPack contextPack, List<HeuristicFinding> heuristicFindings,
                                   ReviewConfig config) {
        List<String> ignorePaths = config != null && config.ignorePaths() != null
                ? config.ignorePaths() : List.of();
        int maxContextTokens = config != null ? config.maxContextTokens() : 128000;

        String prContext = buildPRContext(contextPack, ignorePaths);
        String diff = buildDiffSection(contextPack);
        String codebaseContext = buildCodebaseContext(contextPack, maxContextTokens, ignorePaths);
        String heuristics = buildHeuristicsSection(heuristicFindings);

        return String.format(USER_PROMPT_TEMPLATE, prContext, diff, codebaseContext, heuristics);
    }

    /**
     * Build PR context section.
     * Filters out files matching ignorePaths and displays language/binary info.
     */
    @NotNull
    private String buildPRContext(@NotNull ContextPack contextPack, List<String> ignorePaths) {
        StringBuilder sb = new StringBuilder();

        if (contextPack.prTitle() != null) {
            sb.append("**Title:** ").append(contextPack.prTitle()).append("\n");
        }

        if (contextPack.prDescription() != null && !contextPack.prDescription().isBlank()) {
            sb.append("**Description:**\n").append(contextPack.prDescription()).append("\n");
        }

        if (contextPack.changedFiles() != null && !contextPack.changedFiles().isEmpty()) {
            List<FileChange> visibleFiles = contextPack.changedFiles().stream()
                    .filter(f -> !isIgnored(f.path(), ignorePaths))
                    .toList();

            sb.append("**Changed Files:** ").append(visibleFiles.size());
            if (visibleFiles.size() < contextPack.changedFiles().size()) {
                sb.append(" (").append(contextPack.changedFiles().size() - visibleFiles.size())
                  .append(" ignored)");
            }
            sb.append("\n");

            for (var file : visibleFiles) {
                sb.append("  - ").append(file.path())
                    .append(" (").append(file.changeType()).append(")");
                if (file.language() != null) {
                    sb.append(" [").append(file.language()).append("]");
                }
                sb.append(" +").append(file.additions())
                    .append(" -").append(file.deletions());
                if (file.isBinary()) {
                    sb.append(" [binary]");
                }
                sb.append("\n");
            }
        }

        if (contextPack.touchedSymbols() != null && !contextPack.touchedSymbols().isEmpty()) {
            sb.append("**Touched Symbols:**\n");
            Map<String, List<TouchedSymbol>> byFile = contextPack.touchedSymbols().stream()
                    .collect(Collectors.groupingBy(TouchedSymbol::filePath));

            for (var entry : byFile.entrySet()) {
                sb.append("  ").append(entry.getKey()).append(":\n");
                for (var symbol : entry.getValue()) {
                    sb.append("    - ").append(symbol.name())
                        .append(" (").append(symbol.kind()).append(")\n");
                }
            }
        }

        return sb.toString();
    }

    /**
     * Build diff section.
     */
    @NotNull
    private String buildDiffSection(@NotNull ContextPack contextPack) {
        if (contextPack.diff() == null || contextPack.diff().isBlank()) {
            return "*No diff provided*";
        }

        // Truncate if too long
        String diff = contextPack.diff();
        if (diff.length() > 50000) {
            diff = diff.substring(0, 50000) + "\n... [truncated]";
        }

        return "```diff\n" + diff + "\n```";
    }

    /**
     * Build codebase context section.
     * Respects maxContextTokens budget and filters ignored paths.
     */
    @NotNull
    private String buildCodebaseContext(@NotNull ContextPack contextPack, int maxContextTokens,
                                        List<String> ignorePaths) {
        if (contextPack.contextChunks() == null || contextPack.contextChunks().isEmpty()) {
            return "*No additional context retrieved*";
        }

        StringBuilder sb = new StringBuilder();
        int estimatedTokens = 0;

        for (var chunk : contextPack.contextChunks()) {
            if (isIgnored(chunk.filePath(), ignorePaths)) {
                continue;
            }

            // Estimate tokens for this chunk (~4 chars per token)
            int chunkTokens = (chunk.content() != null ? chunk.content().length() : 0) / 4;
            if (estimatedTokens + chunkTokens > maxContextTokens) {
                sb.append("\n*... context truncated (token budget: ")
                  .append(maxContextTokens).append(")*\n");
                break;
            }
            estimatedTokens += chunkTokens;
            sb.append("### ").append(chunk.filePath())
                .append(" (lines ").append(chunk.startLine())
                .append("-").append(chunk.endLine())
                .append(")\n");

            if (chunk.relevanceScore() > 0) {
                sb.append("*Relevance: ").append(String.format("%.2f", chunk.relevanceScore())).append("*\n");
            }

            sb.append("```").append(getLanguageHint(chunk.filePath())).append("\n");
            sb.append(chunk.content());
            sb.append("\n```\n\n");
        }

        return sb.toString();
    }

    /**
     * Build heuristics section.
     */
    @NotNull
    private String buildHeuristicsSection(List<HeuristicFinding> findings) {
        if (findings == null || findings.isEmpty()) {
            return "*No issues detected by heuristics*";
        }

        StringBuilder sb = new StringBuilder();

        for (var finding : findings) {
            sb.append("- **[").append(finding.severity().toUpperCase()).append("]** ")
                .append(finding.rule()).append("\n");
            sb.append("  File: ").append(finding.filePath());
            if (finding.line() != null) {
                sb.append(", Line ").append(finding.line());
            }
            sb.append("\n");
            sb.append("  ").append(finding.message()).append("\n\n");
        }

        return sb.toString();
    }

    /**
     * Get language hint for code block.
     */
    private String getLanguageHint(String filePath) {
        if (filePath == null) return "";

        int lastDot = filePath.lastIndexOf('.');
        if (lastDot < 0) return "";

        String ext = filePath.substring(lastDot + 1).toLowerCase();
        return switch (ext) {
            case "java" -> "java";
            case "kt", "kts" -> "kotlin";
            case "ts", "tsx" -> "typescript";
            case "js", "jsx", "mjs" -> "javascript";
            case "py" -> "python";
            case "cpp", "cc", "cxx", "hpp", "h" -> "cpp";
            case "c" -> "c";
            case "go" -> "go";
            case "rs" -> "rust";
            case "rb" -> "ruby";
            case "php" -> "php";
            case "cs" -> "csharp";
            case "swift" -> "swift";
            case "scala" -> "scala";
            case "sh", "bash" -> "bash";
            case "yml", "yaml" -> "yaml";
            case "json" -> "json";
            case "xml" -> "xml";
            case "sql" -> "sql";
            case "md" -> "markdown";
            default -> ext;
        };
    }

    /**
     * LLM prompt structure.
     */
    public record LLMPrompt(String systemPrompt, String userPrompt) {
        public int estimateTokens() {
            // Rough estimate: ~4 chars per token
            return (systemPrompt.length() + userPrompt.length()) / 4;
        }
    }

    /**
     * Check if a file path matches any of the ignore patterns.
     * Supports simple glob-like patterns: {@code **\/vendor}, {@code *.lock}, {@code node_modules/}.
     */
    private boolean isIgnored(String filePath, List<String> ignorePaths) {
        if (filePath == null || ignorePaths == null || ignorePaths.isEmpty()) {
            return false;
        }
        for (String pattern : ignorePaths) {
            if (pattern.startsWith("**/")) {
                String suffix = pattern.substring(3);
                if (filePath.contains(suffix)) return true;
            } else if (pattern.startsWith("*.")) {
                if (filePath.endsWith(pattern.substring(1))) return true;
            } else {
                if (filePath.startsWith(pattern)) return true;
            }
        }
        return false;
    }
}

