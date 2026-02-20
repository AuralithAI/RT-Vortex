package ai.aipr.server.dto;

import java.util.List;

/**
 * Result of analyzing a diff.
 */
public record DiffAnalysis(
        String repoId,
        List<FileChange> changedFiles,
        List<TouchedSymbol> touchedSymbols,
        List<DiffHunk> hunks,
        int totalAdditions,
        int totalDeletions
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String repoId;
        private List<FileChange> changedFiles = List.of();
        private List<TouchedSymbol> touchedSymbols = List.of();
        private List<DiffHunk> hunks = List.of();
        private int totalAdditions;
        private int totalDeletions;
        
        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder changedFiles(List<FileChange> changedFiles) { this.changedFiles = changedFiles; return this; }
        public Builder touchedSymbols(List<TouchedSymbol> touchedSymbols) { this.touchedSymbols = touchedSymbols; return this; }
        public Builder hunks(List<DiffHunk> hunks) { this.hunks = hunks; return this; }
        public Builder totalAdditions(int totalAdditions) { this.totalAdditions = totalAdditions; return this; }
        public Builder totalDeletions(int totalDeletions) { this.totalDeletions = totalDeletions; return this; }
        
        public DiffAnalysis build() {
            return new DiffAnalysis(repoId, changedFiles, touchedSymbols, hunks, totalAdditions, totalDeletions);
        }
    }
}
