package ai.auralith.aipr.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.time.Instant;

/**
 * Response from an index request.
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public class IndexResponse {
    
    @JsonProperty("job_id")
    private String jobId;
    
    @JsonProperty("status")
    private String status;
    
    @JsonProperty("repository_url")
    private String repositoryUrl;
    
    @JsonProperty("branch")
    private String branch;
    
    @JsonProperty("commit_sha")
    private String commitSha;
    
    @JsonProperty("files_indexed")
    private Integer filesIndexed;
    
    @JsonProperty("chunks_created")
    private Integer chunksCreated;
    
    @JsonProperty("progress_percent")
    private Double progressPercent;
    
    @JsonProperty("created_at")
    private Instant createdAt;
    
    @JsonProperty("completed_at")
    private Instant completedAt;
    
    @JsonProperty("error")
    private String error;
    
    public IndexResponse() {}
    
    // Getters
    public String getJobId() { return jobId; }
    public String getStatus() { return status; }
    public String getRepositoryUrl() { return repositoryUrl; }
    public String getBranch() { return branch; }
    public String getCommitSha() { return commitSha; }
    public Integer getFilesIndexed() { return filesIndexed; }
    public Integer getChunksCreated() { return chunksCreated; }
    public Double getProgressPercent() { return progressPercent; }
    public Instant getCreatedAt() { return createdAt; }
    public Instant getCompletedAt() { return completedAt; }
    public String getError() { return error; }
    
    // Setters
    public void setJobId(String jobId) { this.jobId = jobId; }
    public void setStatus(String status) { this.status = status; }
    public void setRepositoryUrl(String repositoryUrl) { this.repositoryUrl = repositoryUrl; }
    public void setBranch(String branch) { this.branch = branch; }
    public void setCommitSha(String commitSha) { this.commitSha = commitSha; }
    public void setFilesIndexed(Integer filesIndexed) { this.filesIndexed = filesIndexed; }
    public void setChunksCreated(Integer chunksCreated) { this.chunksCreated = chunksCreated; }
    public void setProgressPercent(Double progressPercent) { this.progressPercent = progressPercent; }
    public void setCreatedAt(Instant createdAt) { this.createdAt = createdAt; }
    public void setCompletedAt(Instant completedAt) { this.completedAt = completedAt; }
    public void setError(String error) { this.error = error; }
    
    /**
     * Check if the indexing job is complete.
     */
    public boolean isComplete() {
        return "completed".equalsIgnoreCase(status) || "failed".equalsIgnoreCase(status);
    }
    
    /**
     * Check if the indexing job was successful.
     */
    public boolean isSuccess() {
        return "completed".equalsIgnoreCase(status);
    }
}
