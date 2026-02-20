package ai.aipr.server.model;

/**
 * Review comment from analysis.
 */
public record ReviewComment(
        String id,
        String filePath,
        int line,
        int endLine,
        String severity,
        String category,
        String message,
        String suggestion,
        String source,
        float confidence
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String id;
        private String filePath;
        private int line;
        private int endLine;
        private String severity;
        private String category;
        private String message;
        private String suggestion;
        private String source;
        private float confidence;
        
        public Builder id(String id) { this.id = id; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder line(int line) { this.line = line; return this; }
        public Builder endLine(int endLine) { this.endLine = endLine; return this; }
        public Builder severity(String severity) { this.severity = severity; return this; }
        public Builder category(String category) { this.category = category; return this; }
        public Builder message(String message) { this.message = message; return this; }
        public Builder suggestion(String suggestion) { this.suggestion = suggestion; return this; }
        public Builder source(String source) { this.source = source; return this; }
        public Builder confidence(float confidence) { this.confidence = confidence; return this; }
        
        public ReviewComment build() {
            return new ReviewComment(id, filePath, line, endLine, severity, category, message, suggestion, source, confidence);
        }
    }
}
