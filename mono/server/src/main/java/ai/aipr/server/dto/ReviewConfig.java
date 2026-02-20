package ai.aipr.server.dto;

import java.util.List;

/**
 * Configuration for a review.
 */
public record ReviewConfig(
        String llmProvider,
        String llmModel,
        int maxContextTokens,
        boolean includeSecurityChecks,
        boolean includePerformanceChecks,
        boolean includeStyleChecks,
        List<String> focusAreas,
        List<String> ignorePaths,
        String reviewDepth,
        String responseLanguage,
        // Additional options for gRPC compatibility
        List<String> categories,
        String minSeverity,
        Integer maxComments,
        boolean includeSuggestions,
        boolean postToPlatform
) {
    // Canonical constructor with default values for new fields
    public ReviewConfig(
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
        this(llmProvider, llmModel, maxContextTokens, includeSecurityChecks, 
             includePerformanceChecks, includeStyleChecks, focusAreas, ignorePaths,
             reviewDepth, responseLanguage, List.of(), "info", 100, true, false);
    }
    
    public static ReviewConfig defaults() {
        return new ReviewConfig(
                null,  // No default - should be configured
                null,  // No default - should be configured
                128000,
                true,
                true,
                true,
                List.of(),
                List.of(),
                "detailed",
                "en",
                List.of(),
                "info",
                100,
                true,
                false
        );
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String llmProvider;
        private String llmModel;
        private int maxContextTokens = 128000;
        private boolean includeSecurityChecks = true;
        private boolean includePerformanceChecks = true;
        private boolean includeStyleChecks = true;
        private List<String> focusAreas = List.of();
        private List<String> ignorePaths = List.of();
        private String reviewDepth = "detailed";
        private String responseLanguage = "en";
        private List<String> categories = List.of();
        private String minSeverity = "info";
        private Integer maxComments = 100;
        private boolean includeSuggestions = true;
        private boolean postToPlatform = false;
        
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
        public Builder categories(List<String> categories) { this.categories = categories; return this; }
        public Builder minSeverity(String minSeverity) { this.minSeverity = minSeverity; return this; }
        public Builder maxComments(Integer maxComments) { this.maxComments = maxComments; return this; }
        public Builder includeSuggestions(boolean includeSuggestions) { this.includeSuggestions = includeSuggestions; return this; }
        public Builder postToPlatform(boolean postToPlatform) { this.postToPlatform = postToPlatform; return this; }
        
        public ReviewConfig build() {
            return new ReviewConfig(llmProvider, llmModel, maxContextTokens, 
                    includeSecurityChecks, includePerformanceChecks, includeStyleChecks, 
                    focusAreas, ignorePaths, reviewDepth, responseLanguage,
                    categories, minSeverity, maxComments, includeSuggestions, postToPlatform);
        }
    }
}
