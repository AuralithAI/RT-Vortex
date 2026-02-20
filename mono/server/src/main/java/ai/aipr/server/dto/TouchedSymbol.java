package ai.aipr.server.dto;

import java.util.List;

/**
 * A symbol (function, class, etc.) that was touched by a change.
 */
public record TouchedSymbol(
        String name,
        String qualifiedName,
        SymbolKind kind,
        String filePath,
        int startLine,
        int endLine,
        ChangeType changeType,
        List<String> callers,
        List<String> callees
) {
    public enum SymbolKind {
        FUNCTION,
        METHOD,
        CLASS,
        INTERFACE,
        ENUM,
        STRUCT,
        VARIABLE,
        CONSTANT,
        MODULE,
        NAMESPACE,
        PROPERTY,
        FIELD
    }
    
    public enum ChangeType {
        ADDED,
        MODIFIED,
        DELETED
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String name;
        private String qualifiedName;
        private SymbolKind kind = SymbolKind.FUNCTION;
        private String filePath;
        private int startLine;
        private int endLine;
        private ChangeType changeType = ChangeType.MODIFIED;
        private List<String> callers = List.of();
        private List<String> callees = List.of();
        
        public Builder name(String name) { this.name = name; return this; }
        public Builder qualifiedName(String qualifiedName) { this.qualifiedName = qualifiedName; return this; }
        public Builder kind(SymbolKind kind) { this.kind = kind; return this; }
        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder startLine(int startLine) { this.startLine = startLine; return this; }
        public Builder endLine(int endLine) { this.endLine = endLine; return this; }
        public Builder changeType(ChangeType changeType) { this.changeType = changeType; return this; }
        public Builder callers(List<String> callers) { this.callers = callers; return this; }
        public Builder callees(List<String> callees) { this.callees = callees; return this; }
        
        public TouchedSymbol build() {
            return new TouchedSymbol(name, qualifiedName, kind, filePath, startLine, endLine, changeType, callers, callees);
        }
    }
}
