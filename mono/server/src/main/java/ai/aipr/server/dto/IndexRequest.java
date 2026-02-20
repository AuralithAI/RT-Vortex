package ai.aipr.server.dto;

import jakarta.validation.constraints.NotBlank;

/**
 * Request to index a repository.
 */
public record IndexRequest(
        @NotBlank(message = "Repository ID is required")
        String repoId,
        
        String branch,
        
        String commitSha,
        
        String sinceCommit,
        
        IndexConfig config
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String repoId;
        private String branch = "main";
        private String commitSha;
        private String sinceCommit;
        private IndexConfig config;
        
        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder branch(String branch) { this.branch = branch; return this; }
        public Builder commitSha(String commitSha) { this.commitSha = commitSha; return this; }
        public Builder sinceCommit(String sinceCommit) { this.sinceCommit = sinceCommit; return this; }
        public Builder config(IndexConfig config) { this.config = config; return this; }
        
        public IndexRequest build() {
            return new IndexRequest(repoId, branch, commitSha, sinceCommit, config);
        }
    }
}
