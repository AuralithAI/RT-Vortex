package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

/**
 * A heuristic finding (non-LLM issue detection).
 */
public record HeuristicFinding(
        String ruleId,
        String ruleName,
        String severity,
        String filePath,
        Integer startLine,
        Integer endLine,
        String message,
        String suggestion,
        String category
) {
    // Convenience aliases for field names used elsewhere
    public String id() { return ruleId; }
    public String rule() { return ruleName; }
    public String checkId() { return ruleId; }
    public Integer line() { return startLine; }

    @NotNull
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String ruleId;
        private String ruleName;
        private String severity = "warning";
        private String filePath;
        private Integer startLine;
        private Integer endLine;
        private String message;
        private String suggestion;
        private String category = "best_practice";

        public Builder ruleId(String ruleId) { this.ruleId = ruleId; return this; }
        public Builder ruleName(String ruleName) { this.ruleName = ruleName; return this; }
        public Builder severity(String severity) { this.severity = severity; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder startLine(Integer startLine) { this.startLine = startLine; return this; }
        public Builder endLine(Integer endLine) { this.endLine = endLine; return this; }
        public Builder line(Integer line) { this.startLine = line; return this; }
        public Builder message(String message) { this.message = message; return this; }
        public Builder suggestion(String suggestion) { this.suggestion = suggestion; return this; }
        public Builder category(String category) { this.category = category; return this; }

        public HeuristicFinding build() {
            return new HeuristicFinding(ruleId, ruleName, severity, filePath, startLine, endLine, message, suggestion, category);
        }
    }
}
