package ai.aipr.server.dto;

import java.util.List;

/**
 * Configuration for indexing operations.
 * 
 * <p>The embedding model should be configured by the user through the UI or
 * application configuration. No default model is assumed.</p>
 */
public record IndexConfig(
        List<String> includePatterns,
        List<String> excludePatterns,
        int maxFileSizeBytes,
        boolean indexBinaries,
        boolean extractSymbols,
        boolean generateEmbeddings,
        String embeddingModel,
        int chunkSizeTokens,
        int chunkOverlapTokens
) {
    /**
     * Create a configuration with default values except embedding model.
     * The embedding model must be provided separately.
     */
    public static IndexConfig defaultsWithModel(String embeddingModel) {
        return new IndexConfig(
                List.of("**/*.java", "**/*.kt", "**/*.py", "**/*.js", "**/*.ts", "**/*.cpp", "**/*.c", "**/*.h", "**/*.go", "**/*.rs"),
                List.of("**/node_modules/**", "**/build/**", "**/target/**", "**/.git/**", "**/vendor/**"),
                1024 * 1024, // 1MB
                false,
                true,
                true,
                embeddingModel,
                512,
                64
        );
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private List<String> includePatterns = List.of();
        private List<String> excludePatterns = List.of();
        private int maxFileSizeBytes = 1024 * 1024;
        private boolean indexBinaries = false;
        private boolean extractSymbols = true;
        private boolean generateEmbeddings = true;
        private String embeddingModel;  // No default - must be configured
        private int chunkSizeTokens = 512;
        private int chunkOverlapTokens = 64;
        
        public Builder includePatterns(List<String> patterns) { this.includePatterns = patterns; return this; }
        public Builder excludePatterns(List<String> patterns) { this.excludePatterns = patterns; return this; }
        public Builder maxFileSizeBytes(int size) { this.maxFileSizeBytes = size; return this; }
        public Builder indexBinaries(boolean value) { this.indexBinaries = value; return this; }
        public Builder extractSymbols(boolean value) { this.extractSymbols = value; return this; }
        public Builder generateEmbeddings(boolean value) { this.generateEmbeddings = value; return this; }
        public Builder embeddingModel(String model) { this.embeddingModel = model; return this; }
        public Builder chunkSizeTokens(int size) { this.chunkSizeTokens = size; return this; }
        public Builder chunkOverlapTokens(int overlap) { this.chunkOverlapTokens = overlap; return this; }
        
        public IndexConfig build() {
            return new IndexConfig(includePatterns, excludePatterns, maxFileSizeBytes, indexBinaries, extractSymbols, generateEmbeddings, embeddingModel, chunkSizeTokens, chunkOverlapTokens);
        }
    }
}
