package ai.auralith.aipr;

/**
 * Exception thrown when an API request fails.
 */
public class AIPRException extends Exception {
    private final int statusCode;
    
    public AIPRException(String message) {
        super(message);
        this.statusCode = -1;
    }
    
    public AIPRException(String message, int statusCode) {
        super(message);
        this.statusCode = statusCode;
    }
    
    public AIPRException(String message, Throwable cause) {
        super(message, cause);
        this.statusCode = -1;
    }
    
    public AIPRException(String message, Throwable cause, int statusCode) {
        super(message, cause);
        this.statusCode = statusCode;
    }
    
    /**
     * Get the HTTP status code if available, -1 otherwise.
     */
    public int getStatusCode() {
        return statusCode;
    }
    
    /**
     * Check if this exception represents a client error (4xx status code).
     */
    public boolean isClientError() {
        return statusCode >= 400 && statusCode < 500;
    }
    
    /**
     * Check if this exception represents a server error (5xx status code).
     */
    public boolean isServerError() {
        return statusCode >= 500 && statusCode < 600;
    }
    
    /**
     * Check if this exception represents a rate limit error (429).
     */
    public boolean isRateLimitError() {
        return statusCode == 429;
    }
}
