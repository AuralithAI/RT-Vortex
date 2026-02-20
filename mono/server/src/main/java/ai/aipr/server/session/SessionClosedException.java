package ai.aipr.server.session;

/**
 * Exception thrown when a session has been closed.
 */
public class SessionClosedException extends RuntimeException {
    
    public SessionClosedException(String message) {
        super(message);
    }
    
    public SessionClosedException(String message, Throwable cause) {
        super(message, cause);
    }
}
