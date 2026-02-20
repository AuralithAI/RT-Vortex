package ai.aipr.server.dto;

import java.util.List;

/**
 * A code chunk for indexing.
 */
public record Chunk(
        String id,
        String filePath,
        int startLine,
        int endLine,
        String content,
        String contentHash,
        String language,
        String parentSymbol,
        List<String> symbols,
        List<String> imports
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String id;
        private String filePath;
        private int startLine;
        private int endLine;
        private String content;
        private String contentHash;
        private String language;
        private String parentSymbol;
        private List<String> symbols = List.of();
        private List<String> imports = List.of();
        
        public Builder id(String id) { this.id = id; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder startLine(int startLine) { this.startLine = startLine; return this; }
        public Builder endLine(int endLine) { this.endLine = endLine; return this; }
        public Builder content(String content) { this.content = content; return this; }
        public Builder contentHash(String contentHash) { this.contentHash = contentHash; return this; }
        public Builder language(String language) { this.language = language; return this; }
        public Builder parentSymbol(String parentSymbol) { this.parentSymbol = parentSymbol; return this; }
        public Builder symbols(List<String> symbols) { this.symbols = symbols; return this; }
        public Builder imports(List<String> imports) { this.imports = imports; return this; }
        
        public Chunk build() {
            return new Chunk(id, filePath, startLine, endLine, content, contentHash, language, parentSymbol, symbols, imports);
        }
    }
}
