package dev.rtvortex.sdk.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

@JsonIgnoreProperties(ignoreUnknown = true)
public class IndexStatus {
    @JsonProperty("repo_id")
    private String repoId;
    private String status;
    private int progress;
    @JsonProperty("job_id")
    private String jobId;
    @JsonProperty("started_at")
    private String startedAt;
    @JsonProperty("completed_at")
    private String completedAt;

    public IndexStatus() {}

    public String getRepoId() { return repoId; }
    public void setRepoId(String repoId) { this.repoId = repoId; }
    public String getStatus() { return status; }
    public void setStatus(String status) { this.status = status; }
    public int getProgress() { return progress; }
    public void setProgress(int progress) { this.progress = progress; }
    public String getJobId() { return jobId; }
    public void setJobId(String jobId) { this.jobId = jobId; }
    public String getStartedAt() { return startedAt; }
    public void setStartedAt(String startedAt) { this.startedAt = startedAt; }
    public String getCompletedAt() { return completedAt; }
    public void setCompletedAt(String completedAt) { this.completedAt = completedAt; }
}
