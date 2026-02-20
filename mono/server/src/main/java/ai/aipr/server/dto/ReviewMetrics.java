package ai.aipr.server.dto;

/**
 * Metrics from a review.
 */
public record ReviewMetrics(
        Double securityScore,
        Double reliabilityScore,
        Double performanceScore,
        Double testingScore,
        Double documentationScore,
        Double overallScore
) {
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private Double securityScore;
        private Double reliabilityScore;
        private Double performanceScore;
        private Double testingScore;
        private Double documentationScore;
        private Double overallScore;

        public Builder securityScore(Double v) { this.securityScore = v; return this; }
        public Builder reliabilityScore(Double v) { this.reliabilityScore = v; return this; }
        public Builder performanceScore(Double v) { this.performanceScore = v; return this; }
        public Builder testingScore(Double v) { this.testingScore = v; return this; }
        public Builder documentationScore(Double v) { this.documentationScore = v; return this; }
        public Builder overallScore(Double v) { this.overallScore = v; return this; }

        public ReviewMetrics build() {
            return new ReviewMetrics(securityScore, reliabilityScore, performanceScore,
                    testingScore, documentationScore, overallScore);
        }
    }
}
