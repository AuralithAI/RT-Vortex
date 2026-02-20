package ai.aipr.server.dto;

/**
 * Statistics from an indexing operation.
 */
public record IndexStats(
        int totalFiles,
        int indexedFiles,
        int skippedFiles,
        int totalChunks,
        int totalSymbols,
        long totalSizeBytes,
        long durationMs
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private int totalFiles;
        private int indexedFiles;
        private int skippedFiles;
        private int totalChunks;
        private int totalSymbols;
        private long totalSizeBytes;
        private long durationMs;
        
        public Builder totalFiles(int totalFiles) { this.totalFiles = totalFiles; return this; }
        public Builder indexedFiles(int indexedFiles) { this.indexedFiles = indexedFiles; return this; }
        public Builder skippedFiles(int skippedFiles) { this.skippedFiles = skippedFiles; return this; }
        public Builder totalChunks(int totalChunks) { this.totalChunks = totalChunks; return this; }
        public Builder chunksCreated(int chunksCreated) { this.totalChunks = chunksCreated; return this; }
        public Builder totalSymbols(int totalSymbols) { this.totalSymbols = totalSymbols; return this; }
        public Builder totalSizeBytes(long totalSizeBytes) { this.totalSizeBytes = totalSizeBytes; return this; }
        public Builder durationMs(long durationMs) { this.durationMs = durationMs; return this; }
        
        public IndexStats build() {
            return new IndexStats(totalFiles, indexedFiles, skippedFiles, totalChunks, totalSymbols, totalSizeBytes, durationMs);
        }
    }
}
