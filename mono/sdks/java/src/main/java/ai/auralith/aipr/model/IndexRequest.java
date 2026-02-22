package ai.auralith.aipr.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/**
 * Request to index a repository.
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public class IndexRequest {
    
    @JsonProperty("repository_url")
    private String repositoryUrl;
    
    @JsonProperty("branch")
    private String branch;
    
    @JsonProperty("commit_sha")
    private String commitSha;
    
    @JsonProperty("include_patterns")
    private List<String> includePatterns;
    
    @JsonProperty("exclude_patterns")
    private List<String> excludePatterns;
    
    @JsonProperty("force_reindex")
    private Boolean forceReindex;
    
    public IndexRequest() {}
    
    private IndexRequest(Builder builder) {
        this.repositoryUrl = builder.repositoryUrl;
        this.branch = builder.branch;
        this.commitSha = builder.commitSha;
        this.includePatterns = builder.includePatterns;
        this.excludePatterns = builder.excludePatterns;
        this.forceReindex = builder.forceReindex;
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    // Getters
    public String getRepositoryUrl() { return repositoryUrl; }
    public String getBranch() { return branch; }
    public String getCommitSha() { return commitSha; }
    public List<String> getIncludePatterns() { return includePatterns; }
    public List<String> getExcludePatterns() { return excludePatterns; }
    public Boolean getForceReindex() { return forceReindex; }
    
    public static class Builder {
        private String repositoryUrl;
        private String branch;  // No default - must be provided by user/UI
        private String commitSha;
        private List<String> includePatterns;
        private List<String> excludePatterns;
        private Boolean forceReindex = false;
        
        public Builder repositoryUrl(String repositoryUrl) {
            this.repositoryUrl = repositoryUrl;
            return this;
        }
        
        public Builder branch(String branch) {
            this.branch = branch;
            return this;
        }
        
        public Builder commitSha(String commitSha) {
            this.commitSha = commitSha;
            return this;
        }
        
        public Builder includePatterns(List<String> includePatterns) {
            this.includePatterns = includePatterns;
            return this;
        }
        
        public Builder excludePatterns(List<String> excludePatterns) {
            this.excludePatterns = excludePatterns;
            return this;
        }
        
        public Builder forceReindex(Boolean forceReindex) {
            this.forceReindex = forceReindex;
            return this;
        }
        
        public IndexRequest build() {
            return new IndexRequest(this);
        }
    }
}
