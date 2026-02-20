package ai.auralith.aipr.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;
import java.util.Map;
import java.util.Objects;

/**
 * Request to submit a pull request for review.
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public class ReviewRequest {
    
    @JsonProperty("repository_url")
    private String repositoryUrl;
    
    @JsonProperty("pull_request_id")
    private Integer pullRequestId;
    
    @JsonProperty("base_sha")
    private String baseSha;
    
    @JsonProperty("head_sha")
    private String headSha;
    
    @JsonProperty("diff_content")
    private String diffContent;
    
    @JsonProperty("files")
    private List<FileChange> files;
    
    @JsonProperty("context")
    private ReviewContext context;
    
    @JsonProperty("config")
    private Map<String, Object> config;
    
    // Default constructor for Jackson
    public ReviewRequest() {}
    
    private ReviewRequest(Builder builder) {
        this.repositoryUrl = builder.repositoryUrl;
        this.pullRequestId = builder.pullRequestId;
        this.baseSha = builder.baseSha;
        this.headSha = builder.headSha;
        this.diffContent = builder.diffContent;
        this.files = builder.files;
        this.context = builder.context;
        this.config = builder.config;
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    // Getters
    public String getRepositoryUrl() { return repositoryUrl; }
    public Integer getPullRequestId() { return pullRequestId; }
    public String getBaseSha() { return baseSha; }
    public String getHeadSha() { return headSha; }
    public String getDiffContent() { return diffContent; }
    public List<FileChange> getFiles() { return files; }
    public ReviewContext getContext() { return context; }
    public Map<String, Object> getConfig() { return config; }
    
    public static class Builder {
        private String repositoryUrl;
        private Integer pullRequestId;
        private String baseSha;
        private String headSha;
        private String diffContent;
        private List<FileChange> files;
        private ReviewContext context;
        private Map<String, Object> config;
        
        public Builder repositoryUrl(String repositoryUrl) {
            this.repositoryUrl = repositoryUrl;
            return this;
        }
        
        public Builder pullRequestId(Integer pullRequestId) {
            this.pullRequestId = pullRequestId;
            return this;
        }
        
        public Builder baseSha(String baseSha) {
            this.baseSha = baseSha;
            return this;
        }
        
        public Builder headSha(String headSha) {
            this.headSha = headSha;
            return this;
        }
        
        public Builder diffContent(String diffContent) {
            this.diffContent = diffContent;
            return this;
        }
        
        public Builder files(List<FileChange> files) {
            this.files = files;
            return this;
        }
        
        public Builder context(ReviewContext context) {
            this.context = context;
            return this;
        }
        
        public Builder config(Map<String, Object> config) {
            this.config = config;
            return this;
        }
        
        public ReviewRequest build() {
            return new ReviewRequest(this);
        }
    }
    
    /**
     * Represents a file change in the pull request.
     */
    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class FileChange {
        @JsonProperty("path")
        private String path;
        
        @JsonProperty("status")
        private String status;
        
        @JsonProperty("additions")
        private Integer additions;
        
        @JsonProperty("deletions")
        private Integer deletions;
        
        @JsonProperty("patch")
        private String patch;
        
        public FileChange() {}
        
        public String getPath() { return path; }
        public String getStatus() { return status; }
        public Integer getAdditions() { return additions; }
        public Integer getDeletions() { return deletions; }
        public String getPatch() { return patch; }
        
        public void setPath(String path) { this.path = path; }
        public void setStatus(String status) { this.status = status; }
        public void setAdditions(Integer additions) { this.additions = additions; }
        public void setDeletions(Integer deletions) { this.deletions = deletions; }
        public void setPatch(String patch) { this.patch = patch; }
    }
    
    /**
     * Additional context for the review.
     */
    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class ReviewContext {
        @JsonProperty("pr_title")
        private String prTitle;
        
        @JsonProperty("pr_description")
        private String prDescription;
        
        @JsonProperty("author")
        private String author;
        
        @JsonProperty("labels")
        private List<String> labels;
        
        public ReviewContext() {}
        
        public String getPrTitle() { return prTitle; }
        public String getPrDescription() { return prDescription; }
        public String getAuthor() { return author; }
        public List<String> getLabels() { return labels; }
        
        public void setPrTitle(String prTitle) { this.prTitle = prTitle; }
        public void setPrDescription(String prDescription) { this.prDescription = prDescription; }
        public void setAuthor(String author) { this.author = author; }
        public void setLabels(List<String> labels) { this.labels = labels; }
    }
}
