package ai.aipr.server.model;

import java.util.List;

/**
 * LLM connection test result.
 */
public class LLMTestResult {
    
    private final boolean success;
    private final String message;
    private final int latencyMs;
    private final List<String> availableModels;
    
    public LLMTestResult(boolean success, String message, int latencyMs, 
                         List<String> availableModels) {
        this.success = success;
        this.message = message;
        this.latencyMs = latencyMs;
        this.availableModels = availableModels;
    }
    
    public boolean isSuccess() { return success; }
    public String getMessage() { return message; }
    public int getLatencyMs() { return latencyMs; }
    public List<String> getAvailableModels() { return availableModels; }
}
