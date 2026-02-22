package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * A chunk of context from the codebase.
 */
public record ContextChunk(
        String id,
        String filePath,
        int startLine,
        int endLine,
        String content,
        String language,
        List<String> symbols,
        float relevanceScore,
        ChunkSource source
) {
    public enum ChunkSource {
        VECTOR_SEARCH,
        SYMBOL_GRAPH,
        LEXICAL_SEARCH,
        FILE_HEADER,
        DIRECT_REFERENCE
    }

    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String id;
        private String filePath;
        private int startLine;
        private int endLine;
        private String content;
        private String language;
        private List<String> symbols = List.of();
        private float relevanceScore;
        private ChunkSource source = ChunkSource.VECTOR_SEARCH;

        public Builder id(String id) { this.id = id; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder startLine(int startLine) { this.startLine = startLine; return this; }
        public Builder endLine(int endLine) { this.endLine = endLine; return this; }
        public Builder content(String content) { this.content = content; return this; }
        public Builder language(String language) { this.language = language; return this; }
        public Builder symbols(List<String> symbols) { this.symbols = symbols; return this; }
        public Builder relevanceScore(float relevanceScore) { this.relevanceScore = relevanceScore; return this; }
        public Builder source(ChunkSource source) { this.source = source; return this; }

        public ContextChunk build() {
            return new ContextChunk(id, filePath, startLine, endLine, content, language, symbols, relevanceScore, source);
        }
    }
}
