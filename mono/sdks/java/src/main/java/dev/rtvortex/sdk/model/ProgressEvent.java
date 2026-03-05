package dev.rtvortex.sdk.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.Map;

@JsonIgnoreProperties(ignoreUnknown = true)
public class ProgressEvent {
    private String event;
    private String step;
    @JsonProperty("step_index")
    private int stepIndex;
    @JsonProperty("total_steps")
    private int totalSteps;
    private String status;
    private String message;
    private Map<String, Object> metadata;

    public ProgressEvent() {}

    public String getEvent() { return event; }
    public void setEvent(String event) { this.event = event; }
    public String getStep() { return step; }
    public void setStep(String step) { this.step = step; }
    public int getStepIndex() { return stepIndex; }
    public void setStepIndex(int stepIndex) { this.stepIndex = stepIndex; }
    public int getTotalSteps() { return totalSteps; }
    public void setTotalSteps(int totalSteps) { this.totalSteps = totalSteps; }
    public String getStatus() { return status; }
    public void setStatus(String status) { this.status = status; }
    public String getMessage() { return message; }
    public void setMessage(String message) { this.message = message; }
    public Map<String, Object> getMetadata() { return metadata; }
    public void setMetadata(Map<String, Object> metadata) { this.metadata = metadata; }
}
