package ai.aipr.server.model;

/**
 * Generic operation result.
 */
public class OperationResult {
    
    private final boolean success;
    private final String message;
    
    public OperationResult(boolean success, String message) {
        this.success = success;
        this.message = message;
    }
    
    public boolean isSuccess() { return success; }
    public String getMessage() { return message; }
    
    public static OperationResult success() {
        return new OperationResult(true, "Success");
    }
    
    public static OperationResult success(String message) {
        return new OperationResult(true, message);
    }
    
    public static OperationResult failure(String message) {
        return new OperationResult(false, message);
    }
}
