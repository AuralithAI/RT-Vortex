package ai.aipr.server.session;

/**
 * Exception thrown when a session has expired.
 */
public class SessionExpiredException extends RuntimeException {
    
    public SessionExpiredException(String message) {
        super(message);
    }
    
    public SessionExpiredException(String message, Throwable cause) {
        super(message, cause);
    }
}
