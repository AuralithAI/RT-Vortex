package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * A code symbol for the symbol graph.
 */
public record Symbol(
        String name,
        String qualifiedName,
        TouchedSymbol.SymbolKind kind,
        String filePath,
        int startLine,
        int endLine,
        String signature,
        String docComment,
        List<String> callers,
        List<String> callees
) {
    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String name;
        private String qualifiedName;
        private TouchedSymbol.SymbolKind kind = TouchedSymbol.SymbolKind.FUNCTION;
        private String filePath;
        private int startLine;
        private int endLine;
        private String signature;
        private String docComment;
        private List<String> callers = List.of();
        private List<String> callees = List.of();

        public Builder name(String name) { this.name = name; return this; }
        public Builder qualifiedName(String qualifiedName) { this.qualifiedName = qualifiedName; return this; }
        public Builder kind(TouchedSymbol.SymbolKind kind) { this.kind = kind; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder startLine(int startLine) { this.startLine = startLine; return this; }
        public Builder endLine(int endLine) { this.endLine = endLine; return this; }
        public Builder signature(String signature) { this.signature = signature; return this; }
        public Builder docComment(String docComment) { this.docComment = docComment; return this; }
        public Builder callers(List<String> callers) { this.callers = callers; return this; }
        public Builder callees(List<String> callees) { this.callees = callees; return this; }

        public Symbol build() {
            return new Symbol(name, qualifiedName, kind, filePath, startLine, endLine, signature, docComment, callers, callees);
        }
    }
}
