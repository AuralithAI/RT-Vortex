package dev.rtvortex.sdk.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.Map;

@JsonIgnoreProperties(ignoreUnknown = true)
public class Review {
    private String id;
    @JsonProperty("repo_id")
    private String repoId;
    @JsonProperty("pr_number")
    private int prNumber;
    private String status;
    @JsonProperty("comments_count")
    private int commentsCount;
    @JsonProperty("current_step")
    private String currentStep;
    @JsonProperty("total_steps")
    private Integer totalSteps;
    @JsonProperty("steps_completed")
    private int stepsCompleted;
    @JsonProperty("created_at")
    private String createdAt;
    @JsonProperty("completed_at")
    private String completedAt;
    private Map<String, Object> metadata;

    public Review() {}

    public String getId() { return id; }
    public void setId(String id) { this.id = id; }
    public String getRepoId() { return repoId; }
    public void setRepoId(String repoId) { this.repoId = repoId; }
    public int getPrNumber() { return prNumber; }
    public void setPrNumber(int prNumber) { this.prNumber = prNumber; }
    public String getStatus() { return status; }
    public void setStatus(String status) { this.status = status; }
    public int getCommentsCount() { return commentsCount; }
    public void setCommentsCount(int commentsCount) { this.commentsCount = commentsCount; }
    public String getCurrentStep() { return currentStep; }
    public void setCurrentStep(String currentStep) { this.currentStep = currentStep; }
    public Integer getTotalSteps() { return totalSteps; }
    public void setTotalSteps(Integer totalSteps) { this.totalSteps = totalSteps; }
    public int getStepsCompleted() { return stepsCompleted; }
    public void setStepsCompleted(int stepsCompleted) { this.stepsCompleted = stepsCompleted; }
    public String getCreatedAt() { return createdAt; }
    public void setCreatedAt(String createdAt) { this.createdAt = createdAt; }
    public String getCompletedAt() { return completedAt; }
    public void setCompletedAt(String completedAt) { this.completedAt = completedAt; }
    public Map<String, Object> getMetadata() { return metadata; }
    public void setMetadata(Map<String, Object> metadata) { this.metadata = metadata; }
}
