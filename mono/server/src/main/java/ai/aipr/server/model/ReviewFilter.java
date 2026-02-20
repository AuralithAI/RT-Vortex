package ai.aipr.server.model;

/**
 * Review filter criteria.
 */
public class ReviewFilter {
    
    private final String status;
    private final String repositoryId;
    private final Integer prNumber;
    private final String author;
    
    public ReviewFilter(String status, String repositoryId, Integer prNumber, String author) {
        this.status = status;
        this.repositoryId = repositoryId;
        this.prNumber = prNumber;
        this.author = author;
    }
    
    public String getStatus() { return status; }
    public String getRepositoryId() { return repositoryId; }
    public Integer getPrNumber() { return prNumber; }
    public String getAuthor() { return author; }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String status;
        private String repositoryId;
        private Integer prNumber;
        private String author;
        
        public Builder status(String status) {
            this.status = status;
            return this;
        }
        
        public Builder repositoryId(String repositoryId) {
            this.repositoryId = repositoryId;
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
            return new ReviewFilter(status, repositoryId, prNumber, author);
        }
    }
}
