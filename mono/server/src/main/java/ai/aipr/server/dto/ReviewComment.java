package ai.aipr.server.dto;

import java.util.List;

/**
 * A review comment.
 */
public record ReviewComment(
        String id,
        String filePath,
        int line,
        Integer endLine,
        String severity,
        String category,
        String message,
        String suggestion,
        List<Reference> references,
        Double confidence,
        String source
) {
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String id;
        private String filePath;
        private int line;
        private Integer endLine;
        private String severity;
        private String category;
        private String message;
        private String suggestion;
        private List<Reference> references;
        private Double confidence;
        private String source;

        public Builder id(String id) { this.id = id; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder line(int line) { this.line = line; return this; }
        public Builder endLine(Integer endLine) { this.endLine = endLine; return this; }
        public Builder severity(String severity) { this.severity = severity; return this; }
        public Builder category(String category) { this.category = category; return this; }
        public Builder message(String message) { this.message = message; return this; }
        public Builder suggestion(String suggestion) { this.suggestion = suggestion; return this; }
        public Builder references(List<Reference> references) { this.references = references; return this; }
        public Builder confidence(Double confidence) { this.confidence = confidence; return this; }
        public Builder source(String source) { this.source = source; return this; }

        public ReviewComment build() {
            return new ReviewComment(id, filePath, line, endLine, severity, 
                    category, message, suggestion, references, confidence, source);
        }
    }

    public record Reference(String file, Integer line, String reason) {}
}
