package ai.auralith.aipr.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.time.Instant;
import java.util.List;

/**
 * Response from a review request.
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public class ReviewResponse {
    
    @JsonProperty("review_id")
    private String reviewId;
    
    @JsonProperty("status")
    private String status;
    
    @JsonProperty("overall_assessment")
    private String overallAssessment;
    
    @JsonProperty("summary")
    private String summary;
    
    @JsonProperty("comments")
    private List<ReviewComment> comments;
    
    @JsonProperty("metrics")
    private ReviewMetrics metrics;
    
    @JsonProperty("created_at")
    private Instant createdAt;
    
    @JsonProperty("completed_at")
    private Instant completedAt;
    
    @JsonProperty("error")
    private String error;
    
    public ReviewResponse() {}
    
    // Getters
    public String getReviewId() { return reviewId; }
    public String getStatus() { return status; }
    public String getOverallAssessment() { return overallAssessment; }
    public String getSummary() { return summary; }
    public List<ReviewComment> getComments() { return comments; }
    public ReviewMetrics getMetrics() { return metrics; }
    public Instant getCreatedAt() { return createdAt; }
    public Instant getCompletedAt() { return completedAt; }
    public String getError() { return error; }
    
    // Setters
    public void setReviewId(String reviewId) { this.reviewId = reviewId; }
    public void setStatus(String status) { this.status = status; }
    public void setOverallAssessment(String overallAssessment) { this.overallAssessment = overallAssessment; }
    public void setSummary(String summary) { this.summary = summary; }
    public void setComments(List<ReviewComment> comments) { this.comments = comments; }
    public void setMetrics(ReviewMetrics metrics) { this.metrics = metrics; }
    public void setCreatedAt(Instant createdAt) { this.createdAt = createdAt; }
    public void setCompletedAt(Instant completedAt) { this.completedAt = completedAt; }
    public void setError(String error) { this.error = error; }
    
    /**
     * Check if the review is complete.
     */
    public boolean isComplete() {
        return "completed".equalsIgnoreCase(status) || "failed".equalsIgnoreCase(status);
    }
    
    /**
     * Check if the review was successful.
     */
    public boolean isSuccess() {
        return "completed".equalsIgnoreCase(status);
    }
    
    /**
     * A single review comment.
     */
    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class ReviewComment {
        @JsonProperty("file")
        private String file;
        
        @JsonProperty("line")
        private Integer line;
        
        @JsonProperty("end_line")
        private Integer endLine;
        
        @JsonProperty("severity")
        private String severity;
        
        @JsonProperty("category")
        private String category;
        
        @JsonProperty("message")
        private String message;
        
        @JsonProperty("suggestion")
        private String suggestion;
        
        @JsonProperty("confidence")
        private Double confidence;
        
        public ReviewComment() {}
        
        public String getFile() { return file; }
        public Integer getLine() { return line; }
        public Integer getEndLine() { return endLine; }
        public String getSeverity() { return severity; }
        public String getCategory() { return category; }
        public String getMessage() { return message; }
        public String getSuggestion() { return suggestion; }
        public Double getConfidence() { return confidence; }
        
        public void setFile(String file) { this.file = file; }
        public void setLine(Integer line) { this.line = line; }
        public void setEndLine(Integer endLine) { this.endLine = endLine; }
        public void setSeverity(String severity) { this.severity = severity; }
        public void setCategory(String category) { this.category = category; }
        public void setMessage(String message) { this.message = message; }
        public void setSuggestion(String suggestion) { this.suggestion = suggestion; }
        public void setConfidence(Double confidence) { this.confidence = confidence; }
    }
    
    /**
     * Metrics from the review.
     */
    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class ReviewMetrics {
        @JsonProperty("files_reviewed")
        private Integer filesReviewed;
        
        @JsonProperty("lines_reviewed")
        private Integer linesReviewed;
        
        @JsonProperty("total_comments")
        private Integer totalComments;
        
        @JsonProperty("critical_issues")
        private Integer criticalIssues;
        
        @JsonProperty("processing_time_ms")
        private Long processingTimeMs;
        
        @JsonProperty("llm_tokens_used")
        private Long llmTokensUsed;
        
        public ReviewMetrics() {}
        
        public Integer getFilesReviewed() { return filesReviewed; }
        public Integer getLinesReviewed() { return linesReviewed; }
        public Integer getTotalComments() { return totalComments; }
        public Integer getCriticalIssues() { return criticalIssues; }
        public Long getProcessingTimeMs() { return processingTimeMs; }
        public Long getLlmTokensUsed() { return llmTokensUsed; }
        
        public void setFilesReviewed(Integer filesReviewed) { this.filesReviewed = filesReviewed; }
        public void setLinesReviewed(Integer linesReviewed) { this.linesReviewed = linesReviewed; }
        public void setTotalComments(Integer totalComments) { this.totalComments = totalComments; }
        public void setCriticalIssues(Integer criticalIssues) { this.criticalIssues = criticalIssues; }
        public void setProcessingTimeMs(Long processingTimeMs) { this.processingTimeMs = processingTimeMs; }
        public void setLlmTokensUsed(Long llmTokensUsed) { this.llmTokensUsed = llmTokensUsed; }
    }
}
