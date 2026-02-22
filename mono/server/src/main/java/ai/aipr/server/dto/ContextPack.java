package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * Context pack for LLM review.
 */
public record ContextPack(
        String repoId,
        String prTitle,
        String prDescription,
        String diff,
        List<FileChange> changedFiles,
        List<ContextChunk> contextChunks,
        List<Chunk> chunks,
        List<TouchedSymbol> touchedSymbols,
        List<String> heuristicWarnings,
        int totalTokens
) {
    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String repoId;
        private String prTitle;
        private String prDescription;
        private String diff;
        private List<FileChange> changedFiles = List.of();
        private List<ContextChunk> contextChunks = List.of();
        private List<Chunk> chunks = List.of();
        private List<TouchedSymbol> touchedSymbols = List.of();
        private List<String> heuristicWarnings = List.of();
        private int totalTokens;

        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder prTitle(String prTitle) { this.prTitle = prTitle; return this; }
        public Builder prDescription(String prDescription) { this.prDescription = prDescription; return this; }
        public Builder diff(String diff) { this.diff = diff; return this; }
        public Builder diff(@NotNull DiffAnalysis diffAnalysis) {
            this.changedFiles = diffAnalysis.changedFiles();
            this.touchedSymbols = diffAnalysis.touchedSymbols();
            return this;
        }
        public Builder changedFiles(List<FileChange> changedFiles) { this.changedFiles = changedFiles; return this; }
        public Builder contextChunks(List<ContextChunk> contextChunks) { this.contextChunks = contextChunks; return this; }
        public Builder chunks(List<Chunk> chunks) { this.chunks = chunks; return this; }
        public Builder touchedSymbols(List<TouchedSymbol> touchedSymbols) { this.touchedSymbols = touchedSymbols; return this; }
        public Builder heuristicWarnings(List<String> heuristicWarnings) { this.heuristicWarnings = heuristicWarnings; return this; }
        public Builder totalTokens(int totalTokens) { this.totalTokens = totalTokens; return this; }

        public ContextPack build() {
            return new ContextPack(repoId, prTitle, prDescription, diff, changedFiles, contextChunks, chunks, touchedSymbols, heuristicWarnings, totalTokens);
        }
    }
}
