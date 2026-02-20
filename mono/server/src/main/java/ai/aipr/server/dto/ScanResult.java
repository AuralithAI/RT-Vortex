package ai.aipr.server.dto;

import java.util.List;

/**
 * Result of scanning a repository.
 */
public record ScanResult(
        String repoId,
        List<FileInfo> files,
        List<Symbol> symbols,
        int totalFiles,
        long totalSizeBytes,
        List<String> languages
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String repoId;
        private List<FileInfo> files = List.of();
        private List<Symbol> symbols = List.of();
        private int totalFiles;
        private long totalSizeBytes;
        private List<String> languages = List.of();
        
        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder files(List<FileInfo> files) { this.files = files; return this; }
        public Builder symbols(List<Symbol> symbols) { this.symbols = symbols; return this; }
        public Builder totalFiles(int totalFiles) { this.totalFiles = totalFiles; return this; }
        public Builder totalSizeBytes(long totalSizeBytes) { this.totalSizeBytes = totalSizeBytes; return this; }
        public Builder languages(List<String> languages) { this.languages = languages; return this; }
        
        public ScanResult build() {
            return new ScanResult(repoId, files, symbols, totalFiles, totalSizeBytes, languages);
        }
    }
}
