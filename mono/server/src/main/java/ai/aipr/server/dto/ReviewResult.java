package ai.aipr.server.dto;

import com.fasterxml.jackson.databind.ObjectMapper;
import java.util.List;

/**
 * Result of an LLM review.
 */
public record ReviewResult(
        String summary,
        String overallAssessment,
        List<ReviewComment> comments,
        List<String> suggestions,
        ReviewMetrics metrics
) {
    private static final ObjectMapper mapper = new ObjectMapper();
    
    public enum OverallAssessment {
        APPROVE,
        REQUEST_CHANGES,
        COMMENT
    }
    
    /**
     * Parse ReviewResult from JSON string.
     */
    public static ReviewResult fromJson(String json) throws Exception {
        return mapper.readValue(json, ReviewResult.class);
    }
    
    /**
     * Create an empty result.
     */
    public static ReviewResult empty() {
        return new ReviewResult("", "COMMENT", List.of(), List.of(), null);
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String summary;
        private String overallAssessment;
        private List<ReviewComment> comments = List.of();
        private List<String> suggestions = List.of();
        private ReviewMetrics metrics;
        
        public Builder summary(String summary) { this.summary = summary; return this; }
        public Builder overallAssessment(String overallAssessment) { this.overallAssessment = overallAssessment; return this; }
        public Builder comments(List<ReviewComment> comments) { this.comments = comments; return this; }
        public Builder suggestions(List<String> suggestions) { this.suggestions = suggestions; return this; }
        public Builder metrics(ReviewMetrics metrics) { this.metrics = metrics; return this; }
        
        public ReviewResult build() {
            return new ReviewResult(summary, overallAssessment, comments, suggestions, metrics);
        }
    }
}
