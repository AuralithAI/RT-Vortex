package ai.aipr.server.dto;

/**
 * Information about a changed file in a diff.
 */
public record FileChange(
        String path,
        String oldPath,
        ChangeType changeType,
        String language,
        int additions,
        int deletions,
        boolean isBinary
) {
    public enum ChangeType {
        ADDED,
        MODIFIED,
        DELETED,
        RENAMED,
        COPIED
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String path;
        private String oldPath;
        private ChangeType changeType = ChangeType.MODIFIED;
        private String language;
        private int additions;
        private int deletions;
        private boolean isBinary;
        
        public Builder path(String path) { this.path = path; return this; }
        public Builder oldPath(String oldPath) { this.oldPath = oldPath; return this; }
        public Builder changeType(ChangeType changeType) { this.changeType = changeType; return this; }
        public Builder language(String language) { this.language = language; return this; }
        public Builder additions(int additions) { this.additions = additions; return this; }
        public Builder deletions(int deletions) { this.deletions = deletions; return this; }
        public Builder isBinary(boolean isBinary) { this.isBinary = isBinary; return this; }
        
        public FileChange build() {
            return new FileChange(path, oldPath, changeType, language, additions, deletions, isBinary);
        }
    }
}
