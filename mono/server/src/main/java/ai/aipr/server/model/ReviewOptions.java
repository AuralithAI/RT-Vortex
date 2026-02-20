package ai.aipr.server.model;

import java.util.List;

/**
 * Options for configuring a PR review.
 */
public record ReviewOptions(
        String llmProvider,
        String llmModel,
        int maxContextTokens,
        boolean includeSecurityChecks,
        boolean includePerformanceChecks,
        boolean includeStyleChecks,
        List<String> focusAreas,
        List<String> ignorePaths,
        String reviewDepth,
        String responseLanguage
) {
    public static ReviewOptions defaults() {
        return new ReviewOptions(
                "openai",
                "gpt-4-turbo-preview",
                128000,
                true,
                true,
                true,
                List.of(),
                List.of(),
                "detailed",
                "en"
        );
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String llmProvider = "openai";
        private String llmModel = "gpt-4-turbo-preview";
        private int maxContextTokens = 128000;
        private boolean includeSecurityChecks = true;
        private boolean includePerformanceChecks = true;
        private boolean includeStyleChecks = true;
        private List<String> focusAreas = List.of();
        private List<String> ignorePaths = List.of();
        private String reviewDepth = "detailed";
        private String responseLanguage = "en";
        
        public Builder llmProvider(String llmProvider) { this.llmProvider = llmProvider; return this; }
        public Builder llmModel(String llmModel) { this.llmModel = llmModel; return this; }
        public Builder maxContextTokens(int maxContextTokens) { this.maxContextTokens = maxContextTokens; return this; }
        public Builder includeSecurityChecks(boolean includeSecurityChecks) { this.includeSecurityChecks = includeSecurityChecks; return this; }
        public Builder includePerformanceChecks(boolean includePerformanceChecks) { this.includePerformanceChecks = includePerformanceChecks; return this; }
        public Builder includeStyleChecks(boolean includeStyleChecks) { this.includeStyleChecks = includeStyleChecks; return this; }
        public Builder focusAreas(List<String> focusAreas) { this.focusAreas = focusAreas; return this; }
        public Builder ignorePaths(List<String> ignorePaths) { this.ignorePaths = ignorePaths; return this; }
        public Builder reviewDepth(String reviewDepth) { this.reviewDepth = reviewDepth; return this; }
        public Builder responseLanguage(String responseLanguage) { this.responseLanguage = responseLanguage; return this; }
        
        public ReviewOptions build() {
            return new ReviewOptions(llmProvider, llmModel, maxContextTokens, includeSecurityChecks, 
                    includePerformanceChecks, includeStyleChecks, focusAreas, ignorePaths, reviewDepth, responseLanguage);
        }
    }
}
