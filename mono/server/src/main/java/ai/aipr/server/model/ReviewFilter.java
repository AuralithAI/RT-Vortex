package ai.aipr.server.model;

/**
 * Review filter criteria.
 */
public record ReviewFilter(
        String status,
        String repoId,
        Integer prNumber,
        String author
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String status;
        private String repoId;
        private Integer prNumber;
        private String author;
        
        public Builder status(String status) {
            this.status = status;
            return this;
        }
        
        public Builder repoId(String repoId) {
            this.repoId = repoId;
            return this;
        }
        
        public Builder prNumber(Integer prNumber) {
            this.prNumber = prNumber;
            return this;
        }
        
        public Builder author(String author) {
            this.author = author;
            return this;
        }
        
        public ReviewFilter build() {
            return new ReviewFilter(status, repoId, prNumber, author);
        }
    }
}
