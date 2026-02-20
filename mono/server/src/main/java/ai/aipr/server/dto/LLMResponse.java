package ai.aipr.server.dto;

/**
 * LLM completion response.
 */
public record LLMResponse(
        String content,
        String model,
        int promptTokens,
        int completionTokens,
        int totalTokens,
        String finishReason,
        long latencyMs
) {
    /**
     * Convenience method for total tokens used.
     */
    public int tokensUsed() {
        return totalTokens > 0 ? totalTokens : promptTokens + completionTokens;
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String content;
        private String model;
        private int promptTokens;
        private int completionTokens;
        private int totalTokens;
        private String finishReason;
        private long latencyMs;
        
        public Builder content(String content) { this.content = content; return this; }
        public Builder model(String model) { this.model = model; return this; }
        public Builder promptTokens(int promptTokens) { this.promptTokens = promptTokens; return this; }
        public Builder completionTokens(int completionTokens) { this.completionTokens = completionTokens; return this; }
        public Builder totalTokens(int totalTokens) { this.totalTokens = totalTokens; return this; }
        public Builder tokensUsed(int tokensUsed) { this.totalTokens = tokensUsed; return this; }
        public Builder finishReason(String finishReason) { this.finishReason = finishReason; return this; }
        public Builder latencyMs(long latencyMs) { this.latencyMs = latencyMs; return this; }
        
        public LLMResponse build() {
            return new LLMResponse(content, model, promptTokens, completionTokens, totalTokens, finishReason, latencyMs);
        }
    }
}
