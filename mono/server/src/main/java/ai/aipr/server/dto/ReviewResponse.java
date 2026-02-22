package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * Response from a PR review.
 */
public record ReviewResponse(
        String reviewId,
        String repoId,
        Integer prNumber,
        String status,
        String summary,
        String overallAssessment,
        List<ReviewComment> comments,
        List<String> suggestions,
        ReviewMetrics metrics,
        ReviewMetadata metadata
) {
    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String reviewId;
        private String repoId;
        private Integer prNumber;
        private String status;
        private String summary;
        private String overallAssessment;
        private List<ReviewComment> comments = List.of();
        private List<String> suggestions = List.of();
        private ReviewMetrics metrics;
        private ReviewMetadata metadata;

        public Builder reviewId(String reviewId) { this.reviewId = reviewId; return this; }
        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder prNumber(Integer prNumber) { this.prNumber = prNumber; return this; }
        public Builder status(String status) { this.status = status; return this; }
        public Builder summary(String summary) { this.summary = summary; return this; }
        public Builder overallAssessment(String overallAssessment) { this.overallAssessment = overallAssessment; return this; }
        public Builder comments(List<ReviewComment> comments) { this.comments = comments; return this; }
        public Builder suggestions(List<String> suggestions) { this.suggestions = suggestions; return this; }
        public Builder metrics(ReviewMetrics metrics) { this.metrics = metrics; return this; }
        public Builder metadata(ReviewMetadata metadata) { this.metadata = metadata; return this; }

        public ReviewResponse build() {
            return new ReviewResponse(reviewId, repoId, prNumber, status, summary,
                    overallAssessment, comments, suggestions, metrics, metadata);
        }
    }
}
