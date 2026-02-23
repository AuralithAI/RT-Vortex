package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

/**
 * Metrics from a review.
 * Contains both analysis scores and quantitative metrics.
 */
public record ReviewMetrics(
        // Score-based metrics (0.0 - 1.0 or 0 - 100)
        Double securityScore,
        Double reliabilityScore,
        Double performanceScore,
        Double testingScore,
        Double documentationScore,
        Double overallScore,
        // Quantitative metrics
        Integer filesAnalyzed,
        Integer linesAdded,
        Integer linesRemoved,
        Integer totalFindings,
        Integer tokensUsed,
        Integer promptTokens,
        Integer completionTokens,
        Integer latencyMs,
        Integer llmLatencyMs
) {

    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private Double securityScore;
        private Double reliabilityScore;
        private Double performanceScore;
        private Double testingScore;
        private Double documentationScore;
        private Double overallScore;
        private Integer filesAnalyzed;
        private Integer linesAdded;
        private Integer linesRemoved;
        private Integer totalFindings;
        private Integer tokensUsed;
        private Integer promptTokens;
        private Integer completionTokens;
        private Integer latencyMs;
        private Integer llmLatencyMs;

        public Builder securityScore(Double v) { this.securityScore = v; return this; }
        public Builder reliabilityScore(Double v) { this.reliabilityScore = v; return this; }
        public Builder performanceScore(Double v) { this.performanceScore = v; return this; }
        public Builder testingScore(Double v) { this.testingScore = v; return this; }
        public Builder documentationScore(Double v) { this.documentationScore = v; return this; }
        public Builder overallScore(Double v) { this.overallScore = v; return this; }
        public Builder filesAnalyzed(Integer v) { this.filesAnalyzed = v; return this; }
        public Builder linesAdded(Integer v) { this.linesAdded = v; return this; }
        public Builder linesRemoved(Integer v) { this.linesRemoved = v; return this; }
        public Builder totalFindings(Integer v) { this.totalFindings = v; return this; }
        public Builder tokensUsed(Integer v) { this.tokensUsed = v; return this; }
        public Builder promptTokens(Integer v) { this.promptTokens = v; return this; }
        public Builder completionTokens(Integer v) { this.completionTokens = v; return this; }
        public Builder latencyMs(Integer v) { this.latencyMs = v; return this; }
        public Builder llmLatencyMs(Integer v) { this.llmLatencyMs = v; return this; }

        public ReviewMetrics build() {
            return new ReviewMetrics(securityScore, reliabilityScore, performanceScore,
                    testingScore, documentationScore, overallScore,
                    filesAnalyzed, linesAdded, linesRemoved, totalFindings,
                    tokensUsed, promptTokens, completionTokens, latencyMs, llmLatencyMs);
        }
    }
}
