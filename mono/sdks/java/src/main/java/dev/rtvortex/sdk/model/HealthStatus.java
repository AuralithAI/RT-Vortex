package dev.rtvortex.sdk.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import java.util.Map;

@JsonIgnoreProperties(ignoreUnknown = true)
public class HealthStatus {
    private String status;
    private Map<String, String> checks;
    private String time;

    public HealthStatus() {}

    public String getStatus() { return status; }
    public void setStatus(String status) { this.status = status; }
    public Map<String, String> getChecks() { return checks; }
    public void setChecks(Map<String, String> checks) { this.checks = checks; }
    public String getTime() { return time; }
    public void setTime(String time) { this.time = time; }
}
