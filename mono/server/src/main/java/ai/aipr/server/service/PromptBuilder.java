package ai.aipr.server.service;

import ai.aipr.server.dto.*;
import org.springframework.stereotype.Service;

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
        String userPrompt = buildUserPrompt(contextPack, heuristicFindings);
        
        return new LLMPrompt(systemPrompt, userPrompt);
    }
    
    /**
     * Build the system prompt with configuration.
     */
    private String buildSystemPrompt(ReviewConfig config) {
        String language = config != null ? config.responseLanguage() : "English";
        String depth = config != null ? config.reviewDepth() : "detailed";
        String focusAreas = config != null && config.focusAreas() != null && !config.focusAreas().isEmpty()
                ? String.join(", ", config.focusAreas())
                : "all";
        
        return String.format(SYSTEM_PROMPT_TEMPLATE, language, depth, focusAreas);
    }
    
    /**
     * Build the user prompt with context and diff.
     */
    private String buildUserPrompt(ContextPack contextPack, List<HeuristicFinding> heuristicFindings) {
        String prContext = buildPRContext(contextPack);
        String diff = buildDiffSection(contextPack);
        String codebaseContext = buildCodebaseContext(contextPack);
        String heuristics = buildHeuristicsSection(heuristicFindings);
        
        return String.format(USER_PROMPT_TEMPLATE, prContext, diff, codebaseContext, heuristics);
    }
    
    /**
     * Build PR context section.
     */
    private String buildPRContext(ContextPack contextPack) {
        StringBuilder sb = new StringBuilder();
        
        if (contextPack.prTitle() != null) {
            sb.append("**Title:** ").append(contextPack.prTitle()).append("\n");
        }
        
        if (contextPack.prDescription() != null && !contextPack.prDescription().isBlank()) {
            sb.append("**Description:**\n").append(contextPack.prDescription()).append("\n");
        }
        
        if (contextPack.changedFiles() != null && !contextPack.changedFiles().isEmpty()) {
            sb.append("**Changed Files:** ").append(contextPack.changedFiles().size()).append("\n");
            for (var file : contextPack.changedFiles()) {
                sb.append("  - ").append(file.path())
                    .append(" (").append(file.changeType()).append(")")
                    .append(" +").append(file.additions())
                    .append(" -").append(file.deletions())
                    .append("\n");
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
    private String buildDiffSection(ContextPack contextPack) {
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
     */
    private String buildCodebaseContext(ContextPack contextPack) {
        if (contextPack.contextChunks() == null || contextPack.contextChunks().isEmpty()) {
            return "*No additional context retrieved*";
        }
        
        StringBuilder sb = new StringBuilder();
        
        for (var chunk : contextPack.contextChunks()) {
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
}
